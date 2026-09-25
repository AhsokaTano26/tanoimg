package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
)

func (a *App) uploadIdentity(w http.ResponseWriter, r *http.Request, scope, visibility string) (apiKeyPrincipal, bool) {
	if a.userID(r) != "" {
		return apiKeyPrincipal{}, true
	}
	p, ok := a.resolveAPIKey(r, scope)
	if !ok {
		fail(w, 401, "缺少有效的 API Key 或登录状态，或密钥缺少上传权限")
		return apiKeyPrincipal{}, false
	}
	if visibility == "public" && !p.Allows("upload:public") {
		fail(w, 403, "API Key 未获公开上传权限")
		return apiKeyPrincipal{}, false
	}
	return p, true
}

func (a *App) acquireFairUploadSlot(w http.ResponseWriter, r *http.Request, public bool) (func(), bool) {
	principal := a.uploadPrincipal(r, public)
	if principal == "" {
		principal = "ip:" + a.clientIP(r)
	}
	release, err := a.uploadScheduler.Acquire(r.Context(), principal)
	if err == nil {
		return release, true
	}
	if errors.Is(err, context.Canceled) {
		return nil, false
	}
	w.Header().Set("Retry-After", "1")
	fail(w, http.StatusTooManyRequests, "上传队列已满或等待超时，请稍后重试")
	return nil, false
}

func (a *App) reserveQuotaOrFail(w http.ResponseWriter, r *http.Request, p apiKeyPrincipal, imageID string, size int64) (string, bool) {
	id, err := a.reserveUploadQuota(r.Context(), p, a.clientIP(r), imageID, size)
	if err == nil {
		return id, true
	}
	if errors.Is(err, errDailyQuotaExceeded) {
		seconds := int64(time.Until(time.Now().UTC().Truncate(24*time.Hour).Add(24*time.Hour)).Seconds()) + 1
		w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		fail(w, http.StatusTooManyRequests, "已达到今日上传数量或流量配额")
		return "", false
	}
	fail(w, http.StatusServiceUnavailable, "暂时无法核算上传配额")
	return "", false
}

func (a *App) claimOrReplayUpload(w http.ResponseWriter, r *http.Request, public bool) (string, bool) {
	key, image, err := a.claimUploadKey(r, public)
	if err != nil {
		if errors.Is(err, errUploadKeyBusy) {
			w.Header().Set("Retry-After", "1")
			fail(w, http.StatusConflict, "同一幂等键的上传仍在进行")
		} else {
			fail(w, http.StatusBadRequest, err.Error())
		}
		return "", false
	}
	if image != nil {
		ok(w, image)
		return "", false
	}
	return key, true
}
