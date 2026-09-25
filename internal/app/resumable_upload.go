package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	resumableChunkLimit = 8 << 20
	resumableTTL        = 24 * time.Hour
	resumableMaxActive  = 32
	resumableMaxRecords = 4096
)

var (
	resumableCreateMu sync.Mutex
	resumableLocksMu  sync.Mutex
	resumableLocks    = make(map[string]*resumableLock)
	resumableID       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

type resumableLock struct {
	mu    sync.Mutex
	users int
}

func lockResumableSession(dir string) func() {
	resumableLocksMu.Lock()
	lock := resumableLocks[dir]
	if lock == nil {
		lock = new(resumableLock)
		resumableLocks[dir] = lock
	}
	lock.users++
	resumableLocksMu.Unlock()
	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		resumableLocksMu.Lock()
		lock.users--
		if lock.users == 0 {
			delete(resumableLocks, dir)
		}
		resumableLocksMu.Unlock()
	}
}

type resumableState struct {
	ID         string   `json:"id"`
	Owner      string   `json:"owner"`
	IP         string   `json:"ip"`
	Filename   string   `json:"filename"`
	Size       int64    `json:"size"`
	SHA256     string   `json:"sha256"`
	Visibility string   `json:"visibility"`
	Alt        string   `json:"alt"`
	Author     string   `json:"author"`
	License    string   `json:"license"`
	Tags       []string `json:"tags"`
	ImageID    string   `json:"imageId,omitempty"`
	ImageUUID  string   `json:"imageUuid,omitempty"`
	Phase      string   `json:"phase"`
	UpdatedAt  int64    `json:"updatedAt"`
	Image      *Image   `json:"image,omitempty"`
}

type resumableCreateRequest struct {
	Filename   string   `json:"filename"`
	Size       int64    `json:"size"`
	SHA256     string   `json:"sha256"`
	Visibility string   `json:"visibility"`
	Alt        string   `json:"alt"`
	Author     string   `json:"author"`
	License    string   `json:"license"`
	Tags       []string `json:"tags"`
}

type resumableResponse struct {
	ID        string `json:"id"`
	Offset    int64  `json:"offset"`
	Size      int64  `json:"size"`
	ChunkSize int64  `json:"chunkSize"`
	ExpiresAt string `json:"expiresAt"`
	Phase     string `json:"phase"`
	Image     *Image `json:"image,omitempty"`
}

func (a *App) registerResumableUploadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/upload/resumable", a.createResumableUpload)
	mux.HandleFunc("GET /api/upload/resumable/{id}", a.getResumableUpload)
	mux.HandleFunc("PUT /api/upload/resumable/{id}/chunk", a.appendResumableChunk)
	mux.HandleFunc("POST /api/upload/resumable/{id}/finalize", a.finalizeResumableUpload)
	mux.HandleFunc("DELETE /api/upload/resumable/{id}", a.deleteResumableUpload)
}

// StartResumableCleanup may be called once from serve startup. Cancellation
// stops the ticker; callers can wait for the returned channel during shutdown.
func (a *App) StartResumableCleanup(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = a.cleanupResumableSessions()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = a.cleanupResumableSessions()
			}
		}
	}()
	return done
}

func (a *App) resumableRoot() string { return filepath.Join(a.DataDir, "resumable") }

func (a *App) resumableAuth(w http.ResponseWriter, r *http.Request) (string, apiKeyPrincipal, bool) {
	if id := a.userID(r); id != "" {
		return "admin:" + id, apiKeyPrincipal{}, true
	}
	key, ok := a.resolveAPIKey(r, "upload:file")
	if !ok {
		fail(w, 401, "缺少有效的 API Key 或登录状态")
		return "", apiKeyPrincipal{}, false
	}
	return "key:" + key.ID, key, true
}

func (a *App) resumablePath(id string) (string, bool) {
	if !resumableID.MatchString(id) {
		return "", false
	}
	return filepath.Join(a.resumableRoot(), id), true
}

func saveResumableState(dir string, state resumableState) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(encoded)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "state.json"))
}

func loadResumableState(dir string) (resumableState, error) {
	var state resumableState
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return state, os.ErrNotExist
	}
	path := filepath.Join(dir, "state.json")
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return state, os.ErrNotExist
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if state.ID != filepath.Base(dir) || state.UpdatedAt <= 0 {
		return state, os.ErrNotExist
	}
	return state, nil
}

func resumableOffset(dir string) (int64, error) {
	info, err := os.Lstat(filepath.Join(dir, "part"))
	if err != nil || !info.Mode().IsRegular() {
		return 0, os.ErrNotExist
	}
	return info.Size(), nil
}

func (a *App) authorizedResumable(w http.ResponseWriter, r *http.Request) (string, resumableState, apiKeyPrincipal, bool) {
	owner, key, ok := a.resumableAuth(w, r)
	if !ok {
		return "", resumableState{}, apiKeyPrincipal{}, false
	}
	dir, valid := a.resumablePath(r.PathValue("id"))
	if !valid {
		fail(w, 404, "上传会话不存在")
		return "", resumableState{}, apiKeyPrincipal{}, false
	}
	state, err := loadResumableState(dir)
	if err != nil || state.Owner != owner || time.Since(time.Unix(state.UpdatedAt, 0)) >= resumableTTL {
		fail(w, 404, "上传会话不存在或已过期")
		return "", resumableState{}, apiKeyPrincipal{}, false
	}
	return dir, state, key, true
}

func responseForResumable(dir string, state resumableState) (resumableResponse, error) {
	offset := state.Size
	if state.Image == nil {
		var err error
		offset, err = resumableOffset(dir)
		if err != nil {
			return resumableResponse{}, err
		}
	}
	return resumableResponse{ID: state.ID, Offset: offset, Size: state.Size, ChunkSize: resumableChunkLimit, ExpiresAt: time.Unix(state.UpdatedAt, 0).Add(resumableTTL).UTC().Format(time.RFC3339), Phase: state.Phase, Image: state.Image}, nil
}

func (a *App) createResumableUpload(w http.ResponseWriter, r *http.Request) {
	owner, key, valid := a.resumableAuth(w, r)
	if !valid {
		return
	}
	var input resumableCreateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, 400, "无效的上传会话请求")
		return
	}
	if input.Visibility == "" {
		input.Visibility = "unlisted"
	}
	if input.Visibility != "public" && input.Visibility != "private" && input.Visibility != "unlisted" {
		fail(w, 400, "visibility 必须为 public、unlisted 或 private")
		return
	}
	if strings.HasPrefix(owner, "key:") && input.Visibility == "public" && !key.Allows("upload:public") {
		fail(w, 403, "API Key 无公开上传权限")
		return
	}
	limit := privateUploadConfig(a).MaxFileSize
	if strings.HasPrefix(owner, "key:") {
		limit = key.MaxFileSize(limit)
	}
	name := filepath.Base(input.Filename)
	if name == "." || name == "" || name != input.Filename || strings.ContainsAny(name, "\x00/\\") || len(name) > 255 || input.Size <= 0 || input.Size > limit {
		fail(w, 400, "文件名或大小无效")
		return
	}
	if len(input.SHA256) != 64 || strings.ToLower(input.SHA256) != input.SHA256 {
		fail(w, 400, "需要小写 SHA-256 摘要")
		return
	}
	if _, err := hex.DecodeString(input.SHA256); err != nil {
		fail(w, 400, "需要小写 SHA-256 摘要")
		return
	}
	var meta Image
	if err := applyUploadMetadata(&meta, input.Alt, input.Author, input.License, input.Tags); err != nil {
		fail(w, 400, err.Error())
		return
	}
	resumableCreateMu.Lock()
	defer resumableCreateMu.Unlock()
	if err := a.cleanupResumableSessionsLocked(); err != nil {
		fail(w, 500, "无法检查上传会话")
		return
	}
	active, records, err := a.countResumableSessions()
	if err != nil || active >= resumableMaxActive || records >= resumableMaxRecords {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "上传会话已满，请稍后重试")
		return
	}
	id, err := newID()
	if err != nil {
		fail(w, 500, "无法创建上传会话")
		return
	}
	dir, _ := a.resumablePath(id)
	if err = os.Mkdir(dir, 0700); err != nil {
		fail(w, 500, "无法创建上传会话")
		return
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	f, openErr := os.OpenFile(filepath.Join(dir, "part"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if openErr != nil {
		err = openErr
		fail(w, 500, "无法创建上传会话")
		return
	}
	err = f.Close()
	state := resumableState{ID: id, Owner: owner, IP: a.clientIP(r), Filename: name, Size: input.Size, SHA256: input.SHA256, Visibility: input.Visibility, Alt: meta.Alt, Author: meta.Author, License: meta.License, Tags: meta.Tags, Phase: "uploading", UpdatedAt: time.Now().Unix()}
	if err == nil {
		err = saveResumableState(dir, state)
	}
	if err != nil {
		fail(w, 500, "无法创建上传会话")
		return
	}
	response, _ := responseForResumable(dir, state)
	w.Header().Set("Cache-Control", "no-store")
	ok(w, response)
}

func (a *App) getResumableUpload(w http.ResponseWriter, r *http.Request) {
	dirPath, validID := a.resumablePath(r.PathValue("id"))
	if !validID {
		fail(w, 404, "上传会话不存在")
		return
	}
	unlock := lockResumableSession(dirPath)
	defer unlock()
	dir, state, _, valid := a.authorizedResumable(w, r)
	if !valid {
		return
	}
	response, err := responseForResumable(dir, state)
	if err != nil {
		fail(w, 500, "无法读取上传进度")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, response)
}

func (a *App) appendResumableChunk(w http.ResponseWriter, r *http.Request) {
	dirPath, validID := a.resumablePath(r.PathValue("id"))
	if !validID {
		fail(w, 404, "上传会话不存在")
		return
	}
	unlock := lockResumableSession(dirPath)
	defer unlock()
	dir, state, _, valid := a.authorizedResumable(w, r)
	if !valid {
		return
	}
	if state.Phase != "uploading" {
		fail(w, 409, "上传会话已完成")
		return
	}
	offset, err := resumableOffset(dir)
	if err != nil {
		fail(w, 500, "无法读取上传进度")
		return
	}
	clientOffset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || clientOffset < 0 {
		fail(w, 400, "缺少有效的 offset")
		return
	}
	if clientOffset != offset {
		w.Header().Set("Upload-Offset", strconv.FormatInt(offset, 10))
		fail(w, 409, "上传偏移不匹配")
		return
	}
	max := min(int64(resumableChunkLimit), state.Size-offset)
	if max <= 0 {
		fail(w, 409, "上传内容已齐全")
		return
	}
	if r.ContentLength > max {
		fail(w, 413, "上传分块超过剩余大小或分块限制")
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "part"), os.O_WRONLY, 0600)
	if err != nil {
		fail(w, 500, "无法写入上传内容")
		return
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		fail(w, 500, "无法写入上传内容")
		return
	}
	n, copyErr := io.Copy(f, http.MaxBytesReader(w, r.Body, max))
	if copyErr != nil || n == 0 {
		_ = f.Truncate(offset)
		if n > 0 {
			fail(w, 413, "上传分块超过剩余大小或分块限制")
		} else {
			fail(w, 400, "上传分块为空或读取失败")
		}
		return
	}
	if err := f.Sync(); err != nil {
		_ = f.Truncate(offset)
		fail(w, 500, "无法保存上传内容")
		return
	}
	state.UpdatedAt = time.Now().Unix()
	if err := saveResumableState(dir, state); err != nil {
		fail(w, 500, "无法保存上传进度")
		return
	}
	response, _ := responseForResumable(dir, state)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Upload-Offset", strconv.FormatInt(response.Offset, 10))
	ok(w, response)
}

func (a *App) deleteResumableUpload(w http.ResponseWriter, r *http.Request) {
	dirPath, validID := a.resumablePath(r.PathValue("id"))
	if !validID {
		fail(w, 404, "上传会话不存在")
		return
	}
	unlock := lockResumableSession(dirPath)
	defer unlock()
	dir, state, _, valid := a.authorizedResumable(w, r)
	if !valid {
		return
	}
	if state.Image != nil {
		fail(w, 409, "图片已保存，不能取消上传会话")
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		fail(w, 500, "无法删除上传会话")
		return
	}
	ok(w, map[string]bool{"deleted": true})
}

func (a *App) finalizeResumableUpload(w http.ResponseWriter, r *http.Request) {
	dirPath, validID := a.resumablePath(r.PathValue("id"))
	if !validID {
		fail(w, 404, "上传会话不存在")
		return
	}
	unlock := lockResumableSession(dirPath)
	defer unlock()
	dir, state, key, valid := a.authorizedResumable(w, r)
	if !valid {
		return
	}
	if state.Image != nil {
		response, _ := responseForResumable(dir, state)
		ok(w, response)
		return
	}
	if state.Phase != "uploading" && state.Phase != "finalizing" {
		fail(w, 409, "上传会话状态无效")
		return
	}
	release, allowed := a.acquireFairUploadSlot(w, r, false)
	if !allowed {
		return
	}
	defer release()
	offset, err := resumableOffset(dir)
	if err != nil || offset != state.Size {
		fail(w, 409, "上传内容尚未齐全")
		return
	}
	f, err := os.Open(filepath.Join(dir, "part"))
	if err != nil {
		fail(w, 500, "无法读取上传内容")
		return
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		fail(w, 500, "无法校验上传内容")
		return
	}
	if hex.EncodeToString(h.Sum(nil)) != state.SHA256 {
		fail(w, 422, "SHA-256 校验失败")
		return
	}
	if state.ImageID == "" {
		state.ImageID, err = newID()
		if err != nil {
			fail(w, 500, "无法创建图片")
			return
		}
	}
	if state.ImageUUID == "" {
		state.ImageUUID, err = newID()
		if err != nil {
			fail(w, 500, "无法创建图片")
			return
		}
	}
	state.Phase = "finalizing"
	state.UpdatedAt = time.Now().Unix()
	if err := saveResumableState(dir, state); err != nil {
		fail(w, 500, "无法保存上传会话")
		return
	}
	if existing, err := a.getImageByUUID(state.ImageUUID); err == nil {
		if existing.ID != state.ImageID {
			fail(w, 409, "上传会话与图片记录冲突")
			return
		}
		state.Image = &existing
		state.Phase = "complete"
		if err := saveResumableState(dir, state); err != nil {
			fail(w, 500, "无法保存上传会话")
			return
		}
		_ = os.Remove(filepath.Join(dir, "part"))
		response, _ := responseForResumable(dir, state)
		ok(w, response)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		fail(w, 500, "无法检查图片记录")
		return
	}
	im, err := a.ingestResumableImage(r.Context(), f, state, key)
	if err != nil {
		state.Phase = "uploading"
		_ = saveResumableState(dir, state)
		if errors.Is(err, errDailyQuotaExceeded) {
			fail(w, 429, "每日上传配额已用尽")
		} else {
			fail(w, 400, err.Error())
		}
		return
	}
	state.Image = &im
	state.Phase = "complete"
	state.UpdatedAt = time.Now().Unix()
	if err := saveResumableState(dir, state); err != nil {
		fail(w, 500, "图片已保存，但无法更新上传会话")
		return
	}
	_ = os.Remove(filepath.Join(dir, "part"))
	response, _ := responseForResumable(dir, state)
	w.Header().Set("Cache-Control", "no-store")
	ok(w, response)
}

func (a *App) ingestResumableImage(ctx context.Context, source *os.File, state resumableState, key apiKeyPrincipal) (Image, error) {
	var im Image
	config := privateUploadConfig(a)
	limit := config.MaxFileSize
	admin := strings.HasPrefix(state.Owner, "admin:")
	if !admin {
		limit = key.MaxFileSize(limit)
		if state.Visibility == "public" && !key.Allows("upload:public") {
			return im, errors.New("API Key 无公开上传权限")
		}
	}
	if state.Size > limit {
		return im, errors.New("文件超过当前大小限制")
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return im, err
	}
	var head [512]byte
	n, err := source.Read(head[:])
	if err != nil && err != io.EOF {
		return im, err
	}
	format := detectFormat(head[:n])
	if format == "" {
		return im, errors.New("不支持的图片内容格式")
	}
	format, err = inspectImageFormat(source, format)
	if err != nil {
		return im, err
	}
	if !admin && !key.AllowsFormat(format) {
		return im, errors.New("API Key 不允许该图片格式")
	}
	_, _ = source.Seek(0, io.SeekStart)
	width, height := 0, 0
	if cfg, _, err := image.DecodeConfig(source); err == nil {
		width, height = cfg.Width, cfg.Height
	}
	processed, processedFormat, size, err := a.processImageFile(ctx, source, format, state.Size, config, 100<<10)
	if err != nil {
		return im, err
	}
	f := processed
	if processed != source {
		defer func() { processed.Close(); os.Remove(processed.Name()) }()
	}
	format = processedFormat
	stripped, strippedSize, err := a.maybeStripJPEGMetadata(f, format, a.lifecycleSettings().StripJPEGMetadata)
	if err != nil {
		return im, err
	}
	if stripped != f {
		defer func() { stripped.Close(); os.Remove(stripped.Name()) }()
		f, size = stripped, strippedSize
	}
	if !admin && !key.AllowsFormat(format) {
		return im, errors.New("API Key 不允许该图片格式")
	}
	digest, err := fileMD5(f)
	if err != nil {
		return im, err
	}
	keyID := ""
	if !admin {
		keyID = key.ID
	}
	duplicate, err := a.findExactDuplicate(f, digest, size, admin, keyID)
	if err != nil {
		return im, err
	}
	filename := state.ImageUUID + "." + format
	dest := filepath.Join(a.DataDir, "uploads", filename)
	// A previous finalize may have created this file just before a crash.
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return im, err
	}
	reservation, err := a.reserveUploadQuota(ctx, key, state.IP, state.ImageID, size)
	if err != nil {
		return im, err
	}
	committed := false
	defer func() { _ = a.finishUploadQuota(context.Background(), reservation, committed) }()
	if err := os.Link(f.Name(), dest); err != nil {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return im, err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return im, err
		}
		_, err = io.Copy(out, f)
		if syncErr := out.Sync(); err == nil {
			err = syncErr
		}
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(dest)
			return im, err
		}
	}
	kind, uploadedBy := "apikey", key.Name
	if admin {
		kind = "private"
		_ = a.DB.QueryRow(`SELECT username FROM users WHERE id=?`, strings.TrimPrefix(state.Owner, "admin:")).Scan(&uploadedBy)
	} else if uploadedBy == "" {
		uploadedBy = "API用户"
	}
	if state.Visibility == "public" {
		kind = "public"
	}
	im = Image{ID: state.ImageID, UUID: state.ImageUUID, Filename: filename, OriginalName: state.Filename, Format: format, Size: size, Width: width, Height: height, UploadedByType: kind, UploadedBy: uploadedBy, UploadedAt: now(), IP: state.IP, APIKeyID: keyID, Visibility: state.Visibility, Alt: state.Alt, Author: state.Author, License: state.License, Tags: state.Tags, MD5: digest, DuplicateOf: duplicate}
	im.UpdatedAt = im.UploadedAt
	im.URL = "/i/" + filename
	if state.Visibility == "public" {
		if publicConfig(a).ContentSafety.Enabled {
			im.ModerationStatus = "pending"
		} else {
			im.ModerationStatus = "skipped"
			im.ModerationChecked = true
		}
	}
	if err := a.saveImage(im); err != nil {
		_ = os.Remove(dest)
		return Image{}, err
	}
	if im.ModerationStatus == "pending" {
		if err := a.enqueueModeration(im); err != nil {
			_, _ = a.DB.Exec(`DELETE FROM images WHERE id=?`, im.ID)
			_ = os.Remove(dest)
			return Image{}, err
		}
	}
	committed = true
	a.enqueueNotification("upload", "图片上传", state.Filename+" 已上传", map[string]any{"id": im.ID, "filename": im.Filename, "url": im.URL, "size": im.Size, "ip": im.IP, "type": im.UploadedByType})
	return im, nil
}

func (a *App) cleanupResumableSessions() error {
	resumableCreateMu.Lock()
	defer resumableCreateMu.Unlock()
	return a.cleanupResumableSessionsLocked()
}

func (a *App) cleanupResumableSessionsLocked() error {
	root := a.resumableRoot()
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	d, err := os.Open(root)
	if err != nil {
		return err
	}
	defer d.Close()
	cutoff := time.Now().Add(-resumableTTL).Unix()
	for {
		names, readErr := d.Readdirnames(128)
		for _, name := range names {
			if !resumableID.MatchString(name) {
				continue
			}
			dir := filepath.Join(root, name)
			unlock := lockResumableSession(dir)
			state, stateErr := loadResumableState(dir)
			if stateErr == nil && state.UpdatedAt > cutoff {
				unlock()
				continue
			}
			info, statErr := os.Lstat(dir)
			if statErr != nil || !info.IsDir() || stateErr != nil && info.ModTime().Unix() > cutoff {
				unlock()
				continue
			}
			if removeErr := os.RemoveAll(dir); removeErr != nil {
				unlock()
				return fmt.Errorf("remove expired resumable session: %w", removeErr)
			}
			unlock()
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func (a *App) countResumableSessions() (active, records int, err error) {
	dir, err := os.Open(a.resumableRoot())
	if err != nil {
		return 0, 0, err
	}
	defer dir.Close()
	for {
		names, readErr := dir.Readdirnames(128)
		for _, name := range names {
			if !resumableID.MatchString(name) {
				continue
			}
			records++
			if records >= resumableMaxRecords {
				return active, records, nil
			}
			state, stateErr := loadResumableState(filepath.Join(a.resumableRoot(), name))
			if stateErr == nil && state.Image == nil {
				active++
			}
		}
		if readErr == io.EOF {
			return active, records, nil
		}
		if readErr != nil {
			return active, records, readErr
		}
	}
}
