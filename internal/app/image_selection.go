package app

import (
	"net/http"
	"strings"
)

// Only IDs are fetched, in bounded cursor pages, for selecting a large library.
func (a *App) imageSelectionIDs(w http.ResponseWriter, r *http.Request, where string, args []any) {
	after := r.URL.Query().Get("after")
	if len(after) > 200 || strings.ContainsRune(after, 0) {
		fail(w, 400, "无效选择游标")
		return
	}
	queryArgs := append(append([]any(nil), args...), after)
	rows, err := a.DB.QueryContext(r.Context(), `SELECT id FROM images WHERE `+where+` AND id>? ORDER BY id LIMIT 1001`, queryArgs...)
	if err != nil {
		fail(w, 500, "读取选择列表失败")
		return
	}
	defer rows.Close()
	ids := make([]string, 0, 1000)
	next := ""
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			fail(w, 500, "读取选择列表失败")
			return
		}
		if len(ids) == 1000 {
			next = ids[len(ids)-1]
			break
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		fail(w, 500, "读取选择列表失败")
		return
	}
	ok(w, map[string]any{"ids": ids, "nextCursor": next})
}
