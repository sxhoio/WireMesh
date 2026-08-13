package control

import (
	"net/http"
	"strings"
	"time"
)

func (a *App) nodeTraffic(w http.ResponseWriter, r *http.Request, c claims) {
	node, err := a.store.GetNode(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "节点不存在")
		return
	}
	iface := strings.TrimSpace(r.URL.Query().Get("interface"))
	if iface == "" {
		writeError(w, http.StatusBadRequest, "请指定接口")
		return
	}
	durations := map[string]time.Duration{
		"5m":  5 * time.Minute,
		"10m": 10 * time.Minute,
		"30m": 30 * time.Minute,
		"1h":  time.Hour,
		"2h":  2 * time.Hour,
		"6h":  6 * time.Hour,
		"12h": 12 * time.Hour,
		"24h": 24 * time.Hour,
		"7d":  7 * 24 * time.Hour,
		"30d": 30 * 24 * time.Hour,
	}
	rangeName := r.URL.Query().Get("range")
	if rangeName == "" {
		rangeName = "24h"
	}
	duration, ok := durations[rangeName]
	if !ok {
		writeError(w, http.StatusBadRequest, "range 必须是 5m、10m、30m、1h、2h、6h、12h、24h、7d 或 30d 之一")
		return
	}
	samples, err := a.store.ListTrafficSamples(c.TenantID, node.ID, iface, time.Now().Add(-duration))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取流量采样数据失败")
		return
	}
	points := make([]TrafficPoint, len(samples))
	for i, sample := range samples {
		points[i] = TrafficPoint{RecordedAt: sample.RecordedAt, ReceiveBytes: sample.ReceiveBytes, TransmitBytes: sample.TransmitBytes}
		if i > 0 {
			elapsed := sample.RecordedAt.Sub(samples[i-1].RecordedAt).Seconds()
			rx := sample.ReceiveBytes - samples[i-1].ReceiveBytes
			tx := sample.TransmitBytes - samples[i-1].TransmitBytes
			if elapsed > 0 && rx >= 0 {
				points[i].RXMbps = float64(rx) * 8 / elapsed / 1_000_000
			}
			if elapsed > 0 && tx >= 0 {
				points[i].TXMbps = float64(tx) * 8 / elapsed / 1_000_000
			}
		}
	}
	if len(points) > 240 {
		step := (len(points) + 239) / 240
		reduced := make([]TrafficPoint, 0, 241)
		for i := 0; i < len(points); i += step {
			reduced = append(reduced, points[i])
		}
		if reduced[len(reduced)-1].RecordedAt != points[len(points)-1].RecordedAt {
			reduced = append(reduced, points[len(points)-1])
		}
		points = reduced
	}
	writeJSON(w, http.StatusOK, points)
}
