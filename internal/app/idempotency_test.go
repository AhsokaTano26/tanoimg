package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIdempotentUploadClaimAndReconcile(t *testing.T) {
	a := testApp(t)
	if err := ensureIdempotencySchema(a); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/upload/public", nil)
	req.RemoteAddr = "192.0.2.25:40000"
	req.Header.Set("Idempotency-Key", "client-batch-item-001")
	key, prior, err := a.claimUploadKey(req, true)
	if err != nil || key == "" || prior != nil {
		t.Fatalf("first claim: %q %+v %v", key, prior, err)
	}
	if _, _, err := a.claimUploadKey(req, true); err != errUploadKeyBusy {
		t.Fatalf("concurrent retry: %v", err)
	}
	im := Image{ID: "idem-image", UUID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", Filename: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee.png", Format: "png", Visibility: "public", UploadedAt: now(), UpdatedAt: now()}
	if err := a.saveImage(im); err != nil {
		t.Fatal(err)
	}
	if err := a.bindUploadKey(req, true, key, im.ID); err != nil {
		t.Fatal(err)
	}
	_, recovered, err := a.claimUploadKey(req, true)
	if err != nil || recovered == nil || recovered.ID != im.ID {
		t.Fatalf("crash recovery: %+v %v", recovered, err)
	}
	key, prior, err = a.claimUploadKey(req, true)
	if err != nil || key != "" || prior == nil || prior.ID != im.ID {
		t.Fatalf("replay: %q %+v %v", key, prior, err)
	}
	mux := http.NewServeMux()
	a.registerIdempotencyRoutes(mux)
	reconcile := httptest.NewRequest("POST", "/api/uploads/reconcile", strings.NewReader(`{"keys":["client-batch-item-001","unknown-item-999"]}`))
	reconcile.RemoteAddr = req.RemoteAddr
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, reconcile)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"imageId":"idem-image"`) || !strings.Contains(w.Body.String(), `"status":"unknown"`) {
		t.Fatalf("reconcile: %d %s", w.Code, w.Body.String())
	}
}
