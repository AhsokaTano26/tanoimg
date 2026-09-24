package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const releaseURL = "https://api.github.com/repos/AhsokaTano26/tanoimg/releases/latest"

func (a *App) checkVersion(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	result := map[string]any{"currentVersion": a.Version, "latestVersion": nil, "hasUpdate": false, "error": nil}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		result["error"] = "无法创建版本查询"
		ok(w, result)
		return
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "TanoImg-Version-Check")
	response, err := a.urlClient.Do(request)
	if err != nil {
		result["error"] = "无法连接发布服务"
		ok(w, result)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		result["error"] = fmt.Sprintf("无法获取发布版本 (HTTP %d)", response.StatusCode)
		ok(w, result)
		return
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&release) != nil || release.TagName == "" {
		result["error"] = "发布版本信息无效"
		ok(w, result)
		return
	}
	result["latestVersion"] = release.TagName
	if _, valid := parseVersion(release.TagName); !valid {
		result["error"] = "发布版本格式无效"
	} else if _, valid := parseVersion(a.Version); !valid {
		result["error"] = "开发构建无法比较版本"
	} else {
		result["hasUpdate"] = newerVersion(release.TagName, a.Version)
	}
	ok(w, result)
}

func parseVersion(version string) ([3]int, bool) {
	var parts [3]int
	version = strings.TrimPrefix(version, "v")
	version = strings.SplitN(version, "-", 2)[0]
	pieces := strings.Split(version, ".")
	if len(pieces) != 3 {
		return parts, false
	}
	for i, piece := range pieces {
		value, err := strconv.Atoi(piece)
		if err != nil || value < 0 {
			return parts, false
		}
		parts[i] = value
	}
	return parts, true
}

func newerVersion(latest, current string) bool {
	newParts, validNew := parseVersion(latest)
	oldParts, validOld := parseVersion(current)
	if !validNew || !validOld {
		return false
	}
	for i := range newParts {
		if newParts[i] > oldParts[i] {
			return true
		}
		if newParts[i] < oldParts[i] {
			return false
		}
	}
	return false
}
