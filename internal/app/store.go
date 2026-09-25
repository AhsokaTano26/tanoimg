package app

import (
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-webauthn/webauthn/webauthn"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type Config struct {
	DataDir       string
	Version       string
	AdminUsername string
	AdminPassword string
	// InitializeAdmin creates an administrator on a fresh serving instance, never during migration.
	InitializeAdmin bool
	TrustProxy      bool
	PublicURL       string
}

type App struct {
	mediaMu          sync.Mutex
	migrationMu      sync.Mutex
	migrationWG      sync.WaitGroup
	migrationActive  string
	migrationClosed  bool
	authMu           sync.Mutex
	authCipher       cipher.AEAD
	webAuthn         *webauthn.WebAuthn
	publicURL        string
	DB               *sql.DB
	DataDir          string
	Version          string
	limiter          chan struct{}
	uploadScheduler  *fairUploadScheduler
	processing       chan struct{}
	waiters          chan struct{}
	TrustProxy       bool
	urlClient        *http.Client
	moderationOnce   sync.Once
	moderationWake   chan struct{}
	moderationCancel func()
	moderationDone   chan struct{}
	publicMu         sync.Mutex
	publicActive     map[string]bool
	notifyOnce       sync.Once
	notifyWake       chan struct{}
	notifyCancel     func()
	notifyDone       chan struct{}
	notifyEnabled    atomic.Bool
}

type Image struct {
	ID                string   `json:"id"`
	UUID              string   `json:"uuid"`
	Filename          string   `json:"filename"`
	OriginalName      string   `json:"originalName"`
	Format            string   `json:"format"`
	Size              int64    `json:"size"`
	Width             int      `json:"width"`
	Height            int      `json:"height"`
	UploadedBy        string   `json:"uploadedBy"`
	UploadedByType    string   `json:"uploadedByType"`
	UploadedAt        string   `json:"uploadedAt"`
	UpdatedAt         string   `json:"updatedAt"`
	IsDeleted         bool     `json:"isDeleted"`
	IsNsfw            bool     `json:"isNsfw"`
	Visibility        string   `json:"visibility"`
	Alt               string   `json:"alt"`
	Author            string   `json:"author"`
	License           string   `json:"license"`
	Tags              []string `json:"tags"`
	MD5               string   `json:"md5,omitempty"`
	DuplicateOf       string   `json:"duplicateOf,omitempty"`
	Receipt           string   `json:"receipt,omitempty"`
	Revision          int64    `json:"-"`
	ModerationChecked bool     `json:"moderationChecked"`
	ModerationStatus  string   `json:"moderationStatus,omitempty"`
	ModerationScore   float64  `json:"moderationScore,omitempty"`
	SourceURL         string   `json:"sourceUrl,omitempty"`
	IP                string   `json:"-"`
	APIKeyID          string   `json:"-"`
	DeletedAt         string   `json:"deletedAt,omitempty"`
	DeletedBy         string   `json:"deletedBy,omitempty"`
	URL               string   `json:"url"`
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
	if c.Version == "" {
		c.Version = "dev"
	}
	a := &App{DB: db, DataDir: c.DataDir, Version: c.Version, limiter: make(chan struct{}, 4), uploadScheduler: newFairUploadScheduler(4, 32, 16), processing: make(chan struct{}, 1), waiters: make(chan struct{}, 32), TrustProxy: c.TrustProxy, urlClient: newURLClient(), moderationWake: make(chan struct{}, 1), publicActive: make(map[string]bool), notifyWake: make(chan struct{}, 1)}
	for _, q := range []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS images (id TEXT PRIMARY KEY, uuid TEXT NOT NULL UNIQUE, filename TEXT NOT NULL, original_name TEXT NOT NULL DEFAULT '', format TEXT NOT NULL, size INTEGER NOT NULL, width INTEGER NOT NULL DEFAULT 0, height INTEGER NOT NULL DEFAULT 0, uploaded_by TEXT NOT NULL DEFAULT '', uploaded_by_type TEXT NOT NULL DEFAULT 'private', uploaded_at TEXT NOT NULL, updated_at TEXT NOT NULL, is_deleted INTEGER NOT NULL DEFAULT 0, is_nsfw INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS images_gallery ON images(is_deleted, is_nsfw, uploaded_by_type, uploaded_at DESC)`,
		`CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, password TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS apikeys (id TEXT PRIMARY KEY, key TEXT NOT NULL UNIQUE, name TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, is_default INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS public_upload_attempts (ip TEXT PRIMARY KEY, window_start INTEGER NOT NULL, count INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS upload_rates (ip TEXT PRIMARY KEY, window_start INTEGER NOT NULL, count INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS ip_blacklist (id TEXT PRIMARY KEY, ip TEXT NOT NULL UNIQUE, reason TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS source_documents (kind TEXT NOT NULL, id TEXT NOT NULL, doc TEXT NOT NULL, PRIMARY KEY(kind,id))`,
		`CREATE TABLE IF NOT EXISTS moderation_tasks (id TEXT PRIMARY KEY, image_id TEXT NOT NULL, filename TEXT NOT NULL, status TEXT NOT NULL, retry_count INTEGER NOT NULL DEFAULT 0, next_attempt INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS moderation_tasks_queue ON moderation_tasks(status,next_attempt,created_at)`,
		`CREATE TABLE IF NOT EXISTS notification_events (id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, payload TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending', retry_count INTEGER NOT NULL DEFAULT 0, next_attempt INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS notification_events_queue ON notification_events(status,next_attempt,id)`,
	} {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := ensureImageColumns(db); err != nil {
		db.Close()
		return nil, err
	}
	for _, setup := range []func(*App) error{ensureMediaLifecycleSchema, ensureReportSchema, ensureReceiptSchema, ensureShareSchema, ensureIdempotencySchema, ensureAlbumSchema} {
		if err := setup(a); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := a.initAPIKeySecurity(); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureAuditSchema(a); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(filepath.Join(c.DataDir, "tanoimg.db"), 0600); err != nil {
		db.Close()
		return nil, err
	}
	if err := a.initSecurity(c.PublicURL); err != nil {
		db.Close()
		return nil, err
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil {
		db.Close()
		return nil, err
	}
	if count == 0 && (c.InitializeAdmin || c.AdminPassword != "") {
		generated := c.AdminPassword == ""
		if generated {
			c.AdminPassword, err = generateInitialPassword()
			if err != nil {
				db.Close()
				return nil, err
			}
		}
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
		if generated {
			log.Printf("initial administrator: username=%q password=%s", c.AdminUsername, c.AdminPassword)
		}
	}
	a.notifyEnabled.Store(a.notificationSettings().Enabled)
	return a, nil
}

func ensureImageColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(images)`)
	if err != nil {
		return err
	}
	existing := make(map[string]bool)
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, column := range []string{
		"moderation_checked INTEGER NOT NULL DEFAULT 0", "moderation_status TEXT NOT NULL DEFAULT ''", "moderation_score REAL NOT NULL DEFAULT 0",
		"source_url TEXT NOT NULL DEFAULT ''", "ip TEXT NOT NULL DEFAULT ''", "api_key_id TEXT NOT NULL DEFAULT ''",
		"deleted_at TEXT NOT NULL DEFAULT ''", "deleted_by TEXT NOT NULL DEFAULT ''",
		"visibility TEXT NOT NULL DEFAULT 'unlisted'", "alt TEXT NOT NULL DEFAULT ''", "author TEXT NOT NULL DEFAULT ''",
		"license TEXT NOT NULL DEFAULT ''", "tags_json TEXT NOT NULL DEFAULT '[]'", "md5 TEXT NOT NULL DEFAULT ''",
		"revision INTEGER NOT NULL DEFAULT 1",
	} {
		name := strings.SplitN(column, " ", 2)[0]
		if !existing[name] {
			if _, err := db.Exec(`ALTER TABLE images ADD COLUMN ` + column); err != nil {
				return err
			}
		}
	}
	if !existing["visibility"] {
		if _, err := db.Exec(`UPDATE images SET visibility='public' WHERE uploaded_by_type='public'`); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS images_visibility_gallery ON images(visibility,is_deleted,is_nsfw,uploaded_at DESC,id DESC)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS images_md5_size ON images(md5,size)`); err != nil {
		return err
	}
	return nil
}

func (a *App) Close() error {
	a.migrationMu.Lock()
	a.migrationClosed = true
	a.migrationMu.Unlock()
	a.migrationWG.Wait()
	if a.moderationCancel != nil {
		a.moderationCancel()
		<-a.moderationDone
	}
	if a.notifyCancel != nil {
		a.notifyCancel()
		<-a.notifyDone
	}
	return a.DB.Close()
}

func (a *App) setting(key string, fallback any) json.RawMessage {
	var value string
	if err := a.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&value); err == nil && json.Valid([]byte(value)) {
		return json.RawMessage(value)
	}
	b, _ := json.Marshal(fallback)
	return b
}

func (a *App) saveImage(im Image) error {
	if im.Visibility == "" {
		im.Visibility = "unlisted"
		if im.UploadedByType == "public" {
			im.Visibility = "public"
		}
	}
	if im.Tags == nil {
		im.Tags = []string{}
	}
	tags, _ := json.Marshal(im.Tags)
	_, err := a.DB.Exec(`INSERT INTO images(id,uuid,filename,original_name,format,size,width,height,uploaded_by,uploaded_by_type,uploaded_at,updated_at,is_deleted,is_nsfw,moderation_checked,moderation_status,moderation_score,source_url,ip,api_key_id,deleted_at,deleted_by,visibility,alt,author,license,tags_json,md5) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET uuid=excluded.uuid,filename=excluded.filename,original_name=excluded.original_name,format=excluded.format,size=excluded.size,width=excluded.width,height=excluded.height,uploaded_by=excluded.uploaded_by,uploaded_by_type=excluded.uploaded_by_type,uploaded_at=excluded.uploaded_at,updated_at=excluded.updated_at,is_deleted=excluded.is_deleted,is_nsfw=excluded.is_nsfw,moderation_checked=excluded.moderation_checked,moderation_status=excluded.moderation_status,moderation_score=excluded.moderation_score,source_url=excluded.source_url,ip=excluded.ip,api_key_id=excluded.api_key_id,deleted_at=excluded.deleted_at,deleted_by=excluded.deleted_by,visibility=excluded.visibility,alt=excluded.alt,author=excluded.author,license=excluded.license,tags_json=excluded.tags_json,md5=excluded.md5`, im.ID, im.UUID, im.Filename, im.OriginalName, im.Format, im.Size, im.Width, im.Height, im.UploadedBy, im.UploadedByType, im.UploadedAt, im.UpdatedAt, im.IsDeleted, im.IsNsfw, im.ModerationChecked, im.ModerationStatus, im.ModerationScore, im.SourceURL, im.IP, im.APIKeyID, im.DeletedAt, im.DeletedBy, im.Visibility, im.Alt, im.Author, im.License, string(tags), im.MD5)
	return err
}

func scanImage(rows *sql.Rows) (Image, error) {
	var im Image
	var tags string
	err := rows.Scan(&im.ID, &im.UUID, &im.Filename, &im.OriginalName, &im.Format, &im.Size, &im.Width, &im.Height, &im.UploadedBy, &im.UploadedByType, &im.UploadedAt, &im.UpdatedAt, &im.IsDeleted, &im.IsNsfw, &im.ModerationChecked, &im.ModerationStatus, &im.ModerationScore, &im.SourceURL, &im.IP, &im.APIKeyID, &im.DeletedAt, &im.DeletedBy, &im.Visibility, &im.Alt, &im.Author, &im.License, &tags, &im.MD5, &im.Revision)
	if err == nil {
		err = json.Unmarshal([]byte(tags), &im.Tags)
	}
	if im.Tags == nil {
		im.Tags = []string{}
	}
	im.URL = "/i/" + im.Filename
	return im, err
}

const imageColumns = `id,uuid,filename,original_name,format,size,width,height,uploaded_by,uploaded_by_type,uploaded_at,updated_at,is_deleted,is_nsfw,moderation_checked,moderation_status,moderation_score,source_url,ip,api_key_id,deleted_at,deleted_by,visibility,alt,author,license,tags_json,md5,revision`

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

// Rejection sampling keeps all accepted 12-character passwords equally likely.
func generateInitialPassword() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%*-_+=?"
	for {
		password := make([]byte, 12)
		var classes uint8
		for i := range password {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			if err != nil {
				return "", err
			}
			c := alphabet[n.Int64()]
			password[i] = c
			switch {
			case c >= 'a' && c <= 'z':
				classes |= 1
			case c >= 'A' && c <= 'Z':
				classes |= 2
			case c >= '0' && c <= '9':
				classes |= 4
			default:
				classes |= 8
			}
		}
		if classes == 15 {
			return string(password), nil
		}
	}
}
