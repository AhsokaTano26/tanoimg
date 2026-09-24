package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type MigrationReport struct {
	Images          int `json:"images"`
	Files           int `json:"files"`
	MissingFiles    int `json:"missingFiles"`
	Users           int `json:"users"`
	APIKeys         int `json:"apiKeys"`
	Settings        int `json:"settings"`
	ModerationTasks int `json:"moderationTasks"`
	BlacklistedIPs  int `json:"blacklistedIPs"`
}

var safeFilename = regexp.MustCompile(`^[a-fA-F0-9-]{32,36}\.[a-zA-Z0-9]+$`)

// MigrateEasyImg reads NeDB's append-only JSON lines without loading the whole file.
// Later versions of a document replace earlier versions, as in NeDB.
func (a *App) MigrateEasyImg(root string) (MigrationReport, error) {
	var report MigrationReport
	if root == "" {
		return report, fmt.Errorf("easyimg directory is required")
	}
	if _, err := os.Stat(filepath.Join(root, "db", "images.db")); err != nil {
		return report, fmt.Errorf("expected db/images.db: %w", err)
	}
	for _, kind := range []string{"users", "apikeys", "settings", "images", "moderation_tasks", "ip_blacklist"} {
		path := filepath.Join(root, "db", kind+".db")
		f, err := os.Open(path)
		if os.IsNotExist(err) && kind != "images" {
			continue
		}
		if err != nil {
			return report, err
		}
		s := bufio.NewScanner(f)
		s.Buffer(make([]byte, 64*1024), 16*1024*1024)
		line := 0
		for s.Scan() {
			line++
			var doc map[string]json.RawMessage
			if err := json.Unmarshal(s.Bytes(), &doc); err != nil {
				f.Close()
				return report, fmt.Errorf("%s:%d: %w", path, line, err)
			}
			if doc["$$indexCreated"] != nil || doc["$$indexRemoved"] != nil {
				continue
			}
			id := str(doc["_id"])
			if id == "" {
				continue
			}
			if boolVal(doc["$$deleted"]) {
				if err := a.removeMigratedDocument(kind, id); err != nil {
					f.Close()
					return report, err
				}
				continue
			}
			switch kind {
			case "users":
				username, password := str(doc["username"]), str(doc["password"])
				if username == "" || password == "" {
					continue
				}
				_, err = a.DB.Exec(`INSERT INTO users(id,username,password) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET username=excluded.username,password=excluded.password`, id, username, password)
				report.Users++
			case "apikeys":
				key := str(doc["key"])
				if key == "" {
					continue
				}
				enabled := true
				if doc["enabled"] != nil {
					enabled = boolVal(doc["enabled"])
				}
				_, err = a.DB.Exec(`INSERT INTO apikeys(id,key,name,enabled,is_default,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET key=excluded.key,name=excluded.name,enabled=excluded.enabled,is_default=excluded.is_default`, id, key, str(doc["name"]), enabled, boolVal(doc["isDefault"]), str(doc["createdAt"]))
				report.APIKeys++
			case "settings":
				key := str(doc["key"])
				if key == "" || doc["value"] == nil {
					continue
				}
				err = a.setSetting(key, doc["value"])
				report.Settings++
			case "images":
				filename, uuid := str(doc["filename"]), str(doc["uuid"])
				if !safeFilename.MatchString(filename) || uuid == "" || !strings.HasPrefix(filename, uuid+".") {
					f.Close()
					return report, fmt.Errorf("%s:%d: invalid image filename", path, line)
				}
				im := Image{ID: id, UUID: uuid, Filename: filename, OriginalName: str(doc["originalName"]), Format: str(doc["format"]), Size: intVal(doc["size"]), Width: int(intVal(doc["width"])), Height: int(intVal(doc["height"])), UploadedBy: str(doc["uploadedBy"]), UploadedByType: str(doc["uploadedByType"]), UploadedAt: str(doc["uploadedAt"]), UpdatedAt: str(doc["updatedAt"]), IsDeleted: boolVal(doc["isDeleted"]), IsNsfw: boolVal(doc["isNsfw"]), ModerationChecked: boolVal(doc["moderationChecked"]), ModerationStatus: str(doc["moderationStatus"]), SourceURL: str(doc["sourceUrl"]), IP: str(doc["ip"]), APIKeyID: str(doc["apiKeyId"]), DeletedAt: str(doc["deletedAt"]), DeletedBy: str(doc["deletedBy"])}
				var moderationResult map[string]json.RawMessage
				json.Unmarshal(doc["moderationResult"], &moderationResult)
				json.Unmarshal(moderationResult["score"], &im.ModerationScore)
				if im.Format == "" {
					im.Format = strings.TrimPrefix(filepath.Ext(filename), ".")
				}
				if im.UploadedByType == "" {
					if boolVal(doc["isPublic"]) {
						im.UploadedByType = "public"
					} else {
						im.UploadedByType = "private"
					}
				}
				if im.UploadedAt == "" {
					im.UploadedAt = now()
				}
				if im.UpdatedAt == "" {
					im.UpdatedAt = im.UploadedAt
				}
				if err = copyIfMissing(filepath.Join(root, "uploads", filename), filepath.Join(a.DataDir, "uploads", filename)); err != nil {
					if os.IsNotExist(err) {
						report.MissingFiles++
					} else {
						f.Close()
						return report, err
					}
				} else {
					report.Files++
				}
				if err = a.saveImage(im); err == nil {
					report.Images++
				}
			case "moderation_tasks":
				report.ModerationTasks++
			case "ip_blacklist":
				ip := str(doc["ip"])
				if ip == "" {
					continue
				}
				_, err = a.DB.Exec(`INSERT INTO ip_blacklist(id,ip,reason,created_at) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET ip=excluded.ip,reason=excluded.reason,created_at=excluded.created_at`, id, ip, str(doc["reason"]), str(doc["createdAt"]))
				if err == nil {
					report.BlacklistedIPs++
				}
			}
			if err != nil {
				f.Close()
				return report, fmt.Errorf("%s:%d: %w", path, line, err)
			}
			if _, err := a.DB.Exec(`INSERT INTO source_documents(kind,id,doc) VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET doc=excluded.doc`, kind, id, string(s.Bytes())); err != nil {
				f.Close()
				return report, fmt.Errorf("%s:%d: %w", path, line, err)
			}
		}
		if err := s.Err(); err != nil {
			f.Close()
			return report, err
		}
		f.Close()
	}
	return report, nil
}

func (a *App) removeMigratedDocument(kind, id string) error {
	var oldDoc string
	if err := a.DB.QueryRow(`SELECT doc FROM source_documents WHERE kind=? AND id=?`, kind, id).Scan(&oldDoc); err == nil && kind == "settings" {
		var previous map[string]json.RawMessage
		if json.Unmarshal([]byte(oldDoc), &previous) == nil {
			if _, err := a.DB.Exec(`DELETE FROM settings WHERE key=?`, str(previous["key"])); err != nil {
				return err
			}
		}
	}
	switch kind {
	case "users":
		_, err := a.DB.Exec(`DELETE FROM users WHERE id=?`, id)
		if err != nil {
			return err
		}
	case "apikeys":
		_, err := a.DB.Exec(`DELETE FROM apikeys WHERE id=?`, id)
		if err != nil {
			return err
		}
	case "images":
		_, err := a.DB.Exec(`DELETE FROM images WHERE id=?`, id)
		if err != nil {
			return err
		}
	case "ip_blacklist":
		_, err := a.DB.Exec(`DELETE FROM ip_blacklist WHERE id=?`, id)
		if err != nil {
			return err
		}
	}
	_, err := a.DB.Exec(`DELETE FROM source_documents WHERE kind=? AND id=?`, kind, id)
	return err
}

func copyIfMissing(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), ".migrate-*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	defer os.Remove(tmp)
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func str(b json.RawMessage) string   { var s string; json.Unmarshal(b, &s); return s }
func boolVal(b json.RawMessage) bool { var v bool; json.Unmarshal(b, &v); return v }
func intVal(b json.RawMessage) int64 { var v int64; json.Unmarshal(b, &v); return v }
