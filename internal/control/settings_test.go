package control

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func authenticatedRequest(app *App, method, path, token, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	return response
}

func TestSystemSettingsAPIAndPermissions(t *testing.T) {
	app := testApp(t)
	admin, adminToken := initializeTestAdmin(t, app, "settings-admin@example.com", "strong-password")

	response := authenticatedRequest(app, http.MethodGet, "/api/v1/settings", adminToken, "")
	if response.Code != http.StatusOK {
		t.Fatalf("read settings: %d %s", response.Code, response.Body.String())
	}
	var settings SystemSettings
	if err := json.NewDecoder(response.Body).Decode(&settings); err != nil {
		t.Fatal(err)
	}
	if settings.DashboardName != "WireMesh 控制台" || settings.NetDefaults.Port != 51820 {
		t.Fatalf("unexpected default settings: %#v", settings)
	}

	settings.DashboardName = "Production WireMesh"
	payload, _ := json.Marshal(settings)
	response = authenticatedRequest(app, http.MethodPut, "/api/v1/settings", adminToken, string(payload))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Production WireMesh") {
		t.Fatalf("update settings: %d %s", response.Code, response.Body.String())
	}
	persisted, err := app.store.GetSettings(admin.TenantID)
	if err != nil || persisted.DashboardName != "Production WireMesh" {
		t.Fatalf("settings not persisted: %#v %v", persisted, err)
	}
	if events, err := app.store.ListAudit(admin.TenantID); err != nil || !hasAuditAction(events, "settings.update") {
		t.Fatalf("missing settings audit: %#v %v", events, err)
	}

	viewer := User{ID: "viewer_settings", TenantID: admin.TenantID, Email: "viewer-settings@example.com", Name: "Viewer", Role: RoleViewer, PasswordHash: "unused", CreatedAt: time.Now().UTC()}
	if err := app.store.CreateUser(viewer); err != nil {
		t.Fatal(err)
	}
	viewerToken := app.auth.issue(viewer)
	response = authenticatedRequest(app, http.MethodGet, "/api/v1/settings", viewerToken, "")
	if response.Code != http.StatusOK {
		t.Fatalf("viewer must be able to read settings: %d %s", response.Code, response.Body.String())
	}
	response = authenticatedRequest(app, http.MethodPut, "/api/v1/settings", viewerToken, string(payload))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("viewer changed settings: %d %s", response.Code, response.Body.String())
	}

	settings.NetDefaults.Port = 70000
	invalid, _ := json.Marshal(settings)
	response = authenticatedRequest(app, http.MethodPut, "/api/v1/settings", adminToken, string(invalid))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings accepted: %d %s", response.Code, response.Body.String())
	}
}

func TestSystemSettingsTenantIsolation(t *testing.T) {
	app := testApp(t)
	adminA, tokenA := initializeTestAdmin(t, app, "tenant-a@example.com", "strong-password")
	settingsA := defaultSystemSettings(adminA.TenantID)
	settingsA.DashboardName = "Tenant A"
	payload, _ := json.Marshal(settingsA)
	if response := authenticatedRequest(app, http.MethodPut, "/api/v1/settings", tokenA, string(payload)); response.Code != http.StatusOK {
		t.Fatalf("tenant A update failed: %d %s", response.Code, response.Body.String())
	}

	adminB := User{ID: "admin_b", TenantID: "tenant_b", Email: "tenant-b@example.com", Name: "Tenant B", Role: RoleAdmin, PasswordHash: "unused", CreatedAt: time.Now().UTC()}
	if err := app.store.CreateUser(adminB); err != nil {
		t.Fatal(err)
	}
	response := authenticatedRequest(app, http.MethodGet, "/api/v1/settings", app.auth.issue(adminB), "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "Tenant A") {
		t.Fatalf("settings leaked across tenants: %d %s", response.Code, response.Body.String())
	}
	if _, err := app.store.GetSettings(adminB.TenantID); !errorsIsNotFound(err) {
		t.Fatalf("tenant B should not acquire tenant A settings: %v", err)
	}
}

func TestNotificationAndUserManagementAPIs(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "management-admin@example.com", "strong-password")
	secretTarget := "https://hooks.example.com/services/very-secret-token"
	body := `{"name":"Operations","type":"webhook","target":"` + secretTarget + `","enabled":true,"agents":"all"}`
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/settings/notifications", token, body)
	if response.Code != http.StatusCreated {
		t.Fatalf("create notification: %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), secretTarget) || strings.Contains(response.Body.String(), "very-secret-token") {
		t.Fatalf("notification target leaked in response: %s", response.Body.String())
	}
	var channel notificationChannelResponse
	if err := json.NewDecoder(response.Body).Decode(&channel); err != nil {
		t.Fatal(err)
	}
	stored, err := app.store.GetNotificationChannel(admin.TenantID, channel.ID)
	if err != nil || stored.Target.Ciphertext == "" || app.decryptTarget(stored) != secretTarget {
		t.Fatalf("notification target not encrypted/persisted: %#v %v", stored, err)
	}
	auditEvents, err := app.store.ListAudit(admin.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range auditEvents {
		raw, _ := json.Marshal(event.Metadata)
		if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), secretTarget) {
			t.Fatalf("notification secret leaked in audit: %s", raw)
		}
	}

	otherAdmin := User{ID: "notification_other", TenantID: "notification_other_tenant", Email: "notification-other@example.com", Name: "Other", Role: RoleAdmin, PasswordHash: "unused", CreatedAt: time.Now().UTC()}
	if err := app.store.CreateUser(otherAdmin); err != nil {
		t.Fatal(err)
	}
	response = authenticatedRequest(app, http.MethodGet, "/api/v1/settings/notifications", app.auth.issue(otherAdmin), "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), channel.ID) {
		t.Fatalf("notification leaked across tenant: %d %s", response.Code, response.Body.String())
	}
	response = authenticatedRequest(app, http.MethodDelete, "/api/v1/settings/notifications/"+channel.ID, app.auth.issue(otherAdmin), "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("other tenant deleted channel: %d %s", response.Code, response.Body.String())
	}

	response = authenticatedRequest(app, http.MethodPost, "/api/v1/users", token, `{"name":"Operator","email":"operator@example.com","password":"operator-password","role":"operator"}`)
	if response.Code != http.StatusCreated || strings.Contains(response.Body.String(), "password") {
		t.Fatalf("create user: %d %s", response.Code, response.Body.String())
	}
	created, err := app.store.GetUserByEmail("operator@example.com")
	if err != nil || created.TenantID != admin.TenantID || created.PasswordHash == "" {
		t.Fatalf("created user invalid: %#v %v", created, err)
	}
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/users", token, `{"name":"Weak","email":"weak@example.com","password":"short","role":"viewer"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("weak password accepted: %d %s", response.Code, response.Body.String())
	}
	if events, err := app.store.ListAudit(admin.TenantID); err != nil || !hasAuditAction(events, "user.create") {
		t.Fatal("missing user creation audit")
	}
}

func TestResolveGeoIPPathExpandsAndNormalizesPath(t *testing.T) {
	expected := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")
	t.Setenv("WIREMESH_GEOIP_TEST_PATH", expected)
	resolved, err := resolveGeoIPPath(`  "${WIREMESH_GEOIP_TEST_PATH}"  `)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != filepath.Clean(expected) {
		t.Fatalf("resolved GeoIP path = %q, want %q", resolved, filepath.Clean(expected))
	}
}

func TestInvalidGeoIPPathKeepsConfiguredPath(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "geo-admin@example.com", "strong-password")
	settings := defaultSystemSettings(admin.TenantID)
	settings.GeoIPDBPath = "existing.mmdb"
	settings.UpdatedAt = time.Now().UTC()
	if err := app.store.UpsertSettings(settings); err != nil {
		t.Fatal(err)
	}
	response := authenticatedRequest(app, http.MethodPut, "/api/v1/settings/geoip", token, `{"dbPath":"missing.mmdb"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid GeoIP path accepted: %d %s", response.Code, response.Body.String())
	}
	persisted, err := app.store.GetSettings(admin.TenantID)
	if err != nil || persisted.GeoIPDBPath != "existing.mmdb" {
		t.Fatalf("invalid path replaced configured path: %#v %v", persisted, err)
	}
}

func hasAuditAction(events []AuditEvent, action string) bool {
	for _, event := range events {
		if event.Action == action {
			return true
		}
	}
	return false
}

func errorsIsNotFound(err error) bool { return err == errNotFound }

func TestSuccessfulLoginUpdatesLastLoginTime(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "last-login@example.com", "strong-password")
	if admin.LastLoginAt.IsZero() {
		t.Fatal("initial successful login did not return a login time")
	}
	firstLogin := admin.LastLoginAt

	failed := httptest.NewRecorder()
	app.Router().ServeHTTP(failed, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"last-login@example.com","password":"wrong-password"}`)))
	if failed.Code != http.StatusUnauthorized {
		t.Fatalf("invalid login returned %d: %s", failed.Code, failed.Body.String())
	}
	persisted, err := app.store.GetUser(admin.ID)
	if err != nil || !persisted.LastLoginAt.Equal(firstLogin) {
		t.Fatalf("failed login changed last login time: %#v %v", persisted.LastLoginAt, err)
	}

	time.Sleep(2 * time.Millisecond)
	successful := httptest.NewRecorder()
	app.Router().ServeHTTP(successful, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"last-login@example.com","password":"strong-password"}`)))
	if successful.Code != http.StatusOK {
		t.Fatalf("valid login returned %d: %s", successful.Code, successful.Body.String())
	}
	var session struct {
		User User `json:"user"`
	}
	if err := json.NewDecoder(successful.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if !session.User.LastLoginAt.After(firstLogin) {
		t.Fatalf("login response was not updated: first=%s current=%s", firstLogin, session.User.LastLoginAt)
	}
	persisted, err = app.store.GetUser(admin.ID)
	if err != nil || !persisted.LastLoginAt.Equal(session.User.LastLoginAt) {
		t.Fatalf("last login was not persisted: response=%s stored=%s err=%v", session.User.LastLoginAt, persisted.LastLoginAt, err)
	}

	response := authenticatedRequest(app, http.MethodGet, "/api/v1/users", token, "")
	if response.Code != http.StatusOK {
		t.Fatalf("list users: %d %s", response.Code, response.Body.String())
	}
	var users []User
	if err := json.NewDecoder(response.Body).Decode(&users); err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || !users[0].LastLoginAt.Equal(session.User.LastLoginAt) {
		t.Fatalf("users API omitted last login time: %#v", users)
	}
}

func TestNotificationChannelSpecificConfigAndTemplate(t *testing.T) {
	app := testApp(t)
	admin, token := initializeTestAdmin(t, app, "notify-config@example.com", "strong-password")
	tests := []struct {
		kind   string
		config NotificationConfig
		secret string
	}{
		{"webhook", NotificationConfig{URL: "https://hooks.example.com/wiremesh/private", Method: "POST", ContentType: "application/json", SignatureType: "hmac-sha256", Secret: "webhook-secret", TimeoutSec: 8}, "webhook-secret"},
		{"dingtalk", NotificationConfig{URL: "https://oapi.dingtalk.com/robot/send?access_token=private", Secret: "dingtalk-secret", MessageType: "markdown", TimeoutSec: 8}, "dingtalk-secret"},
		{"wecom", NotificationConfig{URL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=private", MessageType: "text", AtMobiles: []string{"13800138000"}, AtUserIDs: []string{"operator"}, TimeoutSec: 8}, "private"},
		{"feishu", NotificationConfig{URL: "https://open.feishu.cn/open-apis/bot/v2/hook/private", Secret: "feishu-secret", MessageType: "post", TimeoutSec: 8}, "feishu-secret"},
		{"telegram", NotificationConfig{BotToken: "123456:telegram-secret", ChatID: "-100123456", ThreadID: "42", ParseMode: "HTML", TimeoutSec: 8, UseProxy: true, ProxyURL: "socks5://proxy-user:proxy-password@127.0.0.1:1080"}, "telegram-secret"},
		{"email", NotificationConfig{SMTPHost: "smtp.example.com", SMTPPort: 587, Username: "wiremesh", Password: "smtp-secret", FromAddress: "wiremesh@example.com", FromName: "WireMesh", To: []string{"ops@example.com"}, CC: []string{"manager@example.com"}, Encryption: "starttls", TimeoutSec: 10}, "smtp-secret"},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]any{"name": "Channel " + test.kind, "type": test.kind, "config": test.config, "template": "{{.Title}} / {{.NodeName}} / {{.Message}}", "subjectTemplate": "Alert: {{.Title}}", "enabled": true, "agents": "all"})
			response := authenticatedRequest(app, http.MethodPost, "/api/v1/settings/notifications", token, string(payload))
			if response.Code != http.StatusCreated {
				t.Fatalf("create %s: %d %s", test.kind, response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), test.secret) || (test.config.URL != "" && strings.Contains(response.Body.String(), test.config.URL)) || (test.config.ProxyURL != "" && strings.Contains(response.Body.String(), test.config.ProxyURL)) || strings.Contains(response.Body.String(), "proxy-password") || strings.Contains(response.Body.String(), "ops@example.com") || strings.Contains(response.Body.String(), "13800138000") || strings.Contains(response.Body.String(), "operator") {
				t.Fatalf("%s secret or destination leaked: %s", test.kind, response.Body.String())
			}
			var public notificationChannelResponse
			if err := json.NewDecoder(response.Body).Decode(&public); err != nil {
				t.Fatal(err)
			}
			if public.Template != "{{.Title}} / {{.NodeName}} / {{.Message}}" {
				t.Fatalf("template not returned for %s: %#v", test.kind, public)
			}
			stored, err := app.store.GetNotificationChannel(admin.TenantID, public.ID)
			if err != nil {
				t.Fatal(err)
			}
			envelope, err := app.decryptNotificationEnvelope(stored)
			if err != nil || envelope.Version != 2 || envelope.Template != public.Template {
				t.Fatalf("stored envelope invalid for %s: %#v %v", test.kind, envelope, err)
			}
			switch test.kind {
			case "telegram":
				if envelope.Config.BotToken != test.config.BotToken || envelope.Config.ProxyURL != test.config.ProxyURL || !envelope.Config.UseProxy || !public.Config.BotTokenConfigured || public.Config.BotToken != "" || !public.Config.ProxyURLConfigured || public.Config.ProxyURL != "" || !public.Config.UseProxy {
					t.Fatalf("telegram token or proxy handling invalid: %#v %#v", envelope.Config, public.Config)
				}
			case "email":
				if envelope.Config.Password != test.config.Password || len(envelope.Config.To) != 1 || !public.Config.PasswordConfigured || !public.Config.RecipientsConfigured || len(public.Config.To) != 0 {
					t.Fatalf("email secret handling invalid: %#v %#v", envelope.Config, public.Config)
				}
			case "wecom":
				if envelope.Config.URL != test.config.URL || len(envelope.Config.AtMobiles) != 1 || len(envelope.Config.AtUserIDs) != 1 || !public.Config.URLConfigured || !public.Config.AtMobilesConfigured || public.Config.AtMobileCount != 1 || len(public.Config.AtMobiles) != 0 || !public.Config.AtUserIDsConfigured || public.Config.AtUserIDCount != 1 || len(public.Config.AtUserIDs) != 0 {
					t.Fatalf("wecom destination handling invalid: %#v %#v", envelope.Config, public.Config)
				}
			default:
				if envelope.Config.URL != test.config.URL || !public.Config.URLConfigured || public.Config.URL != "" {
					t.Fatalf("URL handling invalid for %s: %#v %#v", test.kind, envelope.Config, public.Config)
				}
			}
		})
	}
}

func TestValidateNotificationProxyURL(t *testing.T) {
	valid := []string{
		"http://127.0.0.1:7890",
		"https://proxy.example.com:8443",
		"socks5://user:password@127.0.0.1:1080",
		"socks5h://proxy.example.com:1080",
	}
	for _, value := range valid {
		if err := validateNotificationProxyURL(value); err != nil {
			t.Fatalf("valid proxy %q rejected: %v", value, err)
		}
	}
	invalid := []string{"", "ftp://proxy.example.com:21", "http://", "socks5://proxy.example.com:70000", "http://proxy.example.com/path"}
	for _, value := range invalid {
		if err := validateNotificationProxyURL(value); err == nil {
			t.Fatalf("invalid proxy %q accepted", value)
		}
	}
}

func TestNotificationTemplateValidationAndRenderedWebhookTest(t *testing.T) {
	app := testApp(t)
	_, token := initializeTestAdmin(t, app, "notify-template@example.com", "strong-password")
	invalid := `{"name":"Invalid","type":"webhook","config":{"url":"https://hooks.example.com/test","method":"POST","contentType":"application/json","signatureType":"none","timeoutSec":8},"template":"{{.Missing","enabled":true,"agents":"all"}`
	response := authenticatedRequest(app, http.MethodPost, "/api/v1/settings/notifications", token, invalid)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "模板") {
		t.Fatalf("invalid template accepted: %d %s", response.Code, response.Body.String())
	}

	bodyReceived := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodyReceived <- string(raw)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	payload, _ := json.Marshal(map[string]any{
		"name": "Rendered webhook", "type": "webhook",
		"config":   NotificationConfig{URL: server.URL, Method: "POST", ContentType: "application/json", SignatureType: "none", TimeoutSec: 5, AllowPrivate: true},
		"template": `{"title":{{json .Title}},"node":{{json .NodeName}},"message":{{json .Message}}}`,
		"enabled":  true, "agents": "all",
	})
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/settings/notifications", token, string(payload))
	if response.Code != http.StatusCreated {
		t.Fatalf("create webhook: %d %s", response.Code, response.Body.String())
	}
	var channel notificationChannelResponse
	if err := json.NewDecoder(response.Body).Decode(&channel); err != nil {
		t.Fatal(err)
	}
	response = authenticatedRequest(app, http.MethodPost, "/api/v1/settings/notifications/"+channel.ID+"/test", token, "")
	if response.Code != http.StatusOK {
		t.Fatalf("test webhook: %d %s", response.Code, response.Body.String())
	}
	select {
	case body := <-bodyReceived:
		if !json.Valid([]byte(body)) || !strings.Contains(body, "通知渠道测试") || !strings.Contains(body, "系统") {
			t.Fatalf("unexpected rendered body: %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook did not receive the rendered notification")
	}
}
