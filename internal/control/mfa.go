package control

import (
	"net/http"
	"time"
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

func (a *App) mfaDisable(w http.ResponseWriter, r *http.Request, c claims) {
	user, err := a.store.GetUser(c.Subject)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err := a.store.UpdateUserMFA(user.ID, EncryptedSecret{}, false); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable MFA")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "auth.mfa.disable", "user", user.ID, nil)
	writeJSON(w, http.StatusOK, MFAStatusResponse{Enabled: false})
}
