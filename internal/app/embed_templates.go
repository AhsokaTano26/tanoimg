package app

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
)

type embedTemplate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Body string `json:"body"`
}

var embedTemplateID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
var embedPlaceholder = regexp.MustCompile(`\{[^{}]*\}`)

func defaultEmbedTemplates() []embedTemplate {
	return []embedTemplate{
		{ID: "direct", Name: "直链", Body: "{url}"},
		{ID: "html", Name: "HTML", Body: `<img src="{url}" alt="{alt}">`},
		{ID: "markdown", Name: "Markdown", Body: `![{alt}]({url})`},
		{ID: "bbcode", Name: "BBCode", Body: `[img]{url}[/img]`},
	}
}

func (a *App) embedTemplates() []embedTemplate {
	defaultValue := defaultEmbedTemplates()
	var templates []embedTemplate
	if err := json.Unmarshal(a.setting("embedTemplates", defaultValue), &templates); err != nil || !validEmbedTemplates(templates) {
		return defaultValue
	}
	return templates
}

func validEmbedTemplates(templates []embedTemplate) bool {
	if len(templates) == 0 || len(templates) > 12 {
		return false
	}
	known := map[string]bool{"url": true, "alt": true, "width": true, "height": true, "filename": true}
	seen := make(map[string]bool, len(templates))
	for _, item := range templates {
		if !embedTemplateID.MatchString(item.ID) || seen[item.ID] || item.Name == "" || len([]rune(item.Name)) > 40 || strings.TrimSpace(item.Name) != item.Name || item.Body == "" || len(item.Body) > 2048 || !strings.Contains(item.Body, "{url}") {
			return false
		}
		seen[item.ID] = true
		for _, placeholder := range embedPlaceholder.FindAllString(item.Body, -1) {
			if !known[strings.Trim(placeholder, "{}")] {
				return false
			}
		}
	}
	return true
}

func (a *App) registerEmbedTemplateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/embed-templates", a.listEmbedTemplates)
	mux.HandleFunc("PUT /api/admin/embed-templates", a.putEmbedTemplates)
}

func (a *App) listEmbedTemplates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ok(w, a.embedTemplates())
}

func (a *App) putEmbedTemplates(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var templates []embedTemplate
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&templates) != nil || !validEmbedTemplates(templates) {
		fail(w, 400, "嵌入代码模板无效")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(w, 400, "嵌入代码模板无效")
		return
	}
	data, _ := json.Marshal(templates)
	if err := a.setSetting("embedTemplates", data); err != nil {
		fail(w, 500, "无法保存嵌入代码模板")
		return
	}
	ok(w, templates)
}
