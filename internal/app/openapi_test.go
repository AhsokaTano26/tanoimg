package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAPIDocumentServedAndCoversUploadFlows(t *testing.T) {
	a := testApp(t)
	mux := http.NewServeMux()
	a.registerOpenAPIRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/openapi.json", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("document: %d %s", w.Code, w.Body.String())
	}
	var doc struct {
		OpenAPI    string                    `json:"openapi"`
		Paths      map[string]map[string]any `json:"paths"`
		Components struct {
			SecuritySchemes map[string]any `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI != "3.1.0" || len(doc.Paths) < 60 || len(doc.Components.SecuritySchemes) < 2 {
		t.Fatalf("incomplete document: %s, %d paths", doc.OpenAPI, len(doc.Paths))
	}
	for _, pair := range [][2]string{{"/api/upload/private", "post"}, {"/api/upload/resumable", "post"}, {"/api/upload/resumable/{id}/chunk", "put"}, {"/api/admin/transfer-stats", "get"}, {"/api/openapi.json", "get"}} {
		if doc.Paths[pair[0]][pair[1]] == nil {
			t.Fatalf("missing %s %s", pair[1], pair[0])
		}
	}
}
