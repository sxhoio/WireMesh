package control

import (
	"io"
	"net/http"
	"os"
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

// restoreDatabase 接收上传的 SQLite 备份文件并热切换到恢复后的数据库。
func (a *App) restoreDatabase(w http.ResponseWriter, r *http.Request, c claims) {
	if a.database == nil {
		writeError(w, http.StatusConflict, "database is managed by server environment settings")
		return
	}
	defer r.Body.Close()
	target, err := os.CreateTemp(os.TempDir(), "wiremesh-restore-*.db")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to receive backup")
		return
	}
	targetPath := target.Name()
	defer os.Remove(targetPath)
	if _, err := io.Copy(target, io.LimitReader(r.Body, 512<<20)); err != nil {
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
	a.auditEvent(c.TenantID, c.Subject, "database.restore", "tenant", c.TenantID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}
