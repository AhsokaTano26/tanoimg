package app

import (
	"bytes"
	"crypto/rand"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkPrivateUpload measures the whole HTTP handler, SQLite insert, and
// filesystem write. Random pixels make the PNG valid and hard to compress.
func BenchmarkPrivateUpload(b *testing.B)         { benchmarkPrivateUpload(b, false, 256) }
func BenchmarkPrivateUploadHTTP(b *testing.B)     { benchmarkPrivateUpload(b, true, 256) }
func BenchmarkPrivateUploadHTTP2MiB(b *testing.B) { benchmarkPrivateUpload(b, true, 724) }

func benchmarkPrivateUpload(b *testing.B, overHTTP bool, side int) {
	a, err := New(Config{DataDir: b.TempDir()})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { a.Close() })
	if _, err := a.DB.Exec(`INSERT INTO apikeys(id,key,name,enabled,is_default,created_at) VALUES('bench','sk-bench','bench',1,0,'')`); err != nil {
		b.Fatal(err)
	}
	var imageData bytes.Buffer
	imagePixels := image.NewRGBA(image.Rect(0, 0, side, side))
	if _, err := rand.Read(imagePixels.Pix); err != nil {
		b.Fatal(err)
	}
	if err := png.Encode(&imageData, imagePixels); err != nil {
		b.Fatal(err)
	}
	var payload bytes.Buffer
	form := multipart.NewWriter(&payload)
	file, err := form.CreateFormFile("file", "bench.png")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := file.Write(imageData.Bytes()); err != nil {
		b.Fatal(err)
	}
	if err := form.Close(); err != nil {
		b.Fatal(err)
	}
	body := payload.Bytes()
	handler := a.Handler()
	var send func() bool
	if overHTTP {
		server := httptest.NewServer(handler)
		b.Cleanup(server.Close)
		transport := &http.Transport{MaxIdleConns: 8, MaxIdleConnsPerHost: 8, MaxConnsPerHost: 8}
		b.Cleanup(transport.CloseIdleConnections)
		client := &http.Client{Transport: transport}
		send = func() bool {
			req, err := http.NewRequest(http.MethodPost, server.URL+"/api/upload/private", bytes.NewReader(body))
			if err != nil {
				return false
			}
			req.Header.Set("Content-Type", form.FormDataContentType())
			req.Header.Set("X-API-Key", "sk-bench")
			resp, err := client.Do(req)
			if err != nil {
				return false
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return resp.StatusCode == http.StatusOK
		}
	} else {
		send = func() bool {
			req := httptest.NewRequest(http.MethodPost, "/api/upload/private", bytes.NewReader(body))
			req.Header.Set("Content-Type", form.FormDataContentType())
			req.Header.Set("X-API-Key", "sk-bench")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			return rec.Code == http.StatusOK
		}
	}
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	start := time.Now()
	var peakHeap atomic.Uint64
	monitorDone := make(chan struct{})
	monitorExited := make(chan struct{})
	go func() {
		defer close(monitorExited)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-monitorDone:
				return
			case <-ticker.C:
				var memory runtime.MemStats
				runtime.ReadMemStats(&memory)
				for current := peakHeap.Load(); memory.Alloc > current; current = peakHeap.Load() {
					if peakHeap.CompareAndSwap(current, memory.Alloc) {
						break
					}
				}
			}
		}
	}()
	var next atomic.Int64
	var failures atomic.Int64
	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				index := next.Add(1)
				if index > int64(b.N) {
					return
				}
				if !send() {
					failures.Add(1)
				}
			}
		}()
	}
	workers.Wait()
	close(monitorDone)
	<-monitorExited
	elapsed := time.Since(start)
	b.StopTimer()
	if failures.Load() != 0 {
		b.Fatalf("%d upload requests failed", failures.Load())
	}
	var stored int
	if err := a.DB.QueryRow(`SELECT count(*) FROM images`).Scan(&stored); err != nil || stored != b.N {
		b.Fatalf("stored %d of %d images: %v", stored, b.N, err)
	}
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "uploads/s")
	b.ReportMetric(float64(peakHeap.Load())/(1<<20), "peakheap-MiB")
}
