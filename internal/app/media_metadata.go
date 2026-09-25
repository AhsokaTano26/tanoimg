package app

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func normalizeTags(tags []string) ([]string, error) {
	if len(tags) > 20 {
		return nil, errors.New("标签最多 20 个")
	}
	result := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if len([]rune(tag)) > 40 || strings.ContainsAny(tag, "\x00\n\r") {
			return nil, errors.New("标签无效或过长")
		}
		key := strings.ToLower(tag)
		if !seen[key] {
			result = append(result, tag)
			seen[key] = true
		}
	}
	return result, nil
}

func applyUploadMetadata(im *Image, alt, author, license string, tags []string) error {
	if len([]rune(alt)) > 500 || len([]rune(author)) > 100 || len([]rune(license)) > 100 {
		return errors.New("图片描述过长")
	}
	normalized, err := normalizeTags(tags)
	if err != nil {
		return err
	}
	im.Alt = strings.TrimSpace(alt)
	im.Author = strings.TrimSpace(author)
	im.License = strings.TrimSpace(license)
	im.Tags = normalized
	return nil
}

func fileMD5(f *os.File) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func filesEqual(a, b *os.File) (bool, error) {
	if _, err := a.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	defer a.Seek(0, io.SeekStart)
	var left, right [64 << 10]byte
	for {
		n, errA := io.ReadFull(a, left[:])
		m, errB := io.ReadFull(b, right[:])
		if n != m || !bytes.Equal(left[:n], right[:m]) {
			return false, nil
		}
		if errA == io.EOF || errA == io.ErrUnexpectedEOF || errB == io.EOF || errB == io.ErrUnexpectedEOF {
			return errA == errB, nil
		}
		if errA != nil {
			return false, errA
		}
		if errB != nil {
			return false, errB
		}
		if n == 0 {
			return false, io.ErrNoProgress
		}
	}
}

func (a *App) duplicateOwner(r *http.Request) (bool, string) {
	if a.userID(r) != "" {
		return true, ""
	}
	key := r.Header.Get("X-API-Key")
	if key == "" {
		key = r.URL.Query().Get("apiKey")
	}
	if key == "" {
		return false, ""
	}
	var id string
	if a.DB.QueryRow(`SELECT id FROM apikeys WHERE key=? AND enabled=1`, key).Scan(&id) != nil {
		return false, ""
	}
	return false, id
}

func (a *App) findExactDuplicate(source *os.File, digest string, size int64, admin bool, apiKeyID string) (string, error) {
	if !admin && apiKeyID == "" {
		return "", nil
	}
	query := `SELECT id,filename FROM images WHERE md5=? AND size=? AND is_deleted=0`
	args := []any{digest, size}
	if !admin {
		query += ` AND api_key_id=?`
		args = append(args, apiKeyID)
	}
	query += ` ORDER BY uploaded_at DESC LIMIT 100`
	rows, err := a.DB.Query(query, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id, filename string
		if err := rows.Scan(&id, &filename); err != nil {
			return "", err
		}
		if !safeFilename.MatchString(filename) {
			continue
		}
		candidate, err := os.Open(filepath.Join(a.DataDir, "uploads", filename))
		if err != nil {
			continue
		}
		equal, compareErr := filesEqual(source, candidate)
		candidate.Close()
		if compareErr != nil {
			return "", fmt.Errorf("compare duplicate: %w", compareErr)
		}
		if equal {
			return id, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", nil
}
