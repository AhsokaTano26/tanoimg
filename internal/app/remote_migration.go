package app

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

const migrationChunkSize = 8 << 20

var migrationDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var migrationDatabases = map[string]bool{"images.db": true, "users.db": true, "apikeys.db": true, "settings.db": true, "moderation_tasks.db": true, "ip_blacklist.db": true}

type remoteMigration struct {
	ID     string           `json:"id"`
	Size   int64            `json:"size"`
	Owner  string           `json:"owner"`
	Phase  string           `json:"phase"`
	Error  string           `json:"error,omitempty"`
	Report *MigrationReport `json:"report,omitempty"`
}

func (a *App) migrationDir(id string) string { return filepath.Join(a.DataDir, "imports", id) }
func (a *App) saveMigration(m *remoteMigration) error {
	dir := a.migrationDir(m.ID)
	f, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(m); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(dir, "state.json"))
}
func (a *App) readMigration(id string) (*remoteMigration, error) {
	if !migrationDigest.MatchString(id) {
		return nil, fmt.Errorf("invalid import ID")
	}
	data, err := os.ReadFile(filepath.Join(a.migrationDir(id), "state.json"))
	if err != nil {
		return nil, err
	}
	var m remoteMigration
	if err = json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.ID != id {
		return nil, fmt.Errorf("invalid import state")
	}
	if m.Phase == "preparing" && a.migrationActive != id {
		m.Phase = "failed"
		m.Error = "准备过程被重启中断，请重新完成迁移"
		if err = a.saveMigration(&m); err != nil {
			return nil, err
		}
	}
	return &m, nil
}
func (a *App) migrationStatus(m *remoteMigration) map[string]any {
	var offset int64
	if m.Phase == "ready" {
		offset = m.Size
	} else if st, err := os.Stat(filepath.Join(a.migrationDir(m.ID), "archive.tar")); err == nil {
		offset = st.Size()
	}
	result := map[string]any{"id": m.ID, "size": m.Size, "offset": offset, "phase": m.Phase, "chunkSize": migrationChunkSize, "error": m.Error}
	if m.Phase == "ready" {
		dir, _ := filepath.Abs(filepath.Join(a.migrationDir(m.ID), "ready"))
		result["dataDir"] = dir
		result["report"] = m.Report
	}
	return result
}

// Session authentication deliberately excludes upload-only API keys. All writes
// are confined to imports/<digest>; the live database and image files stay intact.
func (a *App) remoteMigrationHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	owner := a.userID(r)
	if owner == "" {
		fail(w, 401, "请使用管理员账户登录，上传 API Key 无法执行迁移")
		return
	}
	a.migrationMu.Lock()
	defer a.migrationMu.Unlock()
	if a.migrationClosed {
		fail(w, 503, "服务正在关闭")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		if r.Method == http.MethodGet {
			ok(w, map[string]any{"protocol": 1, "chunkSize": migrationChunkSize})
			return
		}
		var body struct {
			SHA256 string `json:"sha256"`
			Size   int64  `json:"size"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil || !migrationDigest.MatchString(body.SHA256) || body.Size <= 0 || body.Size > 1<<40 {
			fail(w, 400, "请提供有效的 SHA-256 和归档大小（最多 1 TiB）")
			return
		}
		id = body.SHA256
		m, err := a.readMigration(id)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			fail(w, 500, "无法读取迁移状态")
			return
		}
		if errors.Is(err, os.ErrNotExist) {
			if err = os.MkdirAll(a.migrationDir(id), 0700); err != nil {
				fail(w, 500, "无法创建迁移目录")
				return
			}
			m = &remoteMigration{ID: id, Size: body.Size, Owner: owner, Phase: "uploading"}
			if err = a.saveMigration(m); err != nil {
				fail(w, 500, "无法保存迁移状态")
				return
			}
		}
		if m.Owner != owner {
			fail(w, 403, "此迁移属于另一个管理员")
			return
		}
		if m.Size != body.Size {
			fail(w, 409, "归档大小与已有迁移不一致")
			return
		}
		ok(w, a.migrationStatus(m))
		return
	}
	m, err := a.readMigration(id)
	if err != nil {
		fail(w, 404, "迁移不存在")
		return
	}
	if m.Owner != owner {
		fail(w, 403, "此迁移属于另一个管理员")
		return
	}
	switch {
	case r.Method == http.MethodDelete:
		if m.Phase == "ready" || m.Phase == "preparing" {
			fail(w, 409, "不能删除已准备好或正在准备的数据")
			return
		}
		if err = os.RemoveAll(a.migrationDir(id)); err != nil {
			fail(w, 500, "无法清理暂存数据")
			return
		}
		ok(w, nil)
	case r.Method == http.MethodGet:
		ok(w, a.migrationStatus(m))
	case r.Method == http.MethodPut:
		if m.Phase != "uploading" {
			fail(w, 409, "迁移已进入准备阶段，不能继续上传")
			return
		}
		offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		if err != nil || offset < 0 {
			fail(w, 400, "无效偏移量")
			return
		}
		if r.ContentLength > migrationChunkSize {
			fail(w, 413, "分块不能超过 8 MiB")
			return
		}
		f, err := os.OpenFile(filepath.Join(a.migrationDir(id), "archive.tar"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			fail(w, 500, "无法写入迁移文件")
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			fail(w, 500, "无法读取上传进度")
			return
		}
		if st.Size() != offset {
			fail(w, 409, "偏移量冲突，请重新读取上传进度")
			return
		}
		if offset >= m.Size {
			fail(w, 409, "归档已经上传完成")
			return
		}
		if _, err = f.Seek(offset, io.SeekStart); err != nil {
			fail(w, 500, "无法定位上传位置")
			return
		}
		limit := int64(migrationChunkSize)
		if m.Size-offset < limit {
			limit = m.Size - offset
		}
		n, copyErr := io.Copy(f, http.MaxBytesReader(w, r.Body, limit))
		if copyErr != nil || n == 0 || (r.ContentLength >= 0 && n != r.ContentLength) {
			if err = f.Truncate(offset); err != nil {
				fail(w, 500, "回滚分块失败")
				return
			}
			fail(w, 400, "分块不完整或超过剩余大小，请重试")
			return
		}
		if err = f.Sync(); err != nil {
			f.Truncate(offset)
			fail(w, 500, "保存分块失败")
			return
		}
		ok(w, a.migrationStatus(m))
	case r.Method == http.MethodPost:
		if m.Phase == "ready" {
			ok(w, a.migrationStatus(m))
			return
		}
		if a.migrationActive != "" {
			if a.migrationActive == id {
				respond(w, 202, map[string]any{"success": true, "data": a.migrationStatus(m)})
				return
			}
			fail(w, 409, "另一个迁移正在准备，请等待完成")
			return
		}
		st, err := os.Stat(filepath.Join(a.migrationDir(id), "archive.tar"))
		if err != nil || st.Size() != m.Size {
			fail(w, 409, "归档尚未完整上传")
			return
		}
		m.Phase = "preparing"
		m.Error = ""
		if err = a.saveMigration(m); err != nil {
			fail(w, 500, "无法保存迁移状态")
			return
		}
		a.migrationActive = id
		a.migrationWG.Add(1)
		go a.prepareRemoteMigration(*m)
		respond(w, 202, map[string]any{"success": true, "data": a.migrationStatus(m)})
	}
}

func extractMigrationArchive(archive, source string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	budget := st.Size()
	for _, dir := range []string{"db", "uploads"} {
		if err = os.MkdirAll(filepath.Join(source, dir), 0700); err != nil {
			return err
		}
	}
	tr := tar.NewReader(f)
	count := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := h.Name
		if h.Typeflag == tar.TypeDir && (name == "db/" || name == "uploads/") {
			continue
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			return fmt.Errorf("归档只能包含普通文件")
		}
		dir, base := filepath.Split(name)
		if !(dir == "db/" && migrationDatabases[base]) && !(dir == "uploads/" && safeFilename.MatchString(base)) {
			return fmt.Errorf("归档包含不允许的路径")
		}
		if h.Size < 0 || h.Size > budget {
			return fmt.Errorf("归档声明大小超过实际归档大小")
		}
		budget -= h.Size
		count++
		if count > 1_000_000 {
			return fmt.Errorf("归档文件数量超限")
		}
		parent := filepath.Join(source, dir)
		if err = os.MkdirAll(parent, 0700); err != nil {
			return err
		}
		out, err := os.OpenFile(filepath.Join(parent, base), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("归档有重复文件或无法创建文件: %w", err)
		}
		_, err = io.Copy(out, tr)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
}
func (a *App) prepareRemoteMigration(m remoteMigration) {
	defer a.migrationWG.Done()
	dir := a.migrationDir(m.ID)
	source, ready := filepath.Join(dir, "source"), filepath.Join(dir, "ready")
	report, err := func() (MigrationReport, error) {
		var report MigrationReport
		f, err := os.Open(filepath.Join(dir, "archive.tar"))
		if err != nil {
			return report, err
		}
		hash := sha256.New()
		n, err := io.Copy(hash, f)
		f.Close()
		if err != nil {
			return report, err
		}
		if n != m.Size || hex.EncodeToString(hash.Sum(nil)) != m.ID {
			return report, fmt.Errorf("SHA-256 校验失败，请重置本次暂存上传后重传")
		}
		// These are private scratch directories, never the currently served dataset.
		if err = os.RemoveAll(source); err != nil {
			return report, err
		}
		if err = os.RemoveAll(ready); err != nil {
			return report, err
		}
		if err = extractMigrationArchive(filepath.Join(dir, "archive.tar"), source); err != nil {
			return report, err
		}
		prepared, err := New(Config{DataDir: ready})
		if err != nil {
			return report, err
		}
		defer prepared.Close()
		files, err := os.ReadDir(filepath.Join(source, "uploads"))
		if err != nil {
			return report, err
		}
		for _, file := range files {
			src, dst := filepath.Join(source, "uploads", file.Name()), filepath.Join(ready, "uploads", file.Name())
			if err = os.Link(src, dst); err != nil {
				if err = copyIfMissing(src, dst); err != nil {
					return report, err
				}
			}
		}
		report, err = prepared.MigrateEasyImg(source)
		if err != nil {
			return report, err
		}
		if report.MissingFiles != 0 {
			return report, fmt.Errorf("迁移包含 %d 个缺失原图", report.MissingFiles)
		}
		var users int
		if err = prepared.DB.QueryRow(`SELECT count(*) FROM users`).Scan(&users); err != nil {
			return report, err
		}
		if users == 0 {
			return report, fmt.Errorf("备份没有有效管理员账户")
		}
		var check string
		if err = prepared.DB.QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil {
			return report, err
		}
		if check != "ok" {
			return report, fmt.Errorf("SQLite 完整性检查失败")
		}
		return report, nil
	}()
	a.migrationMu.Lock()
	defer a.migrationMu.Unlock()
	a.migrationActive = ""
	if err != nil {
		m.Phase = "failed"
		m.Error = err.Error()
	} else {
		m.Phase = "ready"
		m.Report = &report
	}
	if saveErr := a.saveMigration(&m); saveErr != nil {
		return
	}
	if err == nil {
		os.Remove(filepath.Join(dir, "archive.tar"))
		os.RemoveAll(source)
	}
}
