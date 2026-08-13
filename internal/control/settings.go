package control

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

type geoReaderState struct {
	Reader     *maxminddb.Reader
	Path       string
	Version    string
	UpdatedAt  time.Time
	EntryCount uint64
}

type geoIPStatusResponse struct {
	DBPath     string    `json:"dbPath"`
	Version    string    `json:"version"`
	UpdatedAt  time.Time `json:"updatedAt"`
	EntryCount uint64    `json:"entryCount"`
}

func configuredGeoIPDBPath() string {
	return strings.TrimSpace(os.Getenv("WIREMESH_GEOIP_DB"))
}

func defaultSystemSettings(tenant string) SystemSettings {
	return SystemSettings{
		TenantID: tenant, DashboardName: "WireMesh 控制台", SessionTimeoutMin: 120, GeoIPDBPath: configuredGeoIPDBPath(),
		NetDefaults: NetworkDefaults{Port: 51820, MTU: 1420, Keepalive: 25, DefaultTopology: "full-mesh"},
		StatusRules: StatusRules{AgentOfflineSec: 120, HandshakeSec: 180, RedFailCount: 3},
		Collect:     CollectionSettings{ReportSec: 10, ProbeSec: 15, MapRefreshSec: 30},
		Retention:   RetentionSettings{}, Agent: AgentSettings{UpgradePolicy: "manual"},
	}
}

func (a *App) tenantSettings(tenant string) (SystemSettings, error) {
	settings, err := a.store.GetSettings(tenant)
	if errors.Is(err, errNotFound) {
		return defaultSystemSettings(tenant), nil
	}
	if err == nil && strings.TrimSpace(settings.GeoIPDBPath) == "" {
		settings.GeoIPDBPath = configuredGeoIPDBPath()
	}
	return settings, err
}

func (a *App) settings(w http.ResponseWriter, r *http.Request, c claims) {
	if r.Method == http.MethodGet {
		settings, err := a.tenantSettings(c.TenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取设置失败")
			return
		}
		writeJSON(w, http.StatusOK, settings)
		return
	}
	var in SystemSettings
	if !decode(w, r, &in) {
		return
	}
	in.DashboardName = strings.TrimSpace(in.DashboardName)
	in.NetDefaults.DNS = strings.TrimSpace(in.NetDefaults.DNS)
	in.Agent.Labels = strings.TrimSpace(in.Agent.Labels)
	if err := validateSystemSettings(in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := a.tenantSettings(c.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设置失败")
		return
	}
	in.TenantID, in.GeoIPDBPath, in.UpdatedAt = c.TenantID, current.GeoIPDBPath, time.Now().UTC()
	if err := a.store.UpsertSettings(in); err != nil {
		writeError(w, http.StatusInternalServerError, "保存设置失败")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "settings.update", "settings", c.TenantID, nil)
	writeJSON(w, http.StatusOK, in)
}

func validateSystemSettings(v SystemSettings) error {
	between := func(name string, value, minValue, maxValue int) error {
		if value < minValue || value > maxValue {
			return fmt.Errorf("%s must be between %d and %d", name, minValue, maxValue)
		}
		return nil
	}
	if v.DashboardName == "" || len([]rune(v.DashboardName)) > 80 {
		return errors.New("dashboardName is required and must not exceed 80 characters")
	}
	checks := []struct {
		name                      string
		value, minValue, maxValue int
	}{
		{"sessionTimeoutMin", v.SessionTimeoutMin, 5, 1440},
		{"netDefaults.port", v.NetDefaults.Port, 1, 65535},
		{"netDefaults.mtu", v.NetDefaults.MTU, 576, 9000},
		{"netDefaults.keepalive", v.NetDefaults.Keepalive, 0, 3600},
		{"statusRules.agentOfflineSec", v.StatusRules.AgentOfflineSec, 5, 86400},
		{"statusRules.handshakeSec", v.StatusRules.HandshakeSec, 5, 86400},
		{"statusRules.redFailCount", v.StatusRules.RedFailCount, 1, 100},
		{"collect.reportSec", v.Collect.ReportSec, 5, 86400},
		{"collect.probeSec", v.Collect.ProbeSec, 5, 86400},
		{"collect.mapRefreshSec", v.Collect.MapRefreshSec, 5, 86400},
		{"retention.rawDays", v.Retention.RawDays, 0, 3650},
		{"retention.hourlyDays", v.Retention.HourlyDays, 0, 3650},
		{"retention.dailyDays", v.Retention.DailyDays, 0, 3650},
	}
	for _, check := range checks {
		if err := between(check.name, check.value, check.minValue, check.maxValue); err != nil {
			return err
		}
	}
	if v.NetDefaults.DefaultTopology != "full-mesh" && v.NetDefaults.DefaultTopology != "hub-spoke" && v.NetDefaults.DefaultTopology != "custom" {
		return errors.New("netDefaults.defaultTopology is invalid")
	}
	if v.Agent.UpgradePolicy != "manual" && v.Agent.UpgradePolicy != "auto-stable" {
		return errors.New("agent.upgradePolicy is invalid")
	}
	return nil
}

func (a *App) geoIPStatus(w http.ResponseWriter, r *http.Request, c claims) {
	settings, err := a.tenantSettings(c.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 GeoIP 设置失败")
		return
	}
	a.geoMu.RLock()
	state := a.geoReaders[c.TenantID]
	a.geoMu.RUnlock()
	if state == nil && settings.GeoIPDBPath != "" {
		_, _ = a.loadGeoIP(c.TenantID, settings.GeoIPDBPath)
		a.geoMu.RLock()
		state = a.geoReaders[c.TenantID]
		a.geoMu.RUnlock()
	}
	response := geoIPStatusResponse{DBPath: settings.GeoIPDBPath}
	if state != nil {
		response = geoIPStatusResponse{DBPath: state.Path, Version: state.Version, UpdatedAt: state.UpdatedAt, EntryCount: state.EntryCount}
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) updateGeoIP(w http.ResponseWriter, r *http.Request, c claims) {
	var in struct {
		DBPath string `json:"dbPath"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.DBPath = strings.TrimSpace(in.DBPath)
	if in.DBPath == "" {
		writeError(w, http.StatusBadRequest, "dbPath 不能为空")
		return
	}
	loadedPath, err := a.loadGeoIP(c.TenantID, in.DBPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "GeoIP 数据库加载失败："+err.Error())
		return
	}
	settings, err := a.tenantSettings(c.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设置失败")
		return
	}
	settings.GeoIPDBPath, settings.UpdatedAt = loadedPath, time.Now().UTC()
	if err := a.store.UpsertSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, "保存 GeoIP 路径失败")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "geoip.update", "settings", c.TenantID, map[string]string{"path": loadedPath})
	a.geoIPStatus(w, r, c)
}

func (a *App) reloadGeoIP(w http.ResponseWriter, r *http.Request, c claims) {
	settings, err := a.tenantSettings(c.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设置失败")
		return
	}
	if settings.GeoIPDBPath == "" {
		writeError(w, http.StatusBadRequest, "尚未配置 GeoIP 数据库路径")
		return
	}
	if _, err := a.loadGeoIP(c.TenantID, settings.GeoIPDBPath); err != nil {
		writeError(w, http.StatusBadRequest, "GeoIP 数据库重新加载失败："+err.Error())
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "geoip.reload", "settings", c.TenantID, nil)
	a.geoIPStatus(w, r, c)
}

func resolveGeoIPPath(path string) (string, error) {
	path = strings.Trim(strings.TrimSpace(path), "\"'")
	if path == "" {
		return "", errors.New("数据库路径不能为空")
	}
	path = os.ExpandEnv(path)
	if path == "~" || strings.HasPrefix(path, "~"+string(filepath.Separator)) || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("解析用户目录失败：%w", err)
		}
		path = filepath.Join(home, strings.TrimLeft(path[1:], "/\\"))
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("解析数据库路径失败：%w", err)
	}
	return filepath.Clean(absolute), nil
}

func (a *App) loadGeoIP(tenant, path string) (string, error) {
	resolved, err := resolveGeoIPPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("文件不存在：%s", resolved)
		}
		return "", fmt.Errorf("无法访问 %s：%w", resolved, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("路径指向目录而不是 mmdb 文件：%s", resolved)
	}
	reader, err := maxminddb.Open(resolved)
	if err != nil {
		return "", fmt.Errorf("无法打开 %s：%w", resolved, err)
	}
	if err := reader.Verify(); err != nil {
		_ = reader.Close()
		return "", fmt.Errorf("文件校验失败：%w", err)
	}
	state := &geoReaderState{Reader: reader, Path: resolved, Version: reader.Metadata.DatabaseType, UpdatedAt: info.ModTime().UTC(), EntryCount: uint64(reader.Metadata.NodeCount)}
	a.geoMu.Lock()
	previous := a.geoReaders[tenant]
	a.geoReaders[tenant] = state
	a.geoMu.Unlock()
	if previous != nil {
		_ = previous.Reader.Close()
	}
	return resolved, nil
}

func (a *App) lookupGeoIP(w http.ResponseWriter, r *http.Request, c claims) {
	ipText := strings.TrimSpace(r.URL.Query().Get("ip"))
	location, err := a.geoLookup(c.TenantID, ipText)
	if err != nil {
		switch {
		case errors.Is(err, errGeoIPUnavailable):
			writeError(w, http.StatusConflict, "GeoIP 数据库未加载")
		case errors.Is(err, errGeoIPNotFound):
			writeError(w, http.StatusNotFound, "未找到该 IP 的地理位置信息")
		case strings.Contains(err.Error(), "valid public IP"):
			writeError(w, http.StatusBadRequest, "请输入有效的公网 IP 地址")
		default:
			writeError(w, http.StatusInternalServerError, "GeoIP 查询失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ip": location.PublicIP, "city": location.City, "country": location.Country,
		"countryCode": location.CountryCode, "latitude": location.Latitude,
		"longitude": location.Longitude, "timezone": location.Timezone,
	})
}

func (a *App) notificationLogs(w http.ResponseWriter, r *http.Request, c claims) {
	items, err := a.store.ListNotificationLogs(c.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取通知日志失败")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *App) users(w http.ResponseWriter, r *http.Request, c claims) {
	if r.Method == http.MethodGet {
		users, err := a.store.ListUsers(c.TenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取用户列表失败")
			return
		}
		out := make([]map[string]interface{}, 0, len(users))
		for _, user := range users {
			item := publicUser(user)
			item["created_at"] = user.CreatedAt
			out = append(out, item)
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	var in struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     Role   `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name, in.Email = strings.TrimSpace(in.Name), strings.ToLower(strings.TrimSpace(in.Email))
	parsed, err := mail.ParseAddress(in.Email)
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "名称不能为空")
		return
	}
	if err != nil || strings.ToLower(parsed.Address) != in.Email {
		writeError(w, http.StatusBadRequest, "请输入有效的邮箱地址")
		return
	}
	if len(in.Password) < 8 {
		writeError(w, http.StatusBadRequest, "密码至少需要 8 个字符")
		return
	}
	if in.Role != RoleViewer && in.Role != RoleOperator && in.Role != RoleAdmin {
		writeError(w, http.StatusBadRequest, "角色无效")
		return
	}
	passwordHash, err := hashPassword(in.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}
	user := User{ID: newID("usr"), TenantID: c.TenantID, Email: in.Email, Name: in.Name, Role: in.Role, PasswordHash: passwordHash, CreatedAt: time.Now().UTC()}
	if err := a.store.CreateUser(user); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "user.create", "user", user.ID, map[string]string{"role": string(user.Role)})
	writeJSON(w, http.StatusCreated, publicUser(user))
}
