package control

import (
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// backupDatabase 使用 VACUUM INTO 生成一致性在线备份并下载（仅 SQLite）。
func (a *App) backupDatabase(w http.ResponseWriter, r *http.Request, c claims) {
	if a.database == nil {
		writeError(w, http.StatusConflict, "database is managed by server environment settings")
		return
	}
	// 随机临时文件（CreateTemp 默认 0600），避免可预测路径与同机可读
	target, err := os.CreateTemp(os.TempDir(), "wiremesh-backup-*.db")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create backup file")
		return
	}
	targetPath := target.Name()
	target.Close()
	defer os.Remove(targetPath)
	if err := a.database.BackupSQLite(targetPath); err != nil {
		writeError(w, http.StatusBadRequest, "database backup failed")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "database.backup", "tenant", c.TenantID, nil)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="wiremesh-backup.db"`)
	w.Header().Set("Cache-Control", "no-store, private")
	http.ServeFile(w, r, targetPath)
}

// restoreDatabase 接收上传的 SQLite 备份文件（multipart: file + password
// + otp）并热切换到恢复后的数据库。二次认证（当前密码 + MFA 验证码）
// 确认操作者身份（C-1 修复），恢复完成后清空全部内存会话强制重新登录
// （库被整体替换，旧令牌不可信）。
func (a *App) restoreDatabase(w http.ResponseWriter, r *http.Request, c claims) {
	if a.database == nil {
		writeError(w, http.StatusConflict, "database is managed by server environment settings")
		return
	}
	if err := r.ParseMultipartForm(512 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "恢复请求必须是 multipart 表单（file + password）")
		return
	}
	defer r.MultipartForm.RemoveAll()
	password := strings.TrimSpace(r.FormValue("password"))
	otp := strings.TrimSpace(r.FormValue("otp"))
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "缺少备份文件字段 file")
		return
	}
	defer file.Close()
	if header == nil || header.Size > 512<<20 {
		writeError(w, http.StatusBadRequest, "备份文件过大或无效")
		return
	}
	// 二次认证：当前密码（+ 已启用 MFA 时的验证码）复核
	user, err := a.store.GetUser(c.Subject)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account no longer exists")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		writeError(w, http.StatusUnauthorized, "当前密码不正确")
		return
	}
	if user.TotpEnabled {
		secretBytes, decryptErr := a.box.Decrypt(user.TotpSecret)
		if decryptErr != nil || !verifyTOTP(string(secretBytes), otp, time.Now()) {
			if otp == "" {
				writeError(w, http.StatusBadRequest, "otp_required")
			} else {
				writeError(w, http.StatusBadRequest, "otp_invalid")
			}
			return
		}
	}
	target, err := os.CreateTemp(os.TempDir(), "wiremesh-restore-*.db")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to receive backup")
		return
	}
	targetPath := target.Name()
	defer os.Remove(targetPath)
	if _, err := io.Copy(target, io.LimitReader(file, 512<<20)); err != nil {
		target.Close()
		writeError(w, http.StatusBadRequest, "failed to receive backup")
		return
	}
	if err := target.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store backup")
		return
	}
	if err := a.database.RestoreSQLite(r.Context(), targetPath); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 恢复整体替换了数据库：内存态会话/吊销表与磁盘已不一致，
	// 清空全部内存凭据，强制所有用户（含操作者）重新登录。
	a.ClearAllSessionsAfterRestore()
	a.auditEvent(c.TenantID, c.Subject, "database.restore", "tenant", c.TenantID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}
