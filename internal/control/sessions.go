package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"time"
)

// UserSession 是一次登录会话的内存记录，用于会话列表与强制下线。
// 重启后清空（内存态），强制下线的令牌在重启后失效恢复——作为运维辅助而非
// 安全边界使用。
type UserSession struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"-"`
	UserID     string    `json:"user_id"`
	UserName   string    `json:"user_name"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Current    bool      `json:"current"`
}

func sessionTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func (a *App) recordSession(user User, token, userAgent string) {
	hash := sessionTokenHash(token)
	now := time.Now()
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	a.sessions[hash] = UserSession{
		ID: hash[:16], TenantID: user.TenantID, UserID: user.ID, UserName: user.Name,
		UserAgent: userAgent, CreatedAt: now, LastSeenAt: now,
	}
}

func (a *App) touchSession(token string) {
	if token == "" || strings.HasPrefix(token, apiTokenPrefix) {
		return
	}
	hash := sessionTokenHash(token)
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if session, ok := a.sessions[hash]; ok {
		session.LastSeenAt = time.Now()
		a.sessions[hash] = session
	}
}

func (a *App) isRevokedToken(token string) bool {
	if token == "" || strings.HasPrefix(token, apiTokenPrefix) {
		return false
	}
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	_, ok := a.revokedTokens[sessionTokenHash(token)]
	return ok
}

func (a *App) revokeSessionByID(tenant, id string) bool {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	for hash, session := range a.sessions {
		if hash[:16] != id || session.TenantID != tenant {
			continue
		}
		delete(a.sessions, hash)
		a.revokedTokens[hash] = time.Now()
		return true
	}
	return false
}

func (a *App) revokeCurrentSession(token string) {
	if token == "" || strings.HasPrefix(token, apiTokenPrefix) {
		return
	}
	hash := sessionTokenHash(token)
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	delete(a.sessions, hash)
	a.revokedTokens[hash] = time.Now()
}

// revokeUserSessions 吊销某用户的全部会话令牌（停用或删除用户时调用）。
func (a *App) revokeUserSessions(tenant, userID string) {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	for hash, session := range a.sessions {
		if session.TenantID != tenant || session.UserID != userID {
			continue
		}
		delete(a.sessions, hash)
		a.revokedTokens[hash] = time.Now()
	}
}

const (
	housekeepingInterval  = 10 * time.Minute
	revokedTokenRetention = 24 * time.Hour
	sessionRetention      = 24 * time.Hour
)

// StartHousekeeping 定期清理内存态凭据表：过期会话、已撤销令牌与失效 SSO state，
// 避免长期运行后无界增长。
func (a *App) StartHousekeeping(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(housekeepingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.cleanupSessionTables()
				a.cleanupSSOStates()
			}
		}
	}()
}

func (a *App) cleanupSessionTables() {
	now := time.Now()
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	for hash, revokedAt := range a.revokedTokens {
		if now.Sub(revokedAt) > revokedTokenRetention {
			delete(a.revokedTokens, hash)
		}
	}
	for hash, session := range a.sessions {
		if now.Sub(session.LastSeenAt) > sessionRetention {
			delete(a.sessions, hash)
		}
	}
}

func (a *App) cleanupSSOStates() {
	now := time.Now()
	a.ssoMu.Lock()
	defer a.ssoMu.Unlock()
	for state, info := range a.ssoStates {
		if now.After(info.ExpiresAt) {
			delete(a.ssoStates, state)
		}
	}
}

func (a *App) listSessions(tenant string, currentToken string) []UserSession {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	out := make([]UserSession, 0, len(a.sessions))
	currentHash := ""
	if currentToken != "" {
		currentHash = sessionTokenHash(currentToken)
	}
	for hash, session := range a.sessions {
		if session.TenantID != tenant {
			continue
		}
		session.Current = hash == currentHash
		out = append(out, session)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (a *App) userSessions(w http.ResponseWriter, r *http.Request, c claims) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, a.listSessions(c.TenantID, requestToken(r)))
		return
	}
	if err := a.revokeSessionByID(c.TenantID, r.PathValue("id")); !err {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
