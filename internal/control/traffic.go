package control

import (
	"net/http"
	"strings"
	"time"
)

func (a *App) nodeTraffic(w http.ResponseWriter, r *http.Request, c claims) {
	node, err := a.store.GetNode(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	iface := strings.TrimSpace(r.URL.Query().Get("interface"))
	if iface == "" {
		writeError(w, http.StatusBadRequest, "interface is required")
		return
	}
	durations := map[string]time.Duration{"24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}
	rangeName := r.URL.Query().Get("range")
	if rangeName == "" {
		rangeName = "24h"
	}
	duration, ok := durations[rangeName]
	if !ok {
		writeError(w, http.StatusBadRequest, "range must be 24h, 7d or 30d")
		return
	}
	samples := a.store.ListTrafficSamples(c.TenantID, node.ID, iface, time.Now().Add(-duration))
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
