package app

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

func decodePatch(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, bool) {
	var patch map[string]json.RawMessage
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&patch) != nil || patch == nil {
		fail(w, 400, "无效配置")
		return nil, false
	}
	return patch, true
}

func settingMap(a *App, key string, defaults map[string]any) map[string]json.RawMessage {
	var current map[string]json.RawMessage
	json.Unmarshal(a.setting(key, defaults), &current)
	if current == nil {
		current = make(map[string]json.RawMessage)
	}
	for k, value := range defaults {
		if current[k] == nil {
			current[k], _ = json.Marshal(value)
		}
	}
	return current
}

func appDefaults() map[string]any {
	return map[string]any{"appName": "TanoImg", "appLogo": "", "backgroundUrl": "", "backgroundBlur": 0, "siteUrl": "", "announcement": map[string]any{"enabled": false, "content": "", "displayType": "modal"}}
}

func (a *App) getSettings(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	settings := settingMap(a, "appSettings", appDefaults())
	var deleted int
	if err := a.DB.QueryRow(`SELECT count(*) FROM images WHERE is_deleted=1`).Scan(&deleted); err != nil {
		fail(w, 500, "读取设置失败")
		return
	}
	settings["deletedImagesCount"], _ = json.Marshal(deleted)
	ok(w, settings)
}

func validSiteURL(value string) bool {
	if value == "" {
		return true
	}
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func (a *App) putSettings(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	patch, valid := decodePatch(w, r)
	if !valid {
		return
	}
	settings := settingMap(a, "appSettings", appDefaults())
	for key, raw := range patch {
		switch key {
		case "appName", "appLogo", "backgroundUrl", "siteUrl":
			var value string
			if json.Unmarshal(raw, &value) != nil {
				fail(w, 400, "无效站点设置")
				return
			}
			value = strings.TrimSpace(value)
			if key == "appName" && (len(value) == 0 || len(value) > 100) {
				fail(w, 400, "站点名称无效")
				return
			}
			if key != "appName" && len(value) > 2048 {
				fail(w, 400, "地址过长")
				return
			}
			if (key == "appLogo" || key == "backgroundUrl") && !validBackgroundURL(value) {
				fail(w, 400, "图片地址无效")
				return
			}
			if key == "siteUrl" {
				value = strings.TrimRight(value, "/")
				if !validSiteURL(value) {
					fail(w, 400, "站点地址无效")
					return
				}
			}
			settings[key], _ = json.Marshal(value)
		case "backgroundBlur":
			var value int
			if json.Unmarshal(raw, &value) != nil || value < 0 || value > 40 {
				fail(w, 400, "背景模糊度无效")
				return
			}
			settings[key], _ = json.Marshal(value)
		case "announcement":
			var value struct {
				Enabled     bool   `json:"enabled"`
				Content     string `json:"content"`
				DisplayType string `json:"displayType"`
			}
			if json.Unmarshal(raw, &value) != nil || len(value.Content) > 4000 || (value.DisplayType != "" && value.DisplayType != "modal" && value.DisplayType != "banner") {
				fail(w, 400, "公告设置无效")
				return
			}
			if value.DisplayType == "" {
				value.DisplayType = "modal"
			}
			settings[key], _ = json.Marshal(value)
		default:
			fail(w, 400, "未知站点设置")
			return
		}
	}
	b, _ := json.Marshal(settings)
	if err := a.setSetting("appSettings", b); err != nil {
		fail(w, 500, "保存设置失败")
		return
	}
	ok(w, settings)
}

func privateDefaults() map[string]any {
	return map[string]any{"maxFileSize": 100 << 20, "enableCompression": false, "compressionQuality": 80, "convertToWebp": false, "convertToPng": false, "convertToJpg": false, "showOnHomepage": false}
}

func (a *App) getPrivateConfig(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	ok(w, settingMap(a, "privateApiConfig", privateDefaults()))
}

func (a *App) putPrivateConfig(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	patch, valid := decodePatch(w, r)
	if !valid {
		return
	}
	settings := settingMap(a, "privateApiConfig", privateDefaults())
	for key, raw := range patch {
		switch key {
		case "maxFileSize", "compressionQuality":
			var value int64
			if json.Unmarshal(raw, &value) != nil || (key == "maxFileSize" && (value < 1 || value > 200<<20)) || (key == "compressionQuality" && (value < 1 || value > 100)) {
				fail(w, 400, "私有上传配置无效")
				return
			}
		case "enableCompression", "convertToWebp", "convertToPng", "convertToJpg", "showOnHomepage":
			var value bool
			if json.Unmarshal(raw, &value) != nil {
				fail(w, 400, "私有上传配置无效")
				return
			}
		default:
			fail(w, 400, "未知私有上传配置")
			return
		}
		settings[key] = raw
	}
	conversions := 0
	for _, key := range []string{"convertToWebp", "convertToPng", "convertToJpg"} {
		var value bool
		json.Unmarshal(settings[key], &value)
		if value {
			conversions++
		}
	}
	if conversions > 1 {
		fail(w, 400, "只能选择一种转换格式")
		return
	}
	b, _ := json.Marshal(settings)
	if err := a.setSetting("privateApiConfig", b); err != nil {
		fail(w, 500, "保存配置失败")
		return
	}
	ok(w, settings)
}
