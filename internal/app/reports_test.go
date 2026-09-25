package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicImageReportIsScopedAndDeduplicated(t *testing.T) {
	a := testApp(t)
	if err := ensureReportSchema(a); err != nil {
		t.Fatal(err)
	}
	for _, im := range []Image{
		{ID: "public", UUID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Filename: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa.png", Format: "png", Visibility: "public", UploadedAt: now(), UpdatedAt: now()},
		{ID: "unlisted", UUID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Filename: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb.png", Format: "png", Visibility: "unlisted", UploadedAt: now(), UpdatedAt: now()},
	} {
		if err := a.saveImage(im); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	a.registerReportRoutes(mux)
	report := func(id string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/images/"+id+"/report", strings.NewReader(`{"reason":"copyright","details":"Source owner"}`))
		r.RemoteAddr = "192.0.2.12:5432"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if r := report("unlisted"); r.Code != 404 {
		t.Fatalf("unlisted: %d", r.Code)
	}
	if r := report("public"); r.Code != 200 || !strings.Contains(r.Body.String(), `"new":true`) {
		t.Fatalf("first: %d %s", r.Code, r.Body.String())
	}
	if r := report("public"); r.Code != 200 || !strings.Contains(r.Body.String(), `"new":false`) {
		t.Fatalf("duplicate: %d %s", r.Code, r.Body.String())
	}
	var count int
	if err := a.DB.QueryRow(`SELECT count(*) FROM image_reports`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("reports: %d %v", count, err)
	}
}
