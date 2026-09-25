package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const transferGiB = float64(1 << 30)

type transferKey struct{ day, category string }
type transferCount struct{ requests, bytes int64 }
type transferCollector struct {
	mu      sync.Mutex
	flushMu sync.Mutex
	pending map[transferKey]transferCount
	dropped int64
	started bool
}

var transferCollectors sync.Map // *App -> *transferCollector; removed after worker shutdown.

func collectorFor(a *App) *transferCollector {
	if value, ok := transferCollectors.Load(a); ok {
		return value.(*transferCollector)
	}
	created := &transferCollector{pending: make(map[transferKey]transferCount)}
	value, _ := transferCollectors.LoadOrStore(a, created)
	return value.(*transferCollector)
}

func (a *App) ensureTransferSchema() error {
	_, err := a.DB.Exec(`CREATE TABLE IF NOT EXISTS transfer_daily (day TEXT NOT NULL,category TEXT NOT NULL,requests INTEGER NOT NULL,bytes INTEGER NOT NULL,PRIMARY KEY(day,category))`)
	return err
}

// StartTransferMetrics flushes a fixed-size in-memory accumulator every ten
// seconds and on cancellation. It must be stopped before closing the database.
func (a *App) StartTransferMetrics(ctx context.Context) (<-chan struct{}, error) {
	if err := a.ensureTransferSchema(); err != nil {
		return nil, err
	}
	c := collectorFor(a)
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil, errors.New("transfer metrics worker already started")
	}
	c.started = true
	c.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = a.flushTransferMetrics()
				return
			case <-ticker.C:
				_ = a.flushTransferMetrics()
			}
		}
	}()
	return done, nil
}

func (a *App) recordTransfer(category string, bytes int64) {
	key := transferKey{day: time.Now().UTC().Format("2006-01-02"), category: category}
	c := collectorFor(a)
	c.mu.Lock()
	if _, found := c.pending[key]; !found && len(c.pending) >= 32 {
		c.dropped++
		c.mu.Unlock()
		return
	}
	value := c.pending[key]
	value.requests++
	value.bytes += bytes
	c.pending[key] = value
	c.mu.Unlock()
}

func (a *App) flushTransferMetrics() error {
	c := collectorFor(a)
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	c.mu.Lock()
	batch := c.pending
	c.pending = make(map[transferKey]transferCount)
	c.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	tx, err := a.DB.Begin()
	if err == nil {
		for key, count := range batch {
			_, err = tx.Exec(`INSERT INTO transfer_daily(day,category,requests,bytes) VALUES(?,?,?,?) ON CONFLICT(day,category) DO UPDATE SET requests=requests+excluded.requests,bytes=bytes+excluded.bytes`, key.day, key.category, count.requests, count.bytes)
			if err != nil {
				break
			}
		}
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
	}
	if err != nil {
		c.mu.Lock()
		for key, count := range batch {
			if _, found := c.pending[key]; !found && len(c.pending) >= 32 {
				c.dropped += count.requests
				continue
			}
			value := c.pending[key]
			value.requests += count.requests
			value.bytes += count.bytes
			c.pending[key] = value
		}
		c.mu.Unlock()
	}
	return err
}

type transferResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *transferResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *transferResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
	return n, err
}
func (w *transferResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	if w.status == 0 {
		w.status = 200
	}
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err := rf.ReadFrom(reader)
		w.bytes += n
		return n, err
	}
	return io.Copy(struct{ io.Writer }{w}, reader)
}
func (w *transferResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func transferCategory(path string) string {
	if strings.HasPrefix(path, "/i/") {
		return "original"
	}
	if strings.HasPrefix(path, "/t/") {
		return "thumbnail"
	}
	if strings.HasPrefix(path, "/api/shares/") && strings.Contains(path, "/files/") {
		return "share"
	}
	return ""
}

func (a *App) wrapTransferMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		category := transferCategory(r.URL.Path)
		if r.Method != http.MethodGet || category == "" {
			next.ServeHTTP(w, r)
			return
		}
		tracked := &transferResponseWriter{ResponseWriter: w}
		next.ServeHTTP(tracked, r)
		if tracked.status == 200 || tracked.status == http.StatusPartialContent {
			a.recordTransfer(category, tracked.bytes)
		}
	})
}

type transferPricing struct {
	Currency     string  `json:"currency"`
	EgressPerGiB float64 `json:"egressPerGiB"`
}

func validTransferPricing(p transferPricing) bool {
	switch p.Currency {
	case "USD", "CNY", "EUR", "GBP", "JPY":
	default:
		return false
	}
	return p.EgressPerGiB >= 0 && p.EgressPerGiB <= 1000000 && !math.IsNaN(p.EgressPerGiB) && !math.IsInf(p.EgressPerGiB, 0)
}

func (a *App) transferPricing() transferPricing {
	p := transferPricing{Currency: "USD"}
	_ = json.Unmarshal(a.setting("transferPricing", p), &p)
	if !validTransferPricing(p) {
		return transferPricing{Currency: "USD"}
	}
	return p
}

func (a *App) registerTransferStatsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/transfer-stats", a.getTransferStats)
	mux.HandleFunc("GET /api/admin/transfer-pricing", a.getTransferPricing)
	mux.HandleFunc("PUT /api/admin/transfer-pricing", a.putTransferPricing)
}

func (a *App) getTransferPricing(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, a.transferPricing())
}

func (a *App) putTransferPricing(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var p transferPricing
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF || !validTransferPricing(p) {
		fail(w, 400, "费用估算设置无效")
		return
	}
	value, _ := json.Marshal(p)
	if err := a.setSetting("transferPricing", value); err != nil {
		fail(w, 500, "无法保存费用估算设置")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, p)
}

type transferDay struct {
	Day      string `json:"day"`
	Category string `json:"category"`
	Requests int64  `json:"requests"`
	Bytes    int64  `json:"bytes"`
}

func (a *App) getTransferStats(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	days := 30
	if raw := r.URL.Query().Get("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 365 {
			fail(w, 400, "days 必须为 1 至 365")
			return
		}
		days = parsed
	}
	if err := a.flushTransferMetrics(); err != nil {
		fail(w, 503, "暂时无法读取传输统计")
		return
	}
	start := time.Now().UTC().AddDate(0, 0, 1-days).Format("2006-01-02")
	rows, err := a.DB.Query(`SELECT day,category,requests,bytes FROM transfer_daily WHERE day>=? ORDER BY day DESC,category`, start)
	if err != nil {
		fail(w, 500, "无法读取传输统计")
		return
	}
	defer rows.Close()
	entries := make([]transferDay, 0)
	var requests, bytes int64
	for rows.Next() {
		var entry transferDay
		if err := rows.Scan(&entry.Day, &entry.Category, &entry.Requests, &entry.Bytes); err != nil {
			fail(w, 500, "无法读取传输统计")
			return
		}
		entries = append(entries, entry)
		requests += entry.Requests
		bytes += entry.Bytes
	}
	if err := rows.Err(); err != nil {
		fail(w, 500, "无法读取传输统计")
		return
	}
	p := a.transferPricing()
	c := collectorFor(a)
	c.mu.Lock()
	dropped := c.dropped
	c.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	ok(w, map[string]any{"days": days, "requests": requests, "bytes": bytes, "daily": entries, "pricing": p, "estimatedEgressCost": float64(bytes) / transferGiB * p.EgressPerGiB, "droppedEvents": dropped, "basis": "Application response body bytes for successful original, thumbnail and shared image GETs; excludes CDN, reverse proxy, TLS overhead and provider billing."})
}
