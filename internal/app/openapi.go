package app

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.json
var openAPIDocument []byte

func (a *App) registerOpenAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(openAPIDocument)
	})
}
