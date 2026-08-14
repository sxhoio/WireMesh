package control

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const apiTokenPrefix = "wm_api_"

func (a *App) apiTokens(w http.ResponseWriter, r *http.Request, c claims) {
	if r.Method == http.MethodGet {
		items, err := a.store.ListAPITokens(c.TenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取 API 令牌失败")
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	var in struct {
		Name    string `json:"name"`
		TTLDays int    `json:"ttl_days"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 80 {
		writeError(w, http.StatusBadRequest, "name is required and must not exceed 80 characters")
		return
	}
	if in.TTLDays < 0 || in.TTLDays > 3650 {
		writeError(w, http.StatusBadRequest, "ttl_days must be between 0 and 3650")
		return
	}
	plaintext := apiTokenPrefix + base64.RawURLEncoding.EncodeToString(randomBytes(32))
	hash := sha256.Sum256([]byte(plaintext))
	now := time.Now().UTC()
	token := APIToken{
		ID: newID("apitok"), TenantID: c.TenantID, Name: in.Name, CreatedBy: c.Subject,
		TokenHash: hex.EncodeToString(hash[:]), CreatedAt: now,
	}
	if in.TTLDays > 0 {
		expires := now.AddDate(0, 0, in.TTLDays)
		token.ExpiresAt = &expires
	}
	if err := a.store.CreateAPIToken(token); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create API token")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "api_token.create", "api_token", token.ID, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"token": plaintext, "api_token": token})
}

func (a *App) deleteAPIToken(w http.ResponseWriter, r *http.Request, c claims) {
	if err := a.store.DeleteAPIToken(c.TenantID, r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, "API token not found")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "api_token.delete", "api_token", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

// lookupAPIToken 验证 `Authorization: Bearer wm_api_...` 形式的 API 令牌，
// 返回对应记录；令牌只存 SHA-256 哈希，过期即失效。
func (a *App) lookupAPIToken(token string) (APIToken, bool) {
	if !strings.HasPrefix(token, apiTokenPrefix) {
		return APIToken{}, false
	}
	hash := sha256.Sum256([]byte(token))
	apiToken, err := a.store.GetAPITokenByHash(hex.EncodeToString(hash[:]))
	if err != nil {
		return APIToken{}, false
	}
	if apiToken.ExpiresAt != nil && time.Now().After(*apiToken.ExpiresAt) {
		return APIToken{}, false
	}
	return apiToken, true
}
