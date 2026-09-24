// Command loadtest drives an isolated TanoImg instance over real HTTP.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	url := flag.String("url", "http://127.0.0.1:3042/api/upload/private", "test endpoint")
	concurrency := flag.Int("concurrency", 4, "closed-loop workers")
	duration := flag.Duration("duration", 10*time.Second, "dispatch window")
	count := flag.Int64("count", 0, "fixed request count instead of duration")
	side := flag.Int("side", 256, "random RGBA PNG side length")
	get := flag.Bool("get", false, "GET instead of multipart upload")
	flag.Parse()
	if *concurrency < 1 || *side < 1 || *side > 4096 || *duration <= 0 || *count < 0 {
		panic("invalid test options")
	}
	var payload bytes.Buffer
	form := multipart.NewWriter(&payload)
	imageBytes := 0
	if !*get {
		pixels := image.NewRGBA(image.Rect(0, 0, *side, *side))
		if _, err := rand.Read(pixels.Pix); err != nil {
			panic(err)
		}
		var img bytes.Buffer
		if err := png.Encode(&img, pixels); err != nil {
			panic(err)
		}
		imageBytes = img.Len()
		part, err := form.CreateFormFile("file", "loadtest.png")
		if err != nil {
			panic(err)
		}
		if _, err := part.Write(img.Bytes()); err != nil {
			panic(err)
		}
	}
	if err := form.Close(); err != nil {
		panic(err)
	}
	transport := &http.Transport{MaxIdleConns: *concurrency, MaxIdleConnsPerHost: *concurrency, MaxConnsPerHost: *concurrency, DisableCompression: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	var mu sync.Mutex
	statuses := map[int]int{}
	latencies := []float64{}
	var next atomic.Int64
	var transferred atomic.Int64
	var workers sync.WaitGroup
	start := time.Now()
	deadline := start.Add(*duration)
	for range *concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				if *count > 0 {
					if next.Add(1) > *count {
						return
					}
				} else if time.Now().After(deadline) {
					return
				}
				method := "POST"
				if *get {
					method = "GET"
				}
				var body io.Reader
				if !*get {
					body = bytes.NewReader(payload.Bytes())
				}
				req, err := http.NewRequest(method, *url, body)
				if err != nil {
					panic(err)
				}
				req.Header.Set("Content-Type", form.FormDataContentType())
				req.Header.Set("X-API-Key", os.Getenv("TANOIMG_BENCH_KEY"))
				began := time.Now()
				resp, err := client.Do(req)
				status := 0
				if err == nil {
					n, readErr := io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if readErr == nil {
						status = resp.StatusCode
						if status == 200 {
							transferred.Add(n)
						}
					}
				}
				latency := float64(time.Since(began).Microseconds()) / 1000
				mu.Lock()
				statuses[status]++
				if status == 200 {
					latencies = append(latencies, latency)
				}
				mu.Unlock()
			}
		}()
	}
	workers.Wait()
	elapsed := time.Since(start).Seconds()
	sort.Float64s(latencies)
	percentile := func(p float64) float64 {
		if len(latencies) == 0 {
			return 0
		}
		return latencies[int(math.Ceil(p*float64(len(latencies))))-1]
	}
	requests := 0
	for _, n := range statuses {
		requests += n
	}
	good := len(latencies)
	result := map[string]any{"concurrency": *concurrency, "seconds": elapsed, "requests": requests, "success": good, "statuses": statuses, "success_rps": float64(good) / elapsed, "error_pct": 100 * float64(requests-good) / float64(requests), "p50_ms": percentile(.5), "p95_ms": percentile(.95), "p99_ms": percentile(.99), "image_bytes": imageBytes, "upload_mib_s": float64(good) * float64(imageBytes) / elapsed / (1 << 20), "response_mib_s": float64(transferred.Load()) / elapsed / (1 << 20), "method": map[bool]string{true: "GET", false: "POST"}[*get]}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
