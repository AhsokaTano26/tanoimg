package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestImageSelectionIDsRequireAdminAndRespectFilters(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	tx, err := a.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1003; i++ {
		id := fmt.Sprintf("selection-%04d", i)
		deleted, nsfw := 0, 0
		if i == 1001 {
			deleted = 1
		}
		if i == 1002 {
			nsfw = 1
		}
		_, err = tx.Exec(`INSERT INTO images(id,uuid,filename,format,size,original_name,visibility,tags_json,is_deleted,is_nsfw,uploaded_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, id, id+".png", "png", 1, "match_100%.png", "private", `["Test"]`, deleted, nsfw, now(), now())
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if rec := adminRequest(a, "", http.MethodGet, "/api/images?selection=all", ""); rec.Code != 401 {
		t.Fatalf("guest: %d", rec.Code)
	}
	read := func(query string) ([]string, string) {
		t.Helper()
		rec := adminRequest(a, token, http.MethodGet, query, "")
		if rec.Code != 200 {
			t.Fatalf("query: %d %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Data struct {
				IDs  []string `json:"ids"`
				Next string   `json:"nextCursor"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Data.IDs, body.Data.Next
	}
	query := "/api/images?selection=all&visibility=private&format=png&tag=test&q=match_100%25"
	ids, next := read(query)
	if len(ids) != 1000 || next != ids[999] {
		t.Fatalf("first page: %d %q", len(ids), next)
	}
	tail, next := read(query + "&after=" + next)
	if len(tail) != 1 || tail[0] != "selection-1000" || next != "" {
		t.Fatalf("tail: %v %q", tail, next)
	}
	empty, next := read("/api/images?selection=all&visibility=public")
	if len(empty) != 0 || next != "" {
		t.Fatalf("visibility leaked: %v", empty)
	}
	empty, _ = read("/api/images?selection=all&q=nonexistent")
	if len(empty) != 0 {
		t.Fatalf("search leaked: %v", empty)
	}
}
