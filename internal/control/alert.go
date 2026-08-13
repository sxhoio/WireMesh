package control

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const alertEvaluateInterval = 30 * time.Second

const (
	alertTypeNodeOffline  = "node_offline"
	alertTypeLinkDown     = "link_down"
	alertTypeConfigFailed = "config_failed"
)

func validAlertRuleType(value string) bool {
	return value == alertTypeNodeOffline || value == alertTypeLinkDown || value == alertTypeConfigFailed
}

func (a *App) alertRuleFromInput(tenantID string, current AlertRule, in AlertRule) (AlertRule, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 80 {
		return AlertRule{}, fmt.Errorf("name is required and must not exceed 80 characters")
	}
	if !validAlertRuleType(in.Type) {
		return AlertRule{}, fmt.Errorf("alert rule type is invalid")
	}
	if in.ThresholdSec < 1 || in.ThresholdSec > 86400 {
		return AlertRule{}, fmt.Errorf("threshold_sec must be between 1 and 86400")
	}
	if in.QuietSec < 0 || in.QuietSec > 604800 {
		return AlertRule{}, fmt.Errorf("quiet_sec must be between 0 and 604800")
	}
	channelIDs := make([]string, 0, len(in.ChannelIDs))
	for _, id := range in.ChannelIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, err := a.store.GetNotificationChannel(tenantID, id); err != nil {
			return AlertRule{}, fmt.Errorf("notification channel %s does not exist", id)
		}
		channelIDs = append(channelIDs, id)
	}
	return AlertRule{
		ID: current.ID, TenantID: tenantID, Name: in.Name, Type: in.Type,
		ThresholdSec: in.ThresholdSec, ChannelIDs: channelIDs,
		Enabled: in.Enabled, QuietSec: in.QuietSec,
		CreatedAt: current.CreatedAt, UpdatedAt: current.UpdatedAt,
	}, nil
}

func (a *App) alertRules(w http.ResponseWriter, r *http.Request, c claims) {
	if r.Method == http.MethodGet {
		items, err := a.store.ListAlertRules(c.TenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list alert rules")
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	var in AlertRule
	if !decode(w, r, &in) {
		return
	}
	rule, err := a.alertRuleFromInput(c.TenantID, AlertRule{}, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC()
	rule.ID, rule.CreatedAt, rule.UpdatedAt = newID("alert"), now, now
	if err := a.store.CreateAlertRule(rule); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create alert rule")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "alert_rule.create", "alert_rule", rule.ID, nil)
	writeJSON(w, http.StatusCreated, rule)
}

func (a *App) updateAlertRule(w http.ResponseWriter, r *http.Request, c claims) {
	id := r.PathValue("id")
	items, err := a.store.ListAlertRules(c.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list alert rules")
		return
	}
	var current AlertRule
	found := false
	for _, item := range items {
		if item.ID == id {
			current, found = item, true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "alert rule not found")
		return
	}
	var in AlertRule
	if !decode(w, r, &in) {
		return
	}
	rule, err := a.alertRuleFromInput(c.TenantID, current, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rule.ID, rule.CreatedAt, rule.UpdatedAt = current.ID, current.CreatedAt, time.Now().UTC()
	if err := a.store.UpdateAlertRule(rule); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update alert rule")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "alert_rule.update", "alert_rule", rule.ID, nil)
	writeJSON(w, http.StatusOK, rule)
}

func (a *App) deleteAlertRule(w http.ResponseWriter, r *http.Request, c claims) {
	if err := a.store.DeleteAlertRule(c.TenantID, r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, "alert rule not found")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "alert_rule.delete", "alert_rule", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) alertEvents(w http.ResponseWriter, r *http.Request, c claims) {
	items, err := a.store.ListAlertEvents(c.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list alert events")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// StartAlertEvaluator 启动后台评估循环：每 30 秒检查一次所有启用的告警规则。
func (a *App) StartAlertEvaluator(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(alertEvaluateInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.evaluateAlertRulesOnce()
			}
		}
	}()
}

func (a *App) evaluateAlertRulesOnce() {
	rules, err := a.store.AllAlertRules()
	if err != nil {
		return
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		a.evaluateAlertRule(rule)
	}
}

func (a *App) evaluateAlertRule(rule AlertRule) {
	nodes, err := a.store.ListNodes(rule.TenantID, "")
	if err != nil {
		return
	}
	for _, node := range nodes {
		if !node.Enabled {
			continue
		}
		if triggered, message := a.evaluateAlertForNode(rule, node); triggered {
			a.fireAlert(rule, node, message)
		}
	}
}

func (a *App) evaluateAlertForNode(rule AlertRule, node Node) (bool, string) {
	threshold := time.Duration(rule.ThresholdSec) * time.Second
	switch rule.Type {
	case alertTypeNodeOffline:
		if !node.LastSeen.IsZero() && time.Since(node.LastSeen) > threshold {
			return true, fmt.Sprintf("节点 %s 超过 %d 秒未上报，判定为离线", node.Name, rule.ThresholdSec)
		}
	case alertTypeConfigFailed:
		deliveries, err := a.store.ListDeliveries(rule.TenantID, node.ID)
		if err != nil {
			return false, ""
		}
		for _, delivery := range deliveries {
			if delivery.State == "failed" && time.Since(delivery.UpdatedAt) <= threshold {
				message := delivery.Message
				if strings.TrimSpace(message) == "" {
					message = "未知错误"
				}
				return true, fmt.Sprintf("节点 %s 应用配置版本 %d 失败：%s", node.Name, delivery.Version, message)
			}
		}
	case alertTypeLinkDown:
		for _, iface := range node.WireGuard {
			for _, peer := range iface.Peers {
				if !peer.LatestHandshakeAt.IsZero() && time.Since(peer.LatestHandshakeAt) > threshold {
					return true, fmt.Sprintf("节点 %s 接口 %s 的 Peer 握手超过 %d 秒未更新", node.Name, iface.Name, rule.ThresholdSec)
				}
			}
		}
	}
	return false, ""
}

// fireAlert 在静默期内对同一规则+节点只触发一次，向绑定的通知渠道发送告警并记录事件。
func (a *App) fireAlert(rule AlertRule, node Node, message string) {
	key := rule.ID + ":" + node.ID
	a.alertMu.Lock()
	if last, ok := a.alertFiredAt[key]; ok && time.Since(last) < time.Duration(rule.QuietSec)*time.Second {
		a.alertMu.Unlock()
		return
	}
	a.alertFiredAt[key] = time.Now()
	a.alertMu.Unlock()

	status := "sent"
	failures := 0
	for _, channelID := range rule.ChannelIDs {
		if !a.sendAlertToChannel(rule.TenantID, channelID, node.Name, message) {
			failures++
		}
	}
	if failures > 0 {
		status = "failed"
	}
	_ = a.store.AddAlertEvent(AlertEvent{
		ID: newID("alertev"), TenantID: rule.TenantID, RuleID: rule.ID, RuleName: rule.Name,
		NodeID: node.ID, NodeName: node.Name, Message: message, Status: status, CreatedAt: time.Now(),
	})
}

func (a *App) sendAlertToChannel(tenantID, channelID, nodeName, message string) bool {
	channel, err := a.store.GetNotificationChannel(tenantID, channelID)
	if err != nil {
		return false
	}
	envelope, err := a.decryptNotificationEnvelope(channel)
	if err != nil {
		return false
	}
	data := notificationTemplateData{
		Event: "alert.rule", Title: "WireMesh 告警", Message: message,
		NodeName: nodeName, NodeStatus: "alert", OccurredAt: time.Now().UTC().Format(time.RFC3339),
	}
	body, err := renderNotificationTemplate(envelope.Template, data)
	if err != nil {
		return false
	}
	subject, err := renderNotificationTemplate(envelope.SubjectTemplate, data)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := sendNotification(ctx, channel.Type, envelope.Config, subject, body); err != nil {
		_ = a.store.AddNotificationLog(NotificationLog{ID: newID("notifylog"), TenantID: tenantID, ChannelID: channel.ID, ChannelName: channel.Name, ChannelType: channel.Type, AgentName: nodeName, Message: message, Status: "failed", CreatedAt: time.Now()})
		return false
	}
	_ = a.store.AddNotificationLog(NotificationLog{ID: newID("notifylog"), TenantID: tenantID, ChannelID: channel.ID, ChannelName: channel.Name, ChannelType: channel.Type, AgentName: nodeName, Message: message, Status: "sent", CreatedAt: time.Now()})
	return true
}
