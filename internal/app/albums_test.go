package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAlbumPublicViewNeverRevealsPrivateImage(t *testing.T) {
	a := testApp(t)
	if err := ensureAlbumSchema(a); err != nil {
		t.Fatal(err)
	}
	for _, im := range []Image{
		{ID: "album-public", UUID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Filename: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa.png", Format: "png", Visibility: "public", UploadedAt: now(), UpdatedAt: now()},
		{ID: "album-private", UUID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Filename: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb.png", Format: "png", Visibility: "private", UploadedAt: now(), UpdatedAt: now()},
	} {
		if err := a.saveImage(im); err != nil {
			t.Fatal(err)
		}
	}
	token := adminToken(t, a)
	mux := http.NewServeMux()
	a.registerAlbumRoutes(mux)
	call := func(method, path, body string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	created := call("POST", "/api/admin/albums", `{"title":"Public collection","description":"Pictures","visibility":"public"}`, true)
	if created.Code != 200 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var payload struct {
		Data Album `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	id := payload.Data.ID
	if rec := call("PUT", "/api/admin/albums/"+id+"/images", `{"imageIds":["album-public","album-private"]}`, true); rec.Code != 200 {
		t.Fatalf("membership: %d %s", rec.Code, rec.Body.String())
	}
	public := call("GET", "/api/albums/"+id, "", false)
	if public.Code != 200 || !strings.Contains(public.Body.String(), "album-public") || strings.Contains(public.Body.String(), "album-private") {
		t.Fatalf("public detail: %d %s", public.Code, public.Body.String())
	}
	listing := call("GET", "/api/albums", "", false)
	if listing.Code != 200 || !strings.Contains(listing.Body.String(), `"imageCount":1`) {
		t.Fatalf("public count: %d %s", listing.Code, listing.Body.String())
	}
	admin := call("GET", "/api/admin/albums/"+id, "", true)
	if admin.Code != 200 || !strings.Contains(admin.Body.String(), "album-private") {
		t.Fatalf("admin detail: %d %s", admin.Code, admin.Body.String())
	}
}
