package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type admissionResult struct {
	release func()
	err     error
}

func waitForAdmissionQueue(t *testing.T, scheduler *fairUploadScheduler, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		scheduler.mu.Lock()
		queued := scheduler.waiting
		scheduler.mu.Unlock()
		if queued == count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("queue did not reach %d waiters", count)
}

func TestFairUploadAdmissionPrefersUnderrepresentedPrincipal(t *testing.T) {
	scheduler := newFairUploadScheduler(2, 4, 3)
	releaseA1, err := scheduler.Acquire(context.Background(), "key:a")
	if err != nil {
		t.Fatal(err)
	}
	releaseA2, err := scheduler.Acquire(context.Background(), "key:a")
	if err != nil {
		t.Fatal(err)
	}
	queuedA := make(chan admissionResult, 1)
	queuedB := make(chan admissionResult, 1)
	go func() {
		release, err := scheduler.Acquire(context.Background(), "key:a")
		queuedA <- admissionResult{release, err}
	}()
	waitForAdmissionQueue(t, scheduler, 1)
	go func() {
		release, err := scheduler.Acquire(context.Background(), "ip:b")
		queuedB <- admissionResult{release, err}
	}()
	waitForAdmissionQueue(t, scheduler, 2)
	releaseA1()
	select {
	case granted := <-queuedB:
		if granted.err != nil {
			t.Fatal(granted.err)
		}
		select {
		case <-queuedA:
			t.Fatal("same principal got a slot before waiting peer")
		default:
		}
		granted.release()
	case <-time.After(time.Second):
		t.Fatal("underrepresented principal did not get free slot")
	}
	grantedA := <-queuedA
	if grantedA.err != nil {
		t.Fatal(grantedA.err)
	}
	grantedA.release()
	releaseA2()
	scheduler.mu.Lock()
	active := scheduler.active
	scheduler.mu.Unlock()
	if active != 0 {
		t.Fatalf("active slot leaked: %d", active)
	}
}

func TestFairUploadAdmissionBoundsQueueAndCancelsWait(t *testing.T) {
	scheduler := newFairUploadScheduler(1, 1, 1)
	release, err := scheduler.Acquire(context.Background(), "ip:a")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() { _, err := scheduler.Acquire(ctx, "ip:b"); queued <- err }()
	waitForAdmissionQueue(t, scheduler, 1)
	if _, err := scheduler.Acquire(context.Background(), "ip:c"); !errors.Is(err, errUploadQueueFull) {
		t.Fatalf("queue overflow: %v", err)
	}
	cancel()
	if err := <-queued; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter: %v", err)
	}
	release()
	if releaseNext, err := scheduler.Acquire(context.Background(), "ip:c"); err != nil {
		t.Fatalf("cancellation leaked capacity: %v", err)
	} else {
		releaseNext()
	}
}

func TestFairUploadAdmissionNeverExceedsActiveLimit(t *testing.T) {
	scheduler := newFairUploadScheduler(4, 32, 16)
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			release, err := scheduler.Acquire(ctx, string(rune('a'+i%5)))
			if err != nil {
				return
			}
			scheduler.mu.Lock()
			if scheduler.active > 4 {
				t.Errorf("active=%d", scheduler.active)
			}
			scheduler.mu.Unlock()
			release()
		}(i)
	}
	wg.Wait()
}
