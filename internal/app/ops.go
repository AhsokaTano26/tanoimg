package app

import (
	"container/heap"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

const opsMinimumFreeBytes = 64 << 20

// registerOpsRoutes is called by Handler after constructing its ServeMux.
// These routes only read existing state and require no database migration.
func (a *App) registerOpsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /readyz", a.opsReady)
	mux.HandleFunc("GET /api/admin/health", a.opsHealth)
	mux.HandleFunc("GET /api/admin/tasks", a.opsTasks)
	mux.HandleFunc("GET /api/admin/tasks/summary", a.opsTaskSummary)
}

type opsDisk struct {
	AvailableBytes uint64 `json:"availableBytes"`
	TotalBytes     uint64 `json:"totalBytes"`
}

func diskSpace(path string) (opsDisk, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return opsDisk{}, err
	}
	return opsDisk{AvailableBytes: uint64(stat.Bavail) * uint64(stat.Bsize), TotalBytes: uint64(stat.Blocks) * uint64(stat.Bsize)}, nil
}

func (a *App) opsReadiness(ctx context.Context) (opsDisk, error) {
	check, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := a.DB.PingContext(check); err != nil {
		return opsDisk{}, err
	}
	disk, err := diskSpace(a.DataDir)
	if err != nil {
		return opsDisk{}, err
	}
	if disk.AvailableBytes < opsMinimumFreeBytes {
		return disk, errors.New("data volume is almost full")
	}
	return disk, nil
}

func (a *App) opsReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, err := a.opsReadiness(r.Context()); err != nil {
		respond(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	ok(w, map[string]string{"status": "ready"})
}

func queueStatusCounts(ctx context.Context, db *sql.DB, table string) (map[string]int64, error) {
	// table is selected by the caller from fixed internal names, never request input.
	rows, err := db.QueryContext(ctx, "SELECT status,count(*) FROM "+table+" GROUP BY status")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[string]int64)
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

func (a *App) opsHealth(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	disk, err := a.opsReadiness(r.Context())
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "数据卷或数据库暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	moderation, err := queueStatusCounts(ctx, a.DB, "moderation_tasks")
	if err != nil {
		fail(w, 503, "无法读取后台队列")
		return
	}
	notification, err := queueStatusCounts(ctx, a.DB, "notification_events")
	if err != nil {
		fail(w, 503, "无法读取后台队列")
		return
	}
	alerts := make([]string, 0, 5)
	if disk.AvailableBytes < 256<<20 {
		alerts = append(alerts, "low_disk_space")
	}
	if moderation["error"] > 0 {
		alerts = append(alerts, "moderation_errors")
	}
	if notification["error"] > 0 {
		alerts = append(alerts, "notification_errors")
	}
	if moderation["pending"]+moderation["failed"] > 1000 {
		alerts = append(alerts, "moderation_backlog")
	}
	if notification["pending"] > 1000 {
		alerts = append(alerts, "notification_backlog")
	}
	status := "ready"
	if len(alerts) > 0 {
		status = "degraded"
	}
	ok(w, map[string]any{"status": status, "database": "ok", "disk": disk, "queues": map[string]any{"moderation": moderation, "notification": notification}, "alerts": alerts})
}

func opsPage(r *http.Request) (page, limit int, valid bool) {
	page, limit = 1, 20
	if value := r.URL.Query().Get("page"); value != "" {
		var err error
		page, err = strconv.Atoi(value)
		if err != nil {
			return 0, 0, false
		}
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil {
			return 0, 0, false
		}
	}
	return page, limit, page >= 1 && page <= 100 && limit >= 1 && limit <= 100
}

func (a *App) opsTasks(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	page, limit, valid := opsPage(r)
	if !valid {
		fail(w, 400, "分页参数无效")
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "moderation"
	}
	var tasks []map[string]any
	var total int
	var err error
	switch kind {
	case "moderation", "notification":
		tasks, total, err = a.databaseTasks(r.Context(), kind, page, limit)
	case "migration":
		tasks, total, err = a.migrationTasks(r.Context(), page, limit)
	default:
		fail(w, 400, "未知任务类型")
		return
	}
	if err != nil {
		fail(w, 503, "无法读取任务")
		return
	}
	ok(w, map[string]any{"kind": kind, "page": page, "limit": limit, "total": total, "tasks": tasks})
}

func (a *App) databaseTasks(ctx context.Context, kind string, page, limit int) ([]map[string]any, int, error) {
	items := make([]map[string]any, 0, limit)
	var total int
	if kind == "moderation" {
		if err := a.DB.QueryRowContext(ctx, `SELECT count(*) FROM moderation_tasks`).Scan(&total); err != nil {
			return nil, 0, err
		}
		rows, err := a.DB.QueryContext(ctx, `SELECT id,image_id,status,retry_count,next_attempt,error,created_at,updated_at FROM moderation_tasks ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		for rows.Next() {
			var id, imageID, status, errorText, created, updated string
			var retries int
			var next int64
			if err := rows.Scan(&id, &imageID, &status, &retries, &next, &errorText, &created, &updated); err != nil {
				return nil, 0, err
			}
			items = append(items, map[string]any{"id": id, "imageId": imageID, "status": status, "retryCount": retries, "nextAttempt": next, "hasError": errorText != "", "createdAt": created, "updatedAt": updated})
		}
		return items, total, rows.Err()
	}
	if err := a.DB.QueryRowContext(ctx, `SELECT count(*) FROM notification_events`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := a.DB.QueryContext(ctx, `SELECT id,kind,status,retry_count,next_attempt,error FROM notification_events ORDER BY id DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, retries int64
		var next int64
		var eventKind, status, errorText string
		if err := rows.Scan(&id, &eventKind, &status, &retries, &next, &errorText); err != nil {
			return nil, 0, err
		}
		items = append(items, map[string]any{"id": id, "eventKind": eventKind, "status": status, "retryCount": retries, "nextAttempt": next, "hasError": errorText != ""})
	}
	return items, total, rows.Err()
}

type migrationMaxHeap []remoteMigration

func (h migrationMaxHeap) Len() int           { return len(h) }
func (h migrationMaxHeap) Less(i, j int) bool { return h[i].ID > h[j].ID }
func (h migrationMaxHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *migrationMaxHeap) Push(x any)        { *h = append(*h, x.(remoteMigration)) }
func (h *migrationMaxHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func (a *App) scanMigrationStates(ctx context.Context, visit func(remoteMigration)) error {
	root := filepath.Join(a.DataDir, "imports")
	dir, err := os.Open(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		names, err := dir.Readdirnames(256)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		for _, name := range names {
			if !migrationDigest.MatchString(name) {
				continue
			}
			path := filepath.Join(root, name)
			info, statErr := os.Lstat(path)
			if statErr != nil || !info.IsDir() {
				continue
			}
			state, openErr := os.Open(filepath.Join(path, "state.json"))
			if openErr != nil {
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(state, 64<<10+1))
			state.Close()
			if readErr != nil || len(data) > 64<<10 {
				continue
			}
			var m remoteMigration
			if json.Unmarshal(data, &m) != nil || m.ID != name {
				continue
			}
			visit(m)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func (a *App) migrationTasks(ctx context.Context, page, limit int) ([]map[string]any, int, error) {
	end := page * limit // opsPage caps this at 10,000.
	selected := make(migrationMaxHeap, 0, end)
	total := 0
	err := a.scanMigrationStates(ctx, func(m remoteMigration) {
		total++
		if len(selected) < end {
			heap.Push(&selected, m)
		} else if m.ID < selected[0].ID {
			selected[0] = m
			heap.Fix(&selected, 0)
		}
	})
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })
	start := (page - 1) * limit
	items := make([]map[string]any, 0, limit)
	if start < len(selected) {
		for _, m := range selected[start:] {
			items = append(items, map[string]any{"id": m.ID, "phase": m.Phase, "size": m.Size, "hasError": m.Error != ""})
		}
	}
	return items, total, nil
}

func (a *App) opsTaskSummary(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	moderation, err := queueStatusCounts(ctx, a.DB, "moderation_tasks")
	if err != nil {
		fail(w, 503, "无法读取任务摘要")
		return
	}
	notification, err := queueStatusCounts(ctx, a.DB, "notification_events")
	if err != nil {
		fail(w, 503, "无法读取任务摘要")
		return
	}
	migrations := make(map[string]int64)
	if err := a.scanMigrationStates(ctx, func(m remoteMigration) { migrations[m.Phase]++ }); err != nil {
		fail(w, 503, "无法读取任务摘要")
		return
	}
	ok(w, map[string]any{"moderation": moderation, "notification": notification, "migration": migrations})
}
