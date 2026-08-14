package control

import (
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type MFAStatusResponse struct {
	Enabled bool `json:"enabled"`
}

func (a *App) mfaStatus(w http.ResponseWriter, r *http.Request, c claims) {
	user, err := a.store.GetUser(c.Subject)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, MFAStatusResponse{Enabled: user.TotpEnabled})
}

func (a *App) mfaSetup(w http.ResponseWriter, r *http.Request, c claims) {
	user, err := a.store.GetUser(c.Subject)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	secret := generateTOTPSecret()
	encrypted, err := a.box.Encrypt([]byte(secret))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to protect secret")
		return
	}
	// 先保存但保持未启用；enable 校验通过后才强制要求 OTP。
	if err := a.store.UpdateUserMFA(user.ID, encrypted, false); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save secret")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "auth.mfa.setup", "user", user.ID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "uri": otpauthURI(secret, user.Email)})
}

func (a *App) mfaEnable(w http.ResponseWriter, r *http.Request, c claims) {
	user, err := a.store.GetUser(c.Subject)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	secretBytes, err := a.box.Decrypt(user.TotpSecret)
	if err != nil {
		writeError(w, http.StatusBadRequest, "mfa setup required first")
		return
	}
	var in struct {
		OTP string `json:"otp"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !verifyTOTP(string(secretBytes), in.OTP, time.Now()) {
		writeError(w, http.StatusBadRequest, "invalid one-time password")
		return
	}
	if err := a.store.UpdateUserMFA(user.ID, user.TotpSecret, true); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enable MFA")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "auth.mfa.enable", "user", user.ID, nil)
	writeJSON(w, http.StatusOK, MFAStatusResponse{Enabled: true})
}

// mfaDisable 关闭 MFA。为防会话劫持者静默移除第二因子（S10 复核），
// 必须验证当前密码 + 当前动态验证码（已启用 MFA 时）双重复核。
func (a *App) mfaDisable(w http.ResponseWriter, r *http.Request, c claims) {
	var in struct {
		Password string `json:"password"`
		OTP      string `json:"otp"`
	}
	if !decode(w, r, &in) {
		return
	}
	user, err := a.store.GetUser(c.Subject)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	// 复核 1：当前密码
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "当前密码不正确")
		return
	}
	// 复核 2：已启用 MFA 时必须提供有效动态验证码（防劫持会话直接移除 MFA）
	if user.TotpEnabled {
		secretBytes, decryptErr := a.box.Decrypt(user.TotpSecret)
		if decryptErr != nil || !verifyTOTP(string(secretBytes), in.OTP, time.Now()) {
			if strings.TrimSpace(in.OTP) == "" {
				writeError(w, http.StatusBadRequest, "otp_required")
			} else {
				writeError(w, http.StatusBadRequest, "otp_invalid")
			}
			return
		}
	}
	if err := a.store.UpdateUserMFA(user.ID, EncryptedSecret{}, false); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable MFA")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "auth.mfa.disable", "user", user.ID, nil)
	writeJSON(w, http.StatusOK, MFAStatusResponse{Enabled: false})
}
