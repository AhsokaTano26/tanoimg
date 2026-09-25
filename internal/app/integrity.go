package app

import (
	"container/heap"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// registerIntegrityRoutes is called by Handler after constructing its mux.
// Scans never mutate image records or files.
func (a *App) registerIntegrityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/integrity", a.scanIntegrity)
}

func integrityLimit(r *http.Request) (int, bool) {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return 100, true
	}
	limit, err := strconv.Atoi(value)
	return limit, err == nil && limit >= 1 && limit <= 200
}

func (a *App) scanIntegrity(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	limit, valid := integrityLimit(r)
	if !valid {
		fail(w, 400, "扫描分页参数无效")
		return
	}
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 512 || strings.ContainsRune(cursor, 0) {
		fail(w, 400, "扫描游标无效")
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "images"
	}
	if kind == "uploads" && cursor != "" && (len(cursor) > 255 || filepath.Base(cursor) != cursor || cursor == "." || cursor == "..") {
		fail(w, 400, "扫描游标无效")
		return
	}
	var result map[string]any
	var err error
	switch kind {
	case "images":
		result, err = a.scanImageRecords(r.Context(), cursor, limit)
	case "uploads":
		result, err = a.scanUploadDirectory(r.Context(), cursor, limit)
	default:
		fail(w, 400, "未知扫描类型")
		return
	}
	if err != nil {
		fail(w, 503, "完整性扫描失败")
		return
	}
	result["kind"] = kind
	ok(w, result)
}

type imageReference struct {
	id       string
	uuid     string
	filename string
	size     int64
}

func (a *App) scanImageRecords(ctx context.Context, cursor string, limit int) (map[string]any, error) {
	rows, err := a.DB.QueryContext(ctx, `SELECT id,uuid,filename,size FROM images WHERE id>? ORDER BY id LIMIT ?`, cursor, limit+1)
	if err != nil {
		return nil, err
	}
	references := make([]imageReference, 0, limit+1)
	for rows.Next() {
		var ref imageReference
		if err = rows.Scan(&ref.id, &ref.uuid, &ref.filename, &ref.size); err != nil {
			rows.Close()
			return nil, err
		}
		references = append(references, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	done := len(references) <= limit
	if !done {
		references = references[:limit]
	}
	findings := make([]map[string]any, 0)
	nextCursor := cursor
	for _, ref := range references {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nextCursor = ref.id
		finding := map[string]any{"id": ref.id, "uuid": ref.uuid, "filename": ref.filename}
		if !safeFilename.MatchString(ref.filename) || !strings.HasPrefix(ref.filename, ref.uuid+".") {
			finding["issue"] = "invalid_filename"
			findings = append(findings, finding)
			continue
		}
		info, err := os.Lstat(filepath.Join(a.DataDir, "uploads", ref.filename))
		if errors.Is(err, os.ErrNotExist) {
			finding["issue"] = "missing"
		} else if err != nil {
			return nil, err
		} else if info.Mode()&os.ModeSymlink != 0 {
			finding["issue"] = "symlink"
		} else if !info.Mode().IsRegular() {
			finding["issue"] = "not_regular"
		} else if info.Size() != ref.size {
			finding["issue"] = "size_mismatch"
			finding["expectedSize"] = ref.size
			finding["actualSize"] = info.Size()
		}
		if finding["issue"] != nil {
			findings = append(findings, finding)
		}
	}
	return map[string]any{"scanned": len(references), "findings": findings, "nextCursor": nextCursor, "done": done}, nil
}

type integrityNameHeap []string

func (h integrityNameHeap) Len() int           { return len(h) }
func (h integrityNameHeap) Less(i, j int) bool { return h[i] > h[j] }
func (h integrityNameHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *integrityNameHeap) Push(x any)        { *h = append(*h, x.(string)) }
func (h *integrityNameHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func nextUploadNames(ctx context.Context, root, cursor string, capacity int) ([]string, error) {
	dir, err := os.Open(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	selected := make(integrityNameHeap, 0, capacity)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		names, readErr := dir.Readdirnames(256)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		for _, name := range names {
			if name <= cursor {
				continue
			}
			if len(selected) < capacity {
				heap.Push(&selected, name)
			} else if name < selected[0] {
				selected[0] = name
				heap.Fix(&selected, 0)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	sort.Strings(selected)
	return selected, nil
}

func temporaryUploadName(name string) bool {
	for _, prefix := range []string{".tanoimg-upload-", ".tanoimg-url-", ".tanoimg-process-", ".migrate-"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func (a *App) scanUploadDirectory(ctx context.Context, cursor string, limit int) (map[string]any, error) {
	root := filepath.Join(a.DataDir, "uploads")
	names, err := nextUploadNames(ctx, root, cursor, limit+1)
	if err != nil {
		return nil, err
	}
	done := len(names) <= limit
	if !done {
		names = names[:limit]
	}
	findings := make([]map[string]any, 0)
	nextCursor := cursor
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nextCursor = name
		finding := map[string]any{"filename": name}
		info, err := os.Lstat(filepath.Join(root, name))
		if errors.Is(err, os.ErrNotExist) {
			// A concurrent upload or deletion can remove an entry during a scan.
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			finding["issue"] = "symlink"
		} else if !info.Mode().IsRegular() {
			finding["issue"] = "not_regular"
		} else if temporaryUploadName(name) {
			continue
		} else if !safeFilename.MatchString(name) {
			finding["issue"] = "orphan"
		} else {
			uuid := strings.TrimSuffix(name, filepath.Ext(name))
			var recorded string
			err = a.DB.QueryRowContext(ctx, `SELECT filename FROM images WHERE uuid=?`, uuid).Scan(&recorded)
			if errors.Is(err, sql.ErrNoRows) || err == nil && recorded != name {
				finding["issue"] = "orphan"
			} else if err != nil {
				return nil, err
			}
		}
		if finding["issue"] != nil {
			findings = append(findings, finding)
		}
	}
	return map[string]any{"scanned": len(names), "findings": findings, "nextCursor": nextCursor, "done": done}, nil
}
