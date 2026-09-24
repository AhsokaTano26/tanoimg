package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type contentSafetyConfig struct {
	Enabled         bool   `json:"enabled"`
	Provider        string `json:"provider"`
	AutoBlacklistIP bool   `json:"autoBlacklistIp"`
	Providers       map[string]struct {
		APIURL    string  `json:"apiUrl"`
		UploadURL string  `json:"uploadUrl"`
		APIKey    string  `json:"apiKey"`
		Threshold float64 `json:"threshold"`
	} `json:"providers"`
}

func (a *App) enqueueModeration(im Image) error {
	id, err := newID()
	if err != nil {
		return err
	}
	_, err = a.DB.Exec(`INSERT INTO moderation_tasks(id,image_id,filename,status,created_at,updated_at) VALUES(?,?,?,'pending',?,?)`, id, im.ID, im.Filename, now(), now())
	if err == nil {
		select {
		case a.moderationWake <- struct{}{}:
		default:
		}
	}
	return err
}

func (a *App) StartModeration() {
	a.moderationOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		a.moderationCancel = cancel
		a.moderationDone = make(chan struct{})
		a.DB.Exec(`UPDATE moderation_tasks SET status='failed',next_attempt=0 WHERE status='processing'`)
		go func() {
			defer close(a.moderationDone)
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				for {
					err := a.processOneModeration(ctx)
					if errors.Is(err, sql.ErrNoRows) || ctx.Err() != nil {
						break
					}
					if err != nil {
						break
					}
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				case <-a.moderationWake:
				}
			}
		}()
	})
}

type moderationOutcome struct {
	NSFW  bool
	Score float64
}

func (a *App) processOneModeration(ctx context.Context) error {
	var id, imageID, filename string
	var retries int
	err := a.DB.QueryRow(`SELECT id,image_id,filename,retry_count FROM moderation_tasks WHERE status IN ('pending','failed') AND next_attempt<=? ORDER BY created_at LIMIT 1`, time.Now().Unix()).Scan(&id, &imageID, &filename, &retries)
	if err != nil {
		return err
	}
	if _, err := a.DB.Exec(`UPDATE moderation_tasks SET status='processing',updated_at=? WHERE id=?`, now(), id); err != nil {
		return err
	}
	config := publicConfig(a).ContentSafety
	if !config.Enabled {
		_, err := a.DB.Exec(`UPDATE moderation_tasks SET status='completed',error='',updated_at=? WHERE id=?`, now(), id)
		if err == nil {
			_, err = a.DB.Exec(`UPDATE images SET moderation_status='skipped',moderation_checked=1 WHERE id=?`, imageID)
		}
		return err
	}
	if !safeFilename.MatchString(filename) {
		err = errors.New("审核任务中的图片文件名无效")
	} else {
		var outcome moderationOutcome
		outcome, err = a.moderateFile(ctx, filepath.Join(a.DataDir, "uploads", filename), filename, config)
		if err == nil {
			tx, txErr := a.DB.Begin()
			if txErr != nil {
				return txErr
			}
			defer tx.Rollback()
			if _, txErr = tx.Exec(`UPDATE images SET moderation_checked=1,moderation_status='completed',moderation_score=?,is_nsfw=?,updated_at=? WHERE id=?`, outcome.Score, outcome.NSFW, now(), imageID); txErr != nil {
				return txErr
			}
			if _, txErr = tx.Exec(`UPDATE moderation_tasks SET status='completed',error='',updated_at=? WHERE id=?`, now(), id); txErr != nil {
				return txErr
			}
			if txErr = tx.Commit(); txErr != nil {
				return txErr
			}
			if outcome.NSFW && config.AutoBlacklistIP {
				var ip string
				if a.DB.QueryRow(`SELECT ip FROM images WHERE id=?`, imageID).Scan(&ip) == nil && ip != "" && ip != "unknown" {
					blacklistID, idErr := newID()
					if idErr == nil {
						a.DB.Exec(`INSERT INTO ip_blacklist(id,ip,reason,created_at) VALUES(?,?,?,?) ON CONFLICT(ip) DO NOTHING`, blacklistID, ip, "自动拉黑：上传违规图片", now())
					}
				}
			}
			a.enqueueNotification("nsfw", "内容审核完成", fmt.Sprintf("图片 %s 审核完成，违规：%t", filename, outcome.NSFW), map[string]any{"imageId": imageID, "filename": filename, "score": outcome.Score, "isNsfw": outcome.NSFW, "provider": config.Provider})
			return nil
		}
	}
	retries++
	status := "failed"
	if retries >= 3 {
		status = "error"
	}
	_, updateErr := a.DB.Exec(`UPDATE moderation_tasks SET status=?,retry_count=?,next_attempt=?,error=?,updated_at=? WHERE id=?`, status, retries, time.Now().Add(60*time.Second).Unix(), err.Error(), now(), id)
	if updateErr != nil {
		return updateErr
	}
	_, updateErr = a.DB.Exec(`UPDATE images SET moderation_status=?,updated_at=? WHERE id=?`, status, now(), imageID)
	return updateErr
}

func multipartImageRequest(ctx context.Context, endpoint, field, filename, imagePath string) (*http.Request, *os.File, error) {
	f, err := os.Open(imagePath)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	var frame bytes.Buffer
	writer := multipart.NewWriter(&frame)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filename))
	header.Set("Content-Type", mimeFor(strings.TrimPrefix(filepath.Ext(filename), ".")))
	if _, err := writer.CreatePart(header); err != nil {
		f.Close()
		return nil, nil, err
	}
	start := frame.Len()
	if err := writer.Close(); err != nil {
		f.Close()
		return nil, nil, err
	}
	data := frame.Bytes()
	body := io.MultiReader(bytes.NewReader(data[:start]), f, bytes.NewReader(data[start:]))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.ContentLength = int64(len(data)) + info.Size()
	return request, f, nil
}

func (a *App) requestModerationJSON(req *http.Request) (map[string]any, error) {
	response, err := a.urlClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("审核服务返回 HTTP %d", response.StatusCode)
	}
	var result map[string]any
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func number(value any) float64        { n, _ := value.(float64); return n }
func object(value any) map[string]any { m, _ := value.(map[string]any); return m }

func (a *App) moderateFile(ctx context.Context, imagePath, filename string, cfg contentSafetyConfig) (moderationOutcome, error) {
	provider := cfg.Provider
	if provider == "" {
		provider = "elysiatools"
	}
	settings := cfg.Providers[provider]
	endpoint, field := settings.APIURL, "image"
	threshold := settings.Threshold
	switch provider {
	case "nsfwdet":
		if endpoint == "" {
			endpoint = "https://nsfwdet.com/api/v1/detect-nsfw"
		}
		if threshold == 0 {
			threshold = 0.5
		}
	case "nsfw_detector":
		field = "file"
		if endpoint == "" {
			return moderationOutcome{}, errors.New("请配置 nsfw_detector API URL")
		}
		if threshold == 0 {
			threshold = 0.8
		}
	case "elysiatools":
		field = "file"
		endpoint = settings.UploadURL
		if endpoint == "" {
			endpoint = "https://elysiatools.com/upload/nsfw-image-detector"
		}
	default:
		return moderationOutcome{}, fmt.Errorf("不支持的审核服务: %s", provider)
	}
	if _, err := parseRemoteURL(endpoint); err != nil {
		return moderationOutcome{}, err
	}
	req, file, err := multipartImageRequest(ctx, endpoint, field, filename, imagePath)
	if err != nil {
		return moderationOutcome{}, err
	}
	defer file.Close()
	if provider == "nsfwdet" && settings.APIKey != "" {
		req.Header.Set("X-API-Key", settings.APIKey)
	}
	if provider == "nsfw_detector" && settings.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+settings.APIKey)
	}
	result, err := a.requestModerationJSON(req)
	if err != nil {
		return moderationOutcome{}, err
	}
	if provider == "nsfwdet" {
		if number(result["code"]) != 0 {
			return moderationOutcome{}, errors.New("审核服务返回错误")
		}
		score := number(object(result["result"])["nsfw"])
		return moderationOutcome{NSFW: score >= threshold, Score: score}, nil
	}
	if provider == "nsfw_detector" {
		if result["status"] != "success" {
			return moderationOutcome{}, errors.New("审核服务返回错误")
		}
		score := number(object(result["result"])["nsfw"])
		return moderationOutcome{NSFW: score >= threshold, Score: score}, nil
	}
	filePath, _ := result["filePath"].(string)
	if filePath == "" {
		return moderationOutcome{}, errors.New("审核上传响应缺少 filePath")
	}
	detectURL := settings.APIURL
	if detectURL == "" {
		detectURL = "https://elysiatools.com/zh/api/tools/nsfw-image-detector"
	}
	if _, err := parseRemoteURL(detectURL); err != nil {
		return moderationOutcome{}, err
	}
	requestBody, _ := json.Marshal(map[string]any{"imageFile": filePath, "sensitivity": 0.5, "analysisMode": "auto"})
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, detectURL, bytes.NewReader(requestBody))
	if err != nil {
		return moderationOutcome{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	result, err = a.requestModerationJSON(req)
	if err != nil {
		return moderationOutcome{}, err
	}
	data := object(result["data"])
	if len(data) == 0 {
		return moderationOutcome{}, errors.New("审核响应缺少 data")
	}
	safe, _ := object(data["data"])["isSafe"].(bool)
	score := 0.0
	if !safe {
		score = (100 - number(data["confidence"])) / 100
	}
	return moderationOutcome{NSFW: !safe, Score: score}, nil
}
