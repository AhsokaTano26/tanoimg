package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type Config struct {
	DataDir       string
	AdminUsername string
	AdminPassword string
	TrustProxy    bool
}

type App struct {
	DB         *sql.DB
	DataDir    string
	limiter    chan struct{}
	waiters    chan struct{}
	TrustProxy bool
	urlClient  *http.Client
}

type Image struct {
	ID             string `json:"id"`
	UUID           string `json:"uuid"`
	Filename       string `json:"filename"`
	OriginalName   string `json:"originalName"`
	Format         string `json:"format"`
	Size           int64  `json:"size"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	UploadedBy     string `json:"uploadedBy"`
	UploadedByType string `json:"uploadedByType"`
	UploadedAt     string `json:"uploadedAt"`
	UpdatedAt      string `json:"updatedAt"`
	IsDeleted      bool   `json:"isDeleted"`
	IsNsfw         bool   `json:"isNsfw"`
	URL            string `json:"url"`
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[:8], s[8:12], s[12:16], s[16:20], s[20:]), nil
}

func New(c Config) (*App, error) {
	if c.DataDir == "" {
		c.DataDir = "data"
	}
	if err := os.MkdirAll(filepath.Join(c.DataDir, "uploads"), 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(c.DataDir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(c.DataDir, "tanoimg.db"))+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	a := &App{DB: db, DataDir: c.DataDir, limiter: make(chan struct{}, 4), waiters: make(chan struct{}, 32), TrustProxy: c.TrustProxy, urlClient: newURLClient()}
	for _, q := range []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS images (id TEXT PRIMARY KEY, uuid TEXT NOT NULL UNIQUE, filename TEXT NOT NULL, original_name TEXT NOT NULL DEFAULT '', format TEXT NOT NULL, size INTEGER NOT NULL, width INTEGER NOT NULL DEFAULT 0, height INTEGER NOT NULL DEFAULT 0, uploaded_by TEXT NOT NULL DEFAULT '', uploaded_by_type TEXT NOT NULL DEFAULT 'private', uploaded_at TEXT NOT NULL, updated_at TEXT NOT NULL, is_deleted INTEGER NOT NULL DEFAULT 0, is_nsfw INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS images_gallery ON images(is_deleted, is_nsfw, uploaded_by_type, uploaded_at DESC)`,
		`CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, password TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS apikeys (id TEXT PRIMARY KEY, key TEXT NOT NULL UNIQUE, name TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, is_default INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS upload_rates (ip TEXT PRIMARY KEY, window_start INTEGER NOT NULL, count INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS ip_blacklist (id TEXT PRIMARY KEY, ip TEXT NOT NULL UNIQUE, reason TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS source_documents (kind TEXT NOT NULL, id TEXT NOT NULL, doc TEXT NOT NULL, PRIMARY KEY(kind,id))`,
	} {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := os.Chmod(filepath.Join(c.DataDir, "tanoimg.db"), 0600); err != nil {
		db.Close()
		return nil, err
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil {
		db.Close()
		return nil, err
	}
	if count == 0 && c.AdminPassword != "" {
		if c.AdminUsername == "" {
			c.AdminUsername = "admin"
		}
		id, err := newID()
		if err != nil {
			db.Close()
			return nil, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(c.AdminPassword), bcrypt.DefaultCost)
		if err != nil {
			db.Close()
			return nil, err
		}
		if _, err := db.Exec(`INSERT INTO users(id,username,password) VALUES(?,?,?)`, id, c.AdminUsername, string(hash)); err != nil {
			db.Close()
			return nil, err
		}
	}
	return a, nil
}

func (a *App) Close() error { return a.DB.Close() }

func (a *App) setting(key string, fallback any) json.RawMessage {
	var value string
	if err := a.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&value); err == nil && json.Valid([]byte(value)) {
		return json.RawMessage(value)
	}
	b, _ := json.Marshal(fallback)
	return b
}

func (a *App) saveImage(im Image) error {
	_, err := a.DB.Exec(`INSERT INTO images(id,uuid,filename,original_name,format,size,width,height,uploaded_by,uploaded_by_type,uploaded_at,updated_at,is_deleted,is_nsfw) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET uuid=excluded.uuid,filename=excluded.filename,original_name=excluded.original_name,format=excluded.format,size=excluded.size,width=excluded.width,height=excluded.height,uploaded_by=excluded.uploaded_by,uploaded_by_type=excluded.uploaded_by_type,uploaded_at=excluded.uploaded_at,updated_at=excluded.updated_at,is_deleted=excluded.is_deleted,is_nsfw=excluded.is_nsfw`, im.ID, im.UUID, im.Filename, im.OriginalName, im.Format, im.Size, im.Width, im.Height, im.UploadedBy, im.UploadedByType, im.UploadedAt, im.UpdatedAt, im.IsDeleted, im.IsNsfw)
	return err
}

func scanImage(rows *sql.Rows) (Image, error) {
	var im Image
	err := rows.Scan(&im.ID, &im.UUID, &im.Filename, &im.OriginalName, &im.Format, &im.Size, &im.Width, &im.Height, &im.UploadedBy, &im.UploadedByType, &im.UploadedAt, &im.UpdatedAt, &im.IsDeleted, &im.IsNsfw)
	im.URL = "/i/" + im.Filename
	return im, err
}

const imageColumns = `id,uuid,filename,original_name,format,size,width,height,uploaded_by,uploaded_by_type,uploaded_at,updated_at,is_deleted,is_nsfw`

func (a *App) getImageByUUID(uuid string) (Image, error) {
	rows, err := a.DB.Query(`SELECT `+imageColumns+` FROM images WHERE uuid=?`, uuid)
	if err != nil {
		return Image{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Image{}, sql.ErrNoRows
	}
	return scanImage(rows)
}

func (a *App) setSetting(key string, value json.RawMessage) error {
	if !json.Valid(value) {
		return errors.New("invalid JSON")
	}
	_, err := a.DB.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, string(value))
	return err
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
