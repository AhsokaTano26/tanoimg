package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransferMetricsCountsSuccessfulImageBytesAndEstimatesCost(t *testing.T) {
	a := testApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	done, err := a.StartTransferMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); <-done }()
	wrapped := a.wrapTransferMetrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "missing") {
			http.NotFound(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "range") {
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write([]byte("12345"))
	}))
	for _, path := range []string{"/i/a.png", "/i/range.png", "/t/a.png", "/api/shares/token/files/a.png", "/i/missing.png", "/api/images"} {
		wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	if err := a.flushTransferMetrics(); err != nil {
		t.Fatal(err)
	}
	token := migrationLogin(t, a)
	mux := http.NewServeMux()
	a.registerTransferStatsRoutes(mux)
	read := func(method, path, body, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if got := read("GET", "/api/admin/transfer-stats", "", ""); got.Code != 401 {
		t.Fatalf("anonymous: %d", got.Code)
	}
	pricing := read("PUT", "/api/admin/transfer-pricing", `{"currency":"USD","egressPerGiB":0.09}`, token)
	if pricing.Code != 200 {
		t.Fatalf("pricing: %d %s", pricing.Code, pricing.Body.String())
	}
	stats := read("GET", "/api/admin/transfer-stats?days=7", "", token)
	if stats.Code != 200 {
		t.Fatalf("stats: %d %s", stats.Code, stats.Body.String())
	}
	var result struct {
		Data struct {
			Requests            int64   `json:"requests"`
			Bytes               int64   `json:"bytes"`
			EstimatedEgressCost float64 `json:"estimatedEgressCost"`
			Basis               string  `json:"basis"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stats.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Requests != 4 || result.Data.Bytes != 20 || result.Data.EstimatedEgressCost <= 0 || result.Data.Basis == "" {
		t.Fatalf("wrong metrics: %s", stats.Body.String())
	}
}

func TestTransferPricingValidation(t *testing.T) {
	a := testApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	done, err := a.StartTransferMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); <-done }()
	token := migrationLogin(t, a)
	mux := http.NewServeMux()
	a.registerTransferStatsRoutes(mux)
	for _, body := range []string{`{"currency":"BAD","egressPerGiB":1}`, `{"currency":"USD","egressPerGiB":-1}`, `{"currency":"USD","egressPerGiB":1000001}`} {
		r := httptest.NewRequest("PUT", "/api/admin/transfer-pricing", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d %s", body, w.Code, w.Body.String())
		}
	}
}
