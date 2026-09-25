package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestImageDetailRespectsVisibility(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	public, rec := mediaUpload(t, a, token, "/api/upload/private?visibility=public", map[string]string{"alt": "Visible"})
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	private, rec := mediaUpload(t, a, token, "/api/upload/private?visibility=private", nil)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	visible := adminRequest(a, "", http.MethodGet, "/api/images/"+public.ID, "")
	if visible.Code != 200 || !strings.Contains(visible.Body.String(), `"alt":"Visible"`) || strings.Contains(visible.Body.String(), `"apiKeyId"`) {
		t.Fatalf("public image detail: %d %s", visible.Code, visible.Body.String())
	}
	if hidden := adminRequest(a, "", http.MethodGet, "/api/images/"+private.ID, ""); hidden.Code != 404 {
		t.Fatalf("private detail leaked: %d", hidden.Code)
	}
	if owner := adminRequest(a, token, http.MethodGet, "/api/images/"+private.ID, ""); owner.Code != 200 {
		t.Fatalf("admin detail: %d %s", owner.Code, owner.Body.String())
	}
	if page := adminRequest(a, "", http.MethodGet, "/image/"+public.ID, ""); page.Code != 200 {
		t.Fatalf("detail page: %d", page.Code)
	}
}
