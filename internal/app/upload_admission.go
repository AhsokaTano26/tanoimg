package app

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	errUploadQueueFull   = errors.New("upload queue is full")
	errUploadWaitTimeout = errors.New("upload wait timed out")
)

type uploadWaiter struct {
	principal string
	ready     chan struct{}
	granted   bool // guarded by fairUploadScheduler.mu
}

// fairUploadScheduler is work-conserving and caps both active and queued work.
// It favors the waiting principal with the fewest active slots, then rotates
// equally represented principals. Each principal retains FIFO order.
type fairUploadScheduler struct {
	mu              sync.Mutex
	maxActive       int
	maxWaiting      int
	maxPerPrincipal int
	waitTimeout     time.Duration
	active          int
	waiting         int
	activeBy        map[string]int
	queues          map[string][]*uploadWaiter
	order           []string
}

func newFairUploadScheduler(maxActive, maxWaiting, maxPerPrincipal int) *fairUploadScheduler {
	if maxActive < 1 || maxWaiting < 1 || maxPerPrincipal < 1 {
		panic("invalid upload scheduler limits")
	}
	return &fairUploadScheduler{maxActive: maxActive, maxWaiting: maxWaiting, maxPerPrincipal: maxPerPrincipal, waitTimeout: 30 * time.Second, activeBy: make(map[string]int), queues: make(map[string][]*uploadWaiter)}
}

func (s *fairUploadScheduler) releaseFunc(principal string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.releaseLocked(principal)
		})
	}
}

func (s *fairUploadScheduler) releaseLocked(principal string) {
	if s.active == 0 || s.activeBy[principal] == 0 {
		return
	}
	s.active--
	s.activeBy[principal]--
	if s.activeBy[principal] == 0 {
		delete(s.activeBy, principal)
	}
	s.dispatchLocked()
}

func (s *fairUploadScheduler) dispatchLocked() {
	for s.active < s.maxActive && len(s.order) != 0 {
		choice := 0
		for i := 1; i < len(s.order); i++ {
			if s.activeBy[s.order[i]] < s.activeBy[s.order[choice]] {
				choice = i
			}
		}
		principal := s.order[choice]
		queue := s.queues[principal]
		waiter := queue[0]
		queue = queue[1:]
		s.order = append(s.order[:choice], s.order[choice+1:]...)
		if len(queue) == 0 {
			delete(s.queues, principal)
		} else {
			s.queues[principal] = queue
			s.order = append(s.order, principal)
		}
		s.waiting--
		s.active++
		s.activeBy[principal]++
		waiter.granted = true
		close(waiter.ready)
	}
}

func (s *fairUploadScheduler) removeWaiterLocked(waiter *uploadWaiter) {
	queue := s.queues[waiter.principal]
	for i, candidate := range queue {
		if candidate == waiter {
			queue = append(queue[:i], queue[i+1:]...)
			s.waiting--
			if len(queue) == 0 {
				delete(s.queues, waiter.principal)
				for j, principal := range s.order {
					if principal == waiter.principal {
						s.order = append(s.order[:j], s.order[j+1:]...)
						break
					}
				}
			} else {
				s.queues[waiter.principal] = queue
			}
			return
		}
	}
}

func (s *fairUploadScheduler) Acquire(ctx context.Context, principal string) (func(), error) {
	if principal == "" {
		principal = "unknown"
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.active < s.maxActive && s.waiting == 0 {
		s.active++
		s.activeBy[principal]++
		s.mu.Unlock()
		return s.releaseFunc(principal), nil
	}
	if s.waiting >= s.maxWaiting || len(s.queues[principal]) >= s.maxPerPrincipal {
		s.mu.Unlock()
		return nil, errUploadQueueFull
	}
	waiter := &uploadWaiter{principal: principal, ready: make(chan struct{})}
	if len(s.queues[principal]) == 0 {
		s.order = append(s.order, principal)
	}
	s.queues[principal] = append(s.queues[principal], waiter)
	s.waiting++
	s.dispatchLocked()
	s.mu.Unlock()

	waitCtx, cancel := context.WithTimeout(ctx, s.waitTimeout)
	defer cancel()
	select {
	case <-waiter.ready:
		if err := waitCtx.Err(); err != nil {
			s.mu.Lock()
			s.releaseLocked(principal)
			s.mu.Unlock()
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				return nil, errUploadWaitTimeout
			}
			return nil, err
		}
		return s.releaseFunc(principal), nil
	case <-waitCtx.Done():
		s.mu.Lock()
		if waiter.granted {
			s.releaseLocked(principal)
		} else {
			s.removeWaiterLocked(waiter)
			s.dispatchLocked()
		}
		s.mu.Unlock()
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return nil, errUploadWaitTimeout
		}
		return nil, waitCtx.Err()
	}
}
