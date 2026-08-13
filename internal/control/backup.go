package control

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// backupDatabase 使用 VACUUM INTO 生成一致性在线备份并下载（仅 SQLite）。
func (a *App) backupDatabase(w http.ResponseWriter, r *http.Request, c claims) {
	if a.database == nil {
		writeError(w, http.StatusConflict, "database is managed by server environment settings")
		return
	}
	target := filepath.Join(os.TempDir(), fmt.Sprintf("wiremesh-backup-%d.db", time.Now().Unix()))
	if err := a.database.BackupSQLite(target); err != nil {
		writeError(w, http.StatusBadRequest, "database backup failed: "+err.Error())
		return
	}
	defer os.Remove(target)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="wiremesh-backup.db"`)
	http.ServeFile(w, r, target)
}

// restoreDatabase 接收上传的 SQLite 备份文件并热切换到恢复后的数据库。
func (a *App) restoreDatabase(w http.ResponseWriter, r *http.Request, c claims) {
	if a.database == nil {
		writeError(w, http.StatusConflict, "database is managed by server environment settings")
		return
	}
	defer r.Body.Close()
	target := filepath.Join(os.TempDir(), fmt.Sprintf("wiremesh-restore-%d.db", time.Now().Unix()))
	file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to receive backup")
		return
	}
	defer os.Remove(target)
	if _, err := io.Copy(file, io.LimitReader(r.Body, 512<<20)); err != nil {
		file.Close()
		writeError(w, http.StatusBadRequest, "failed to receive backup")
		return
	}
	if err := file.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store backup")
		return
	}
	if err := a.database.RestoreSQLite(r.Context(), target); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "database.restore", "tenant", c.TenantID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}
