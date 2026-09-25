package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnonymousUploadReceiptDeletesOnlyItsImage(t *testing.T) {
	a := testApp(t)
	if err := ensureReceiptSchema(a); err != nil {
		t.Fatal(err)
	}
	im := Image{ID: "receipt-image", UUID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Filename: "cccccccc-cccc-4ccc-8ccc-cccccccccccc.png", Format: "png", Visibility: "public", UploadedAt: now(), UpdatedAt: now()}
	if err := a.saveImage(im); err != nil {
		t.Fatal(err)
	}
	token, err := a.issueUploadReceipt(im.ID)
	if err != nil {
		t.Fatal(err)
	}
	var persisted string
	if err := a.DB.QueryRow(`SELECT token_hash FROM upload_receipts WHERE image_id=?`, im.ID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted == token {
		t.Fatal("receipt secret stored in cleartext")
	}
	mux := http.NewServeMux()
	a.registerReceiptRoutes(mux)
	call := func(secret string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("DELETE", "/api/images/receipt-image/self", nil)
		r.Header.Set("X-Upload-Receipt", secret)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if rec := call("wrong-secret-with-ample-length-000000000000"); rec.Code != 404 {
		t.Fatalf("wrong token: %d", rec.Code)
	}
	if rec := call(token); rec.Code != 200 {
		t.Fatalf("valid token: %d %s", rec.Code, rec.Body.String())
	}
	var deleted bool
	if err := a.DB.QueryRow(`SELECT is_deleted FROM images WHERE id=?`, im.ID).Scan(&deleted); err != nil || !deleted {
		t.Fatalf("image not recycled: %t %v", deleted, err)
	}
	if rec := call(token); rec.Code != 404 {
		t.Fatalf("replay: %d", rec.Code)
	}
}
