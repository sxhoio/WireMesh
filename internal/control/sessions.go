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

// RevokedToken 是已吊销会话令牌的持久化记录（DB 存储），
// 保证服务重启后吊销仍然生效，不再依赖仅存在于内存的黑名单。
type RevokedToken struct {
	TokenHash string
	TenantID  string
	RevokedAt time.Time
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

// markTokenRevoked 把令牌哈希加入内存黑名单并持久化到数据库，
// 保证服务重启后吊销仍然生效。持久化失败不阻塞（内存态仍生效）。
func (a *App) markTokenRevoked(tenant, tokenHash string, at time.Time) {
	a.sessionMu.Lock()
	a.revokedTokens[tokenHash] = at
	a.sessionMu.Unlock()
	_ = a.store.AddRevokedToken(RevokedToken{TokenHash: tokenHash, TenantID: tenant, RevokedAt: at})
}

// loadRevokedTokens 启动时从数据库加载已吊销令牌到内存黑名单。
func (a *App) loadRevokedTokens() {
	rows, err := a.store.ListRevokedTokens()
	if err != nil {
		return
	}
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	for _, row := range rows {
		if row.TenantID != "" {
			a.revokedTokens[row.TokenHash] = row.RevokedAt
		}
	}
}

func (a *App) revokeSessionByID(tenant, id string) bool {
	a.sessionMu.Lock()
	var target string
	for hash, session := range a.sessions {
		if hash[:16] == id && session.TenantID == tenant {
			target = hash
			break
		}
	}
	if target == "" {
		a.sessionMu.Unlock()
		return false
	}
	delete(a.sessions, target)
	at := time.Now()
	a.revokedTokens[target] = at
	a.sessionMu.Unlock()
	_ = a.store.AddRevokedToken(RevokedToken{TokenHash: target, TenantID: tenant, RevokedAt: at})
	return true
}

// revokeCurrentSession 吊销当前会话令牌（logout）。M-6：吊销记录必须携带
// 租户 ID，否则重启后 loadRevokedTokens 会跳过空租户行导致令牌"复活"。
// tenant 为空时（令牌解析失败）仅清内存态，不写库。
func (a *App) revokeCurrentSession(token, tenant string) {
	if token == "" || strings.HasPrefix(token, apiTokenPrefix) {
		return
	}
	hash := sessionTokenHash(token)
	at := time.Now()
	a.sessionMu.Lock()
	delete(a.sessions, hash)
	a.revokedTokens[hash] = at
	a.sessionMu.Unlock()
	if tenant != "" {
		_ = a.store.AddRevokedToken(RevokedToken{TokenHash: hash, TenantID: tenant, RevokedAt: at})
	}
}

// revokeUserSessions 吊销某用户的全部会话令牌（停用或删除用户时调用）。
func (a *App) revokeUserSessions(tenant, userID string) {
	a.revokeUserSessionsExcept(tenant, userID, "")
}

// revokeUserSessionsExcept 吊销某用户的会话令牌，但保留 exceptToken（如
// 改密请求的当前会话，M-8）。exceptToken 为空时吊销全部。
func (a *App) revokeUserSessionsExcept(tenant, userID, exceptToken string) {
	exceptHash := ""
	if exceptToken != "" && !strings.HasPrefix(exceptToken, apiTokenPrefix) {
		exceptHash = sessionTokenHash(exceptToken)
	}
	a.sessionMu.Lock()
	targets := make([]string, 0)
	for hash, session := range a.sessions {
		if session.TenantID == tenant && session.UserID == userID && hash != exceptHash {
			targets = append(targets, hash)
			delete(a.sessions, hash)
		}
	}
	at := time.Now()
	for _, hash := range targets {
		a.revokedTokens[hash] = at
	}
	a.sessionMu.Unlock()
	for _, hash := range targets {
		_ = a.store.AddRevokedToken(RevokedToken{TokenHash: hash, TenantID: tenant, RevokedAt: at})
	}
}

const (
	housekeepingInterval  = 10 * time.Minute
	revokedTokenRetention = 24 * time.Hour
	sessionRetention      = 24 * time.Hour
)

// 数据保留策略（专项：性能/可扩展性）。
// retention.rawDays 配置流量采样保留天数；0 或未配置时使用此默认值，
// 防止心跳高频写入导致 traffic_samples 无界增长。
const defaultTrafficRetentionDays = 30

// 每次发布会为每节点创建一条 delivery、每个网络新增一个修订版本；
// 这些操作历史按节点/网络保留最近 N 条，防止长期运行无限膨胀。
const (
	maxDeliveriesPerNode       = 200
	maxRevisionsPerNetwork     = 50
	maxTrafficSamplesPerTenant = 500000
)

// StartHousekeeping 定期清理内存态凭据表与持久化操作历史：过期会话、
// 已撤销令牌、失效 SSO state，以及超出保留策略的流量采样/下发记录/修订版本。
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
				a.pruneOperationalData()
			}
		}
	}()
}

// pruneOperationalData 按租户设置的数据保留策略清理持久化操作历史，
// 防止长期运行下数据库无界增长（性能/可扩展性专项）。
func (a *App) pruneOperationalData() {
	// 全部租户：流量采样按时间保留（settings.retention.rawDays）。
	tenants, err := a.store.ListTenants()
	if err != nil {
		return
	}
	for _, tenant := range tenants {
		days := defaultTrafficRetentionDays
		if settings, settingsErr := a.tenantSettings(tenant); settingsErr == nil && settings.Retention.RawDays > 0 {
			days = settings.Retention.RawDays
		}
		cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		_, _ = a.store.DeleteTrafficSamplesBefore(tenant, cutoff)
		// 下发记录与修订版本按数量保留
		_, _ = a.store.DeleteDeliveriesBefore(tenant, "", maxDeliveriesPerNode)
		networks, networkErr := a.store.ListNetworks(tenant, "")
		if networkErr != nil {
			continue
		}
		for _, network := range networks {
			// 保留最近 maxRevisionsPerNetwork 个版本：先取当前最新版本号，
			// 删除更早的版本（LatestRevision 不受影响）。
			if latest, latestErr := a.store.LatestRevision(tenant, network.ID); latestErr == nil && latest.Version > maxRevisionsPerNetwork {
				_, _ = a.store.DeleteRevisionsBefore(tenant, network.ID, latest.Version-maxRevisionsPerNetwork+1)
			}
		}
	}
}

func (a *App) cleanupSessionTables() {
	now := time.Now()
	a.sessionMu.Lock()
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
	a.sessionMu.Unlock()
	// 同步清理数据库中超过保留期的吊销记录
	_ = a.store.DeleteRevokedTokensBefore(now.Add(-revokedTokenRetention))
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
