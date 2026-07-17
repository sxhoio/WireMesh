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

func (a *Authenticator) Login(email, password string) (string, User, error) {
	user, err := a.store.GetUserByEmail(strings.ToLower(email))
	if err != nil {
		return "", User{}, errors.New("invalid credentials")
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return "", User{}, errors.New("invalid credentials")
	}
	user.LastLoginAt = time.Now().UTC()
	if err := a.store.UpdateUserLastLogin(user.ID, user.LastLoginAt); err != nil {
		return "", User{}, errLoginPersistence
	}
	return a.issue(user), user, nil
}

func (a *Authenticator) issue(user User) string {
	payload, _ := json.Marshal(claims{Subject: user.ID, TenantID: user.TenantID, Role: user.Role, Exp: time.Now().Add(12 * time.Hour).Unix()})
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
