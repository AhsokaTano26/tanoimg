package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestEmbedTemplatesAdminValidationAndPublicRead(t *testing.T) {
	a := testApp(t)
	guest := adminRequest(a, "", http.MethodGet, "/api/embed-templates", "")
	if guest.Code != 200 || !strings.Contains(guest.Body.String(), `"id":"markdown"`) {
		t.Fatalf("default templates: %d %s", guest.Code, guest.Body.String())
	}
	if changed := adminRequest(a, "", http.MethodPut, "/api/admin/embed-templates", `[ {"id":"plain","name":"Plain","body":"{url}"} ]`); changed.Code != 401 {
		t.Fatalf("guest changed templates: %d", changed.Code)
	}
	token := adminToken(t, a)
	for _, body := range []string{
		`[{"id":"bad","name":"Bad","body":"{unknown} {url}"}]`,
		`[{"id":"same","name":"One","body":"{url}"},{"id":"same","name":"Two","body":"{url}"}]`,
		`[{"id":"missing","name":"No URL","body":"test"}]`,
	} {
		if changed := adminRequest(a, token, http.MethodPut, "/api/admin/embed-templates", body); changed.Code != 400 {
			t.Fatalf("accepted invalid templates: %d %s", changed.Code, body)
		}
	}
	body := `[{"id":"full","name":"完整图片","body":"<img src=\"{url}\" alt=\"{alt}\" width=\"{width}\" height=\"{height}\" data-name=\"{filename}\">"}]`
	if changed := adminRequest(a, token, http.MethodPut, "/api/admin/embed-templates", body); changed.Code != 200 {
		t.Fatalf("save: %d %s", changed.Code, changed.Body.String())
	}
	guest = adminRequest(a, "", http.MethodGet, "/api/embed-templates", "")
	if guest.Code != 200 || !strings.Contains(guest.Body.String(), `"id":"full"`) || strings.Contains(guest.Body.String(), `"id":"markdown"`) {
		t.Fatalf("public configured templates: %d %s", guest.Code, guest.Body.String())
	}
}
