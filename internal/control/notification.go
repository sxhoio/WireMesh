package control

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const defaultNotificationTemplate = `【{{.Title}}】
事件：{{.Event}}
节点：{{.NodeName}}
状态：{{.NodeStatus}}
详情：{{.Message}}
时间：{{.OccurredAt}}`

const defaultNotificationSubjectTemplate = `WireMesh：{{.Title}}`

const defaultWebhookTemplate = `{"event":{{json .Event}},"title":{{json .Title}},"message":{{json .Message}},"nodeName":{{json .NodeName}},"nodeStatus":{{json .NodeStatus}},"occurredAt":{{json .OccurredAt}}}`

type NotificationHeader struct {
	Name            string `json:"name"`
	Value           string `json:"value,omitempty"`
	ValueConfigured bool   `json:"valueConfigured,omitempty"`
}

type NotificationConfig struct {
	URL                   string               `json:"url,omitempty"`
	URLConfigured         bool                 `json:"urlConfigured,omitempty"`
	Method                string               `json:"method,omitempty"`
	ContentType           string               `json:"contentType,omitempty"`
	Headers               []NotificationHeader `json:"headers,omitempty"`
	SignatureType         string               `json:"signatureType,omitempty"`
	Secret                string               `json:"secret,omitempty"`
	SecretConfigured      bool                 `json:"secretConfigured,omitempty"`
	TimeoutSec            int                  `json:"timeoutSec,omitempty"`
	AllowPrivate          bool                 `json:"allowPrivate,omitempty"`
	MessageType           string               `json:"messageType,omitempty"`
	AtAll                 bool                 `json:"atAll,omitempty"`
	AtMobiles             []string             `json:"atMobiles,omitempty"`
	AtMobilesConfigured   bool                 `json:"atMobilesConfigured,omitempty"`
	AtMobileCount         int                  `json:"atMobileCount,omitempty"`
	AtUserIDs             []string             `json:"atUserIds,omitempty"`
	AtUserIDsConfigured   bool                 `json:"atUserIdsConfigured,omitempty"`
	AtUserIDCount         int                  `json:"atUserIdCount,omitempty"`
	BotToken              string               `json:"botToken,omitempty"`
	BotTokenConfigured    bool                 `json:"botTokenConfigured,omitempty"`
	UseProxy              bool                 `json:"useProxy,omitempty"`
	ProxyURL              string               `json:"proxyUrl,omitempty"`
	ProxyURLConfigured    bool                 `json:"proxyUrlConfigured,omitempty"`
	ChatID                string               `json:"chatId,omitempty"`
	ChatIDConfigured      bool                 `json:"chatIdConfigured,omitempty"`
	ThreadID              string               `json:"threadId,omitempty"`
	ParseMode             string               `json:"parseMode,omitempty"`
	DisableWebPagePreview bool                 `json:"disableWebPagePreview,omitempty"`
	DisableNotification   bool                 `json:"disableNotification,omitempty"`
	SMTPHost              string               `json:"smtpHost,omitempty"`
	SMTPPort              int                  `json:"smtpPort,omitempty"`
	Username              string               `json:"username,omitempty"`
	Password              string               `json:"password,omitempty"`
	PasswordConfigured    bool                 `json:"passwordConfigured,omitempty"`
	FromAddress           string               `json:"fromAddress,omitempty"`
	FromName              string               `json:"fromName,omitempty"`
	To                    []string             `json:"to,omitempty"`
	RecipientsConfigured  bool                 `json:"recipientsConfigured,omitempty"`
	RecipientCount        int                  `json:"recipientCount,omitempty"`
	CC                    []string             `json:"cc,omitempty"`
	CCConfigured          bool                 `json:"ccConfigured,omitempty"`
	CCCount               int                  `json:"ccCount,omitempty"`
	Encryption            string               `json:"encryption,omitempty"`
	SkipTLSVerify         bool                 `json:"skipTlsVerify,omitempty"`
}

type notificationChannelEnvelope struct {
	Version         int                `json:"version"`
	Config          NotificationConfig `json:"config"`
	Template        string             `json:"template"`
	SubjectTemplate string             `json:"subjectTemplate,omitempty"`
}

type notificationChannelResponse struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Type            string             `json:"type"`
	Config          NotificationConfig `json:"config"`
	Template        string             `json:"template"`
	SubjectTemplate string             `json:"subjectTemplate,omitempty"`
	Enabled         bool               `json:"enabled"`
	Agents          interface{}        `json:"agents"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
}

type notificationTemplateData struct {
	Event        string
	Title        string
	Message      string
	NodeName     string
	NodeID       string
	NodeStatus   string
	NetworkName  string
	ProjectName  string
	Endpoint     string
	Region       string
	OS           string
	AgentVersion string
	OccurredAt   string
	DashboardURL string
}

func defaultNotificationTemplateFor(kind string) string {
	if kind == "webhook" {
		return defaultWebhookTemplate
	}
	return defaultNotificationTemplate
}

func notificationTemplateFunctions() template.FuncMap {
	return template.FuncMap{"json": func(value any) string { raw, _ := json.Marshal(value); return string(raw) }}
}

func defaultNotificationConfig(kind string) NotificationConfig {
	switch kind {
	case "webhook":
		return NotificationConfig{Method: http.MethodPost, ContentType: "application/json", SignatureType: "none", TimeoutSec: 8}
	case "dingtalk":
		return NotificationConfig{MessageType: "markdown", TimeoutSec: 8}
	case "wecom":
		return NotificationConfig{MessageType: "markdown", TimeoutSec: 8}
	case "feishu":
		return NotificationConfig{MessageType: "text", TimeoutSec: 8}
	case "telegram":
		return NotificationConfig{ParseMode: "HTML", TimeoutSec: 8, DisableWebPagePreview: true}
	case "email":
		return NotificationConfig{SMTPPort: 587, Encryption: "starttls", TimeoutSec: 10}
	default:
		return NotificationConfig{TimeoutSec: 8}
	}
}

func (a *App) notificationChannels(w http.ResponseWriter, r *http.Request, c claims) {
	if r.Method == http.MethodGet {
		channels, err := a.store.ListNotificationChannels(c.TenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取通知渠道列表失败")
			return
		}
		out := make([]notificationChannelResponse, 0, len(channels))
		for _, channel := range channels {
			out = append(out, a.publicNotificationChannel(channel))
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	var in struct {
		Name            string             `json:"name"`
		Type            string             `json:"type"`
		Target          string             `json:"target"`
		Config          NotificationConfig `json:"config"`
		Template        string             `json:"template"`
		SubjectTemplate string             `json:"subjectTemplate"`
		Enabled         bool               `json:"enabled"`
		Agents          interface{}        `json:"agents"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Target != "" && in.Config.URL == "" {
		in.Config.URL = in.Target
	}
	channel, err := a.notificationFromInput(c.TenantID, NotificationChannel{}, in.Name, in.Type, in.Config, in.Template, in.SubjectTemplate, in.Enabled, in.Agents)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC()
	channel.ID, channel.CreatedAt, channel.UpdatedAt = newID("notify"), now, now
	if err := a.store.CreateNotificationChannel(channel); err != nil {
		writeError(w, http.StatusInternalServerError, "创建通知渠道失败")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "notification.create", "notification_channel", channel.ID, map[string]string{"type": channel.Type})
	writeJSON(w, http.StatusCreated, a.publicNotificationChannel(channel))
}

func (a *App) updateNotificationChannel(w http.ResponseWriter, r *http.Request, c claims) {
	current, err := a.store.GetNotificationChannel(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "通知渠道不存在")
		return
	}
	var in struct {
		Name            string             `json:"name"`
		Type            string             `json:"type"`
		Target          string             `json:"target"`
		Config          NotificationConfig `json:"config"`
		Template        string             `json:"template"`
		SubjectTemplate string             `json:"subjectTemplate"`
		Enabled         bool               `json:"enabled"`
		Agents          interface{}        `json:"agents"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Target != "" && in.Config.URL == "" {
		in.Config.URL = in.Target
	}
	channel, err := a.notificationFromInput(c.TenantID, current, in.Name, in.Type, in.Config, in.Template, in.SubjectTemplate, in.Enabled, in.Agents)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	channel.ID, channel.CreatedAt, channel.UpdatedAt = current.ID, current.CreatedAt, time.Now().UTC()
	if err := a.store.UpdateNotificationChannel(channel); err != nil {
		writeError(w, http.StatusInternalServerError, "更新通知渠道失败")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "notification.update", "notification_channel", channel.ID, map[string]string{"type": channel.Type})
	writeJSON(w, http.StatusOK, a.publicNotificationChannel(channel))
}

func (a *App) notificationFromInput(tenant string, current NotificationChannel, name, kind string, input NotificationConfig, bodyTemplate, subjectTemplate string, enabled bool, agents interface{}) (NotificationChannel, error) {
	name, kind = strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(kind))
	if name == "" || len([]rune(name)) > 80 {
		return NotificationChannel{}, errors.New("name is required and must not exceed 80 characters")
	}
	allowedTypes := map[string]bool{"webhook": true, "dingtalk": true, "wecom": true, "feishu": true, "telegram": true, "email": true}
	if !allowedTypes[kind] {
		return NotificationChannel{}, errors.New("notification type is invalid")
	}
	currentEnvelope := notificationChannelEnvelope{}
	if current.Target.Ciphertext != "" && current.Type == kind {
		currentEnvelope, _ = a.decryptNotificationEnvelope(current)
	}
	config := mergeNotificationConfig(kind, currentEnvelope.Config, input)
	if bodyTemplate == "" {
		if currentEnvelope.Template != "" && current.Type == kind {
			bodyTemplate = currentEnvelope.Template
		} else {
			bodyTemplate = defaultNotificationTemplate
		}
	}
	if subjectTemplate == "" {
		if currentEnvelope.SubjectTemplate != "" && current.Type == kind {
			subjectTemplate = currentEnvelope.SubjectTemplate
		} else {
			subjectTemplate = defaultNotificationSubjectTemplate
		}
	}
	if err := validateNotificationConfig(kind, config); err != nil {
		return NotificationChannel{}, err
	}
	if err := validateNotificationTemplate("消息模板", bodyTemplate); err != nil {
		return NotificationChannel{}, err
	}
	if err := validateNotificationTemplate("主题模板", subjectTemplate); err != nil {
		return NotificationChannel{}, err
	}
	allAgents, ids, err := parseNotificationAgents(agents)
	if err != nil {
		return NotificationChannel{}, err
	}
	for _, id := range ids {
		if _, err := a.store.GetNode(tenant, id); err != nil {
			return NotificationChannel{}, fmt.Errorf("agent %s does not exist", id)
		}
	}
	raw, err := json.Marshal(notificationChannelEnvelope{Version: 2, Config: config, Template: bodyTemplate, SubjectTemplate: subjectTemplate})
	if err != nil {
		return NotificationChannel{}, errors.New("failed to encode notification configuration")
	}
	secret, err := a.box.Encrypt(raw)
	if err != nil {
		return NotificationChannel{}, errors.New("failed to encrypt notification configuration")
	}
	return NotificationChannel{TenantID: tenant, Name: name, Type: kind, Target: secret, Enabled: enabled, AllAgents: allAgents, AgentIDs: ids}, nil
}

func mergeNotificationConfig(kind string, current, input NotificationConfig) NotificationConfig {
	base := defaultNotificationConfig(kind)
	if current.TimeoutSec != 0 {
		base = current
	}
	if input.URL != "" {
		base.URL = strings.TrimSpace(input.URL)
	}
	if input.Method != "" {
		base.Method = strings.ToUpper(strings.TrimSpace(input.Method))
	}
	if input.ContentType != "" {
		base.ContentType = strings.TrimSpace(input.ContentType)
	}
	if input.Headers != nil {
		old := map[string]string{}
		for _, h := range base.Headers {
			old[strings.ToLower(h.Name)] = h.Value
		}
		base.Headers = make([]NotificationHeader, 0, len(input.Headers))
		for _, h := range input.Headers {
			h.Name = strings.TrimSpace(h.Name)
			if h.Value == "" {
				h.Value = old[strings.ToLower(h.Name)]
			}
			base.Headers = append(base.Headers, NotificationHeader{Name: h.Name, Value: h.Value})
		}
	}
	if input.SignatureType != "" {
		base.SignatureType = strings.ToLower(strings.TrimSpace(input.SignatureType))
	}
	if input.Secret != "" {
		base.Secret = input.Secret
	}
	if input.TimeoutSec > 0 {
		base.TimeoutSec = input.TimeoutSec
	}
	base.AllowPrivate = input.AllowPrivate
	if input.MessageType != "" {
		base.MessageType = strings.ToLower(strings.TrimSpace(input.MessageType))
	}
	base.AtAll = input.AtAll
	if input.AtMobiles != nil {
		base.AtMobiles = cleanStringList(input.AtMobiles)
	}
	if input.AtUserIDs != nil {
		base.AtUserIDs = cleanStringList(input.AtUserIDs)
	}
	if input.BotToken != "" {
		base.BotToken = strings.TrimSpace(input.BotToken)
	}
	base.UseProxy = input.UseProxy
	if input.ProxyURL != "" {
		base.ProxyURL = strings.TrimSpace(input.ProxyURL)
	}
	if input.ChatID != "" {
		base.ChatID = strings.TrimSpace(input.ChatID)
	}
	base.ThreadID = strings.TrimSpace(input.ThreadID)
	if input.ParseMode != "" || kind == "telegram" {
		base.ParseMode = strings.TrimSpace(input.ParseMode)
	}
	base.DisableWebPagePreview = input.DisableWebPagePreview
	base.DisableNotification = input.DisableNotification
	if input.SMTPHost != "" {
		base.SMTPHost = strings.TrimSpace(input.SMTPHost)
	}
	if input.SMTPPort > 0 {
		base.SMTPPort = input.SMTPPort
	}
	if input.Username != "" {
		base.Username = strings.TrimSpace(input.Username)
	}
	if input.Password != "" {
		base.Password = input.Password
	}
	if input.FromAddress != "" {
		base.FromAddress = strings.TrimSpace(input.FromAddress)
	}
	if input.FromName != "" {
		base.FromName = strings.TrimSpace(input.FromName)
	}
	if input.To != nil && len(cleanStringList(input.To)) > 0 {
		base.To = cleanStringList(input.To)
	}
	if input.CC != nil {
		base.CC = cleanStringList(input.CC)
	}
	if input.Encryption != "" {
		base.Encryption = strings.ToLower(strings.TrimSpace(input.Encryption))
	}
	base.SkipTLSVerify = input.SkipTLSVerify
	return base
}

func cleanStringList(values []string) []string {
	out, seen := make([]string, 0, len(values)), map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			out = append(out, value)
			seen[value] = true
		}
	}
	return out
}

func validateNotificationConfig(kind string, c NotificationConfig) error {
	if c.TimeoutSec < 1 || c.TimeoutSec > 60 {
		return errors.New("timeoutSec must be between 1 and 60")
	}
	switch kind {
	case "webhook":
		if err := validateHTTPNotificationURL(c.URL); err != nil {
			return err
		}
		if c.Method != "POST" && c.Method != "PUT" && c.Method != "PATCH" {
			return errors.New("webhook method must be POST, PUT, or PATCH")
		}
		if len(c.Headers) > 20 {
			return errors.New("webhook headers must not exceed 20 items")
		}
		for _, h := range c.Headers {
			if h.Name == "" || strings.ContainsAny(h.Name, "\r\n:") || strings.ContainsAny(h.Value, "\r\n") {
				return errors.New("webhook contains an invalid header")
			}
		}
		if c.SignatureType != "none" && c.SignatureType != "hmac-sha256" && c.SignatureType != "bearer" {
			return errors.New("webhook signatureType is invalid")
		}
		if c.SignatureType != "none" && c.Secret == "" {
			return errors.New("webhook signature secret is required")
		}
	case "dingtalk":
		if err := validateHTTPNotificationURL(c.URL); err != nil {
			return err
		}
		if c.MessageType != "text" && c.MessageType != "markdown" {
			return errors.New("DingTalk messageType must be text or markdown")
		}
	case "wecom":
		if err := validateHTTPNotificationURL(c.URL); err != nil {
			return err
		}
		if c.MessageType != "text" && c.MessageType != "markdown" {
			return errors.New("WeCom messageType must be text or markdown")
		}
	case "feishu":
		if err := validateHTTPNotificationURL(c.URL); err != nil {
			return err
		}
		if c.MessageType != "text" && c.MessageType != "post" {
			return errors.New("Feishu messageType must be text or post")
		}
	case "telegram":
		if c.BotToken == "" {
			return errors.New("Telegram botToken is required")
		}
		if c.ChatID == "" {
			return errors.New("Telegram chatId is required")
		}
		if c.UseProxy && c.ProxyURL == "" {
			return errors.New("Telegram proxyUrl is required when proxy is enabled")
		}
		if c.ProxyURL != "" {
			if err := validateNotificationProxyURL(c.ProxyURL); err != nil {
				return err
			}
		}
		if c.ParseMode != "" && c.ParseMode != "HTML" && c.ParseMode != "MarkdownV2" {
			return errors.New("Telegram parseMode is invalid")
		}
		if c.ThreadID != "" {
			if _, err := strconv.ParseInt(c.ThreadID, 10, 64); err != nil {
				return errors.New("Telegram threadId must be an integer")
			}
		}
	case "email":
		if c.SMTPHost == "" {
			return errors.New("SMTP host is required")
		}
		if c.SMTPPort < 1 || c.SMTPPort > 65535 {
			return errors.New("SMTP port is invalid")
		}
		if c.Username != "" && c.Password == "" {
			return errors.New("SMTP password is required when username is configured")
		}
		if _, err := mail.ParseAddress(c.FromAddress); err != nil {
			return errors.New("valid sender address is required")
		}
		if len(c.To) == 0 {
			return errors.New("at least one email recipient is required")
		}
		for _, address := range append(append([]string{}, c.To...), c.CC...) {
			if _, err := mail.ParseAddress(address); err != nil {
				return errors.New("email recipient address is invalid")
			}
		}
		if c.Encryption != "none" && c.Encryption != "starttls" && c.Encryption != "tls" {
			return errors.New("email encryption must be none, starttls, or tls")
		}
	}
	return nil
}

func validateNotificationProxyURL(value string) error {
	proxyURL, err := url.Parse(strings.TrimSpace(value))
	if err != nil || proxyURL.Hostname() == "" || proxyURL.Opaque != "" {
		return errors.New("Telegram proxyUrl is invalid")
	}
	scheme := strings.ToLower(proxyURL.Scheme)
	if scheme != "http" && scheme != "https" && scheme != "socks5" && scheme != "socks5h" {
		return errors.New("Telegram proxyUrl must use http, https, socks5, or socks5h")
	}
	if proxyURL.RawQuery != "" || proxyURL.Fragment != "" || (proxyURL.Path != "" && proxyURL.Path != "/") {
		return errors.New("Telegram proxyUrl must not contain a path, query, or fragment")
	}
	if port := proxyURL.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("Telegram proxyUrl port is invalid")
		}
	}
	if (scheme == "socks5" || scheme == "socks5h") && proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		if len(proxyURL.User.Username()) > 255 || len(password) > 255 {
			return errors.New("Telegram SOCKS5 proxy credentials are too long")
		}
	}
	return nil
}

func validateHTTPNotificationURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return errors.New("notification URL must be a valid HTTP or HTTPS URL")
	}
	if parsed.User != nil {
		return errors.New("notification URL must not contain embedded credentials")
	}
	return nil
}

func validateNotificationTemplate(label, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s不能为空", label)
	}
	if len(value) > 20000 {
		return fmt.Errorf("%s不能超过 20000 个字符", label)
	}
	_, err := template.New("notification").Funcs(notificationTemplateFunctions()).Option("missingkey=error").Parse(value)
	if err != nil {
		return fmt.Errorf("%s语法无效：%v", label, err)
	}
	return nil
}

func parseNotificationAgents(value interface{}) (bool, []string, error) {
	if text, ok := value.(string); ok && text == "all" {
		return true, nil, nil
	}
	raw, ok := value.([]interface{})
	if !ok {
		return false, nil, errors.New("agents must be all or an array of agent IDs")
	}
	ids, seen := make([]string, 0, len(raw)), map[string]bool{}
	for _, item := range raw {
		id, ok := item.(string)
		id = strings.TrimSpace(id)
		if !ok || id == "" {
			return false, nil, errors.New("agents contains an invalid ID")
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	if len(ids) == 0 {
		return false, nil, errors.New("at least one agent is required for custom scope")
	}
	return false, ids, nil
}

func (a *App) decryptNotificationEnvelope(channel NotificationChannel) (notificationChannelEnvelope, error) {
	if channel.Target.Ciphertext == "" {
		return notificationChannelEnvelope{}, errors.New("notification configuration is empty")
	}
	raw, err := a.box.Decrypt(channel.Target)
	if err != nil {
		return notificationChannelEnvelope{}, err
	}
	var envelope notificationChannelEnvelope
	if json.Unmarshal(raw, &envelope) == nil && envelope.Version >= 2 {
		if envelope.Template == "" {
			envelope.Template = defaultNotificationTemplate
		}
		if envelope.SubjectTemplate == "" {
			envelope.SubjectTemplate = defaultNotificationSubjectTemplate
		}
		return envelope, nil
	}
	// Compatibility with channels created before per-channel configuration was introduced.
	config := defaultNotificationConfig(channel.Type)
	legacyTarget := strings.TrimSpace(string(raw))
	switch channel.Type {
	case "telegram":
		separator := strings.LastIndex(legacyTarget, ":")
		if separator > 0 {
			config.BotToken, config.ChatID = legacyTarget[:separator], legacyTarget[separator+1:]
		}
	case "email":
		config.To = cleanStringList(strings.Split(legacyTarget, ","))
	default:
		config.URL = legacyTarget
	}
	return notificationChannelEnvelope{Version: 1, Config: config, Template: defaultNotificationTemplateFor(channel.Type), SubjectTemplate: defaultNotificationSubjectTemplate}, nil
}

func (a *App) decryptTarget(channel NotificationChannel) string {
	envelope, err := a.decryptNotificationEnvelope(channel)
	if err != nil {
		return ""
	}
	switch channel.Type {
	case "telegram":
		return envelope.Config.BotToken + ":" + envelope.Config.ChatID
	case "email":
		return strings.Join(envelope.Config.To, ",")
	default:
		return envelope.Config.URL
	}
}

func publicNotificationConfig(c NotificationConfig) NotificationConfig {
	out := c
	out.URLConfigured, out.URL = c.URL != "", ""
	out.SecretConfigured, out.Secret = c.Secret != "", ""
	out.BotTokenConfigured, out.BotToken = c.BotToken != "", ""
	out.ProxyURLConfigured, out.ProxyURL = c.ProxyURL != "", ""
	out.ChatIDConfigured, out.ChatID = c.ChatID != "", ""
	out.PasswordConfigured, out.Password = c.Password != "", ""
	out.RecipientsConfigured, out.RecipientCount, out.To = len(c.To) > 0, len(c.To), nil
	out.CCConfigured, out.CCCount, out.CC = len(c.CC) > 0, len(c.CC), nil
	out.AtMobilesConfigured, out.AtMobileCount, out.AtMobiles = len(c.AtMobiles) > 0, len(c.AtMobiles), nil
	out.AtUserIDsConfigured, out.AtUserIDCount, out.AtUserIDs = len(c.AtUserIDs) > 0, len(c.AtUserIDs), nil
	out.Headers = append([]NotificationHeader(nil), c.Headers...)
	for i := range out.Headers {
		out.Headers[i].ValueConfigured = out.Headers[i].Value != ""
		out.Headers[i].Value = ""
	}
	return out
}

func (a *App) publicNotificationChannel(channel NotificationChannel) notificationChannelResponse {
	envelope, _ := a.decryptNotificationEnvelope(channel)
	var agents interface{} = channel.AgentIDs
	if channel.AllAgents {
		agents = "all"
	}
	return notificationChannelResponse{ID: channel.ID, Name: channel.Name, Type: channel.Type, Config: publicNotificationConfig(envelope.Config), Template: envelope.Template, SubjectTemplate: envelope.SubjectTemplate, Enabled: channel.Enabled, Agents: agents, CreatedAt: channel.CreatedAt, UpdatedAt: channel.UpdatedAt}
}

func (a *App) deleteNotificationChannel(w http.ResponseWriter, r *http.Request, c claims) {
	id := r.PathValue("id")
	if err := a.store.DeleteNotificationChannel(c.TenantID, id); err != nil {
		writeError(w, http.StatusNotFound, "通知渠道不存在")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "notification.delete", "notification_channel", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) testNotificationChannel(w http.ResponseWriter, r *http.Request, c claims) {
	channel, err := a.store.GetNotificationChannel(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "通知渠道不存在")
		return
	}
	envelope, err := a.decryptNotificationEnvelope(channel)
	if err == nil {
		data := notificationTemplateData{Event: "channel.test", Title: "通知渠道测试", Message: "这是一条来自 WireMesh 的测试通知。", NodeName: "系统", NodeStatus: "test", OccurredAt: time.Now().UTC().Format(time.RFC3339)}
		var body, subject string
		body, err = renderNotificationTemplate(envelope.Template, data)
		if err == nil {
			subject, err = renderNotificationTemplate(envelope.SubjectTemplate, data)
		}
		if err == nil {
			err = sendNotification(r.Context(), channel.Type, envelope.Config, subject, body)
		}
	}
	status, message := "test", "测试通知已发送"
	if err != nil {
		status, message = "failed", sanitizeNotificationError(err)
	}
	logEntry := NotificationLog{ID: newID("notifylog"), TenantID: c.TenantID, ChannelID: channel.ID, ChannelName: channel.Name, ChannelType: channel.Type, AgentName: "system", Message: message, Status: status, CreatedAt: time.Now().UTC()}
	_ = a.store.AddNotificationLog(logEntry)
	a.auditEvent(c.TenantID, c.Subject, "notification.test", "notification_channel", channel.ID, map[string]string{"status": status})
	if err != nil {
		writeError(w, http.StatusBadGateway, message)
		return
	}
	writeJSON(w, http.StatusOK, logEntry)
}

func renderNotificationTemplate(source string, data notificationTemplateData) (string, error) {
	tpl, err := template.New("notification").Funcs(notificationTemplateFunctions()).Option("missingkey=error").Parse(source)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

func sendNotification(ctx context.Context, kind string, c NotificationConfig, subject, message string) error {
	switch kind {
	case "webhook":
		return sendWebhook(ctx, c, message)
	case "dingtalk":
		return sendDingTalk(ctx, c, subject, message)
	case "wecom":
		return sendWeCom(ctx, c, message)
	case "feishu":
		return sendFeishu(ctx, c, subject, message)
	case "telegram":
		return sendTelegram(ctx, c, message)
	case "email":
		return sendEmail(ctx, c, subject, message)
	default:
		return errors.New("unsupported notification channel type")
	}
}

func sendWebhook(ctx context.Context, c NotificationConfig, message string) error {
	if strings.Contains(strings.ToLower(c.ContentType), "application/json") && !json.Valid([]byte(message)) {
		return errors.New("rendered webhook template is not valid JSON")
	}
	req, err := http.NewRequestWithContext(ctx, c.Method, c.URL, strings.NewReader(message))
	if err != nil {
		return errors.New("failed to create webhook request")
	}
	req.Header.Set("Content-Type", c.ContentType)
	for _, h := range c.Headers {
		req.Header.Set(h.Name, h.Value)
	}
	if c.SignatureType == "bearer" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	if c.SignatureType == "hmac-sha256" {
		mac := hmac.New(sha256.New, []byte(c.Secret))
		_, _ = mac.Write([]byte(message))
		req.Header.Set("X-WireMesh-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	return doNotificationRequest(req, c.TimeoutSec, c.AllowPrivate, "")
}

func sendDingTalk(ctx context.Context, c NotificationConfig, subject, message string) error {
	target := c.URL
	if c.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		mac := hmac.New(sha256.New, []byte(c.Secret))
		_, _ = mac.Write([]byte(timestamp + "\n" + c.Secret))
		parsed, _ := url.Parse(target)
		query := parsed.Query()
		query.Set("timestamp", timestamp)
		query.Set("sign", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		parsed.RawQuery = query.Encode()
		target = parsed.String()
	}
	var payload any
	if c.MessageType == "markdown" {
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"title": subject, "text": message}, "at": map[string]any{"atMobiles": c.AtMobiles, "isAtAll": c.AtAll}}
	} else {
		payload = map[string]any{"msgtype": "text", "text": map[string]string{"content": message}, "at": map[string]any{"atMobiles": c.AtMobiles, "isAtAll": c.AtAll}}
	}
	return postJSONNotification(ctx, target, payload, c.TimeoutSec, c.AllowPrivate)
}

func sendWeCom(ctx context.Context, c NotificationConfig, message string) error {
	var payload any
	if c.MessageType == "markdown" {
		if c.AtAll {
			message += "\n<@all>"
		}
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": message}}
	} else {
		users := append([]string(nil), c.AtUserIDs...)
		if c.AtAll {
			users = []string{"@all"}
		}
		payload = map[string]any{"msgtype": "text", "text": map[string]any{"content": message, "mentioned_list": users, "mentioned_mobile_list": c.AtMobiles}}
	}
	return postJSONNotification(ctx, c.URL, payload, c.TimeoutSec, c.AllowPrivate)
}

func sendFeishu(ctx context.Context, c NotificationConfig, subject, message string) error {
	payload := map[string]any{}
	if c.MessageType == "post" {
		row := []map[string]string{{"tag": "text", "text": message}}
		if c.AtAll {
			row = append(row, map[string]string{"tag": "at", "user_id": "all", "user_name": "所有人"})
		}
		payload["msg_type"] = "post"
		payload["content"] = map[string]any{"post": map[string]any{"zh_cn": map[string]any{"title": subject, "content": [][]map[string]string{row}}}}
	} else {
		if c.AtAll {
			message += "\n<at user_id=\"all\">所有人</at>"
		}
		payload["msg_type"] = "text"
		payload["content"] = map[string]string{"text": message}
	}
	if c.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		stringToSign := timestamp + "\n" + c.Secret
		mac := hmac.New(sha256.New, []byte(stringToSign))
		_, _ = mac.Write(nil)
		payload["timestamp"] = timestamp
		payload["sign"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	}
	return postJSONNotification(ctx, c.URL, payload, c.TimeoutSec, c.AllowPrivate)
}

func sendTelegram(ctx context.Context, c NotificationConfig, message string) error {
	payload := map[string]any{"chat_id": c.ChatID, "text": message, "disable_web_page_preview": c.DisableWebPagePreview, "disable_notification": c.DisableNotification}
	if c.ParseMode != "" {
		payload["parse_mode"] = c.ParseMode
	}
	if c.ThreadID != "" {
		id, _ := strconv.ParseInt(c.ThreadID, 10, 64)
		payload["message_thread_id"] = id
	}
	target := "https://api.telegram.org/bot" + url.PathEscape(c.BotToken) + "/sendMessage"
	proxyURL := ""
	if c.UseProxy {
		proxyURL = c.ProxyURL
	}
	return postJSONNotificationWithProxy(ctx, target, payload, c.TimeoutSec, c.UseProxy, proxyURL)
}

func postJSONNotification(ctx context.Context, target string, payload any, timeout int, allowPrivate bool) error {
	return postJSONNotificationWithProxy(ctx, target, payload, timeout, allowPrivate, "")
}

func postJSONNotificationWithProxy(ctx context.Context, target string, payload any, timeout int, allowPrivate bool, proxyValue string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.New("failed to encode notification payload")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return errors.New("failed to create notification request")
	}
	req.Header.Set("Content-Type", "application/json")
	return doNotificationRequest(req, timeout, allowPrivate, proxyValue)
}

var errNotificationPrivateAddress = errors.New("notification target resolves to a private or local address")

func doNotificationRequest(req *http.Request, timeout int, allowPrivate bool, proxyValue string) error {
	client, err := notificationHTTPClient(timeout, proxyValue, allowPrivate)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		if errors.Is(err, errNotificationPrivateAddress) {
			return errNotificationPrivateAddress
		}
		return errors.New("notification request failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("notification endpoint returned HTTP %d", response.StatusCode)
	}
	return nil
}

func notificationHTTPClient(timeout int, proxyValue string, allowPrivate bool) (*http.Client, error) {
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	switch {
	case proxyValue != "":
		if err := validateNotificationProxyURL(proxyValue); err != nil {
			return nil, err
		}
		proxyURL, _ := url.Parse(proxyValue)
		switch strings.ToLower(proxyURL.Scheme) {
		case "http", "https":
			transport.Proxy = http.ProxyURL(proxyURL)
		case "socks5", "socks5h":
			transport.Proxy = nil
			dialTimeout := time.Duration(timeout) * time.Second
			transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
				return dialSOCKS5Proxy(ctx, proxyURL, address, dialTimeout)
			}
		}
	case !allowPrivate:
		dialer := &net.Dialer{Timeout: time.Duration(timeout) * time.Second, KeepAlive: 30 * time.Second}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialNotificationAddress(ctx, dialer, network, address)
		}
	}
	client.Transport = transport
	return client, nil
}

// dialNotificationAddress resolves the target host once, rejects private and
// local addresses, and then dials the resolved IP directly. Reusing the same
// resolved addresses for both the policy check and the connection closes the
// DNS-rebinding window where a resolver returns a public address during the
// check and a private address on the actual dial.
func dialNotificationAddress(ctx context.Context, dialer *net.Dialer, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errNotificationPrivateAddress
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, errNotificationPrivateAddress
	}
	var lastErr error
	for _, ip := range ips {
		if isUnsafeNotificationIP(ip.IP) {
			return nil, errNotificationPrivateAddress
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errNotificationPrivateAddress
}

func dialSOCKS5Proxy(ctx context.Context, proxyURL *url.URL, targetAddress string, timeout time.Duration) (net.Conn, error) {
	proxyAddress := proxyURL.Host
	if proxyURL.Port() == "" {
		proxyAddress = net.JoinHostPort(proxyURL.Hostname(), "1080")
	}
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", proxyAddress)
	if err != nil {
		return nil, errors.New("Telegram proxy connection failed")
	}
	fail := func(message string) (net.Conn, error) {
		_ = conn.Close()
		return nil, errors.New(message)
	}
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)
	methods := []byte{0x00}
	if proxyURL.User != nil {
		methods = append(methods, 0x02)
	}
	greeting := append([]byte{0x05, byte(len(methods))}, methods...)
	if _, err := conn.Write(greeting); err != nil {
		return fail("Telegram proxy negotiation failed")
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil || reply[0] != 0x05 || reply[1] == 0xff {
		return fail("Telegram proxy rejected authentication methods")
	}
	if reply[1] == 0x02 {
		username := proxyURL.User.Username()
		password, _ := proxyURL.User.Password()
		auth := []byte{0x01, byte(len(username))}
		auth = append(auth, username...)
		auth = append(auth, byte(len(password)))
		auth = append(auth, password...)
		if _, err := conn.Write(auth); err != nil {
			return fail("Telegram proxy authentication failed")
		}
		if _, err := io.ReadFull(conn, reply); err != nil || reply[0] != 0x01 || reply[1] != 0x00 {
			return fail("Telegram proxy authentication failed")
		}
	} else if reply[1] != 0x00 {
		return fail("Telegram proxy selected an unsupported authentication method")
	}
	host, portText, err := net.SplitHostPort(targetAddress)
	if err != nil {
		return fail("Telegram proxy target address is invalid")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fail("Telegram proxy target port is invalid")
	}
	request := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			request = append(request, 0x01)
			request = append(request, ipv4...)
		} else {
			request = append(request, 0x04)
			request = append(request, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return fail("Telegram proxy target host is too long")
		}
		request = append(request, 0x03, byte(len(host)))
		request = append(request, host...)
	}
	request = append(request, byte(port>>8), byte(port))
	if _, err := conn.Write(request); err != nil {
		return fail("Telegram proxy connect request failed")
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil || header[0] != 0x05 {
		return fail("Telegram proxy returned an invalid response")
	}
	if header[1] != 0x00 {
		return fail(fmt.Sprintf("Telegram proxy connect failed with code %d", header[1]))
	}
	addressLength := 0
	switch header[3] {
	case 0x01:
		addressLength = 4
	case 0x03:
		length := []byte{0}
		if _, err := io.ReadFull(conn, length); err != nil {
			return fail("Telegram proxy returned an invalid address")
		}
		addressLength = int(length[0])
	case 0x04:
		addressLength = 16
	default:
		return fail("Telegram proxy returned an unsupported address type")
	}
	if _, err := io.ReadFull(conn, make([]byte, addressLength+2)); err != nil {
		return fail("Telegram proxy returned an incomplete response")
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func sendEmail(ctx context.Context, c NotificationConfig, subject, body string) error {
	address := net.JoinHostPort(c.SMTPHost, strconv.Itoa(c.SMTPPort))
	dialTimeout := time.Duration(c.TimeoutSec) * time.Second
	tlsConfig := &tls.Config{ServerName: c.SMTPHost, MinVersion: tls.VersionTLS12, InsecureSkipVerify: c.SkipTLSVerify}
	var conn net.Conn
	var err error
	if c.Encryption == "tls" {
		if c.AllowPrivate {
			conn, err = tls.DialWithDialer(&net.Dialer{Timeout: dialTimeout}, "tcp", address, tlsConfig)
		} else {
			raw, dialErr := dialNotificationAddress(ctx, &net.Dialer{Timeout: dialTimeout}, "tcp", address)
			if dialErr != nil {
				err = dialErr
			} else {
				tlsConn := tls.Client(raw, tlsConfig)
				if err = tlsConn.HandshakeContext(ctx); err != nil {
					_ = raw.Close()
				} else {
					conn = tlsConn
				}
			}
		}
	} else if c.AllowPrivate {
		conn, err = (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialNotificationAddress(ctx, &net.Dialer{Timeout: dialTimeout}, "tcp", address)
	}
	if err != nil {
		if errors.Is(err, errNotificationPrivateAddress) {
			return errNotificationPrivateAddress
		}
		return errors.New("SMTP connection failed")
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, c.SMTPHost)
	if err != nil {
		return errors.New("SMTP handshake failed")
	}
	defer client.Close()
	if c.Encryption == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return errors.New("SMTP STARTTLS failed")
		}
	}
	if c.Username != "" {
		if ok, _ := client.Extension("AUTH"); !ok {
			return errors.New("SMTP server does not support authentication")
		}
		if err := client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.SMTPHost)); err != nil {
			return errors.New("SMTP authentication failed")
		}
	}
	from, _ := mail.ParseAddress(c.FromAddress)
	if err := client.Mail(from.Address); err != nil {
		return errors.New("SMTP sender was rejected")
	}
	recipients := append(append([]string{}, c.To...), c.CC...)
	for _, recipient := range recipients {
		parsed, _ := mail.ParseAddress(recipient)
		if err := client.Rcpt(parsed.Address); err != nil {
			return errors.New("SMTP recipient was rejected")
		}
	}
	writer, err := client.Data()
	if err != nil {
		return errors.New("SMTP message data was rejected")
	}
	fromHeader := c.FromAddress
	if c.FromName != "" {
		fromHeader = (&mail.Address{Name: c.FromName, Address: from.Address}).String()
	}
	message := "From: " + fromHeader + "\r\nTo: " + strings.Join(c.To, ", ") + "\r\n"
	if len(c.CC) > 0 {
		message += "Cc: " + strings.Join(c.CC, ", ") + "\r\n"
	}
	message += "Subject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" + body
	if _, err := io.WriteString(writer, message); err != nil {
		_ = writer.Close()
		return errors.New("SMTP message write failed")
	}
	if err := writer.Close(); err != nil {
		return errors.New("SMTP message delivery failed")
	}
	if err := client.Quit(); err != nil {
		return errors.New("SMTP session close failed")
	}
	return nil
}

func isUnsafeNotificationIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func sanitizeNotificationError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > 240 {
		text = text[:240]
	}
	return text
}
