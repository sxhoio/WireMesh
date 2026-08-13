package control

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var errLoginPersistence = errors.New("failed to record login time")

// defaultSessionTTL 是未配置会话超时（或设置读取失败）时的回退有效期。
const defaultSessionTTL = 12 * time.Hour

// authCookieName is the HttpOnly cookie used to carry the session token in
// browser requests. The Authorization header remains supported as a fallback
// for the Agent protocol, tests, and non-browser clients.
const authCookieName = "wiremesh_token"

type claims struct {
	Subject, TenantID string
	Role              Role
	Exp               int64
}

type Authenticator struct {
	secret []byte
	store  Store
}

func newAuthenticator(store Store, secret string) *Authenticator {
	sum := sha256.Sum256([]byte(secret))
	return &Authenticator{secret: sum[:], store: store}
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

// sessionTTL 读取租户配置的会话超时（分钟），未配置或异常时回退 12 小时。
func (a *Authenticator) sessionTTL(tenant string) time.Duration {
	if settings, err := a.store.GetSettings(tenant); err == nil && settings.SessionTimeoutMin >= 5 && settings.SessionTimeoutMin <= 1440 {
		return time.Duration(settings.SessionTimeoutMin) * time.Minute
	}
	return defaultSessionTTL
}

func (a *Authenticator) Login(email, password string) (string, User, error) {
	user, err := a.store.GetUserByEmail(strings.ToLower(email))
	if err != nil {
		return "", User{}, errors.New("invalid credentials")
	}
	if !user.Active {
		return "", User{}, errors.New("invalid credentials")
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return "", User{}, errors.New("invalid credentials")
	}
	user.LastLoginAt = time.Now().UTC()
	if err := a.store.UpdateUserLastLogin(user.ID, user.LastLoginAt); err != nil {
		return "", User{}, errLoginPersistence
	}
	return a.issueTTL(user, a.sessionTTL(user.TenantID)), user, nil
}

func (a *Authenticator) issue(user User) string {
	return a.issueTTL(user, defaultSessionTTL)
}

func (a *Authenticator) issueTTL(user User, ttl time.Duration) string {
	payload, _ := json.Marshal(claims{Subject: user.ID, TenantID: user.TenantID, Role: user.Role, Exp: time.Now().Add(ttl).Unix()})
	body := base64.RawURLEncoding.EncodeToString(payload)
	sig := a.sign(body)
	return body + "." + base64.RawURLEncoding.EncodeToString(sig)
}
func (a *Authenticator) sign(body string) []byte {
	h := hmac.New(sha256.New, a.secret)
	h.Write([]byte(body))
	return h.Sum(nil)
}
func (a *Authenticator) Parse(token string) (claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return claims{}, errors.New("invalid token")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, a.sign(parts[0])) {
		return claims{}, errors.New("invalid token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims{}, err
	}
	var c claims
	if err = json.Unmarshal(raw, &c); err != nil {
		return claims{}, err
	}
	if c.Exp < time.Now().Unix() {
		return claims{}, errors.New("token expired")
	}
	return c, nil
}

func allowed(role Role, required Role) bool {
	order := map[Role]int{RoleViewer: 1, RoleOperator: 2, RoleAdmin: 3}
	return order[role] >= order[required]
}
