package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageAccessAndFrontendAssets(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	for _, target := range []string{"/", "/upload", "/login"} {
		got := adminRequest(a, "", http.MethodGet, target, "")
		if got.Code != 200 {
			t.Fatalf("%s: %d", target, got.Code)
		}
	}
	for _, target := range []string{"/admin", "/admin/gallery", "/admin/upload", "/admin/recycle", "/admin/stats", "/admin/api", "/admin/settings", "/admin/appearance", "/admin/site", "/admin/public-upload", "/admin/private-upload", "/admin/apikeys", "/admin/moderation", "/admin/moderation-images", "/admin/notification", "/admin/account", "/admin/blacklist", "/admin/storage", "/admin/about", "/settings", "/recycle", "/stats", "/api"} {
		got := adminRequest(a, "", http.MethodGet, target, "")
		if got.Code != http.StatusSeeOther || !strings.HasPrefix(got.Header().Get("Location"), "/login?redirect=") {
			t.Fatalf("guest %s: %d %s", target, got.Code, got.Header().Get("Location"))
		}
		got = adminRequest(a, token, http.MethodGet, target, "")
		if got.Code != 200 {
			t.Fatalf("admin %s: %d", target, got.Code)
		}
	}
	entries, err := webFiles.ReadDir("web/assets")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		got := httptest.NewRecorder()
		a.Handler().ServeHTTP(got, httptest.NewRequest(http.MethodGet, "/assets/"+entry.Name(), nil))
		kind := "javascript"
		if strings.HasSuffix(entry.Name(), ".css") {
			kind = "text/css"
		}
		if got.Code != 200 || !strings.Contains(got.Header().Get("Content-Type"), kind) {
			t.Fatalf("asset %s: %d %s", entry.Name(), got.Code, got.Header().Get("Content-Type"))
		}
	}
}

func TestPublicGalleryScopeWithAdminSession(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	private := fixtureImage(t, a, "private-image", false)
	public := fixtureImage(t, a, "public-image", false)
	public.UploadedByType = "public"
	if err := a.saveImage(public); err != nil {
		t.Fatal(err)
	}
	got := adminRequest(a, token, http.MethodGet, "/api/images?scope=public", "")
	if got.Code != 200 || strings.Contains(got.Body.String(), private.ID) || !strings.Contains(got.Body.String(), public.ID) {
		t.Fatalf("public scope: %d %s", got.Code, got.Body.String())
	}
	got = adminRequest(a, token, http.MethodGet, "/api/images", "")
	if got.Code != 200 || !strings.Contains(got.Body.String(), private.ID) {
		t.Fatalf("admin scope: %d %s", got.Code, got.Body.String())
	}
}
