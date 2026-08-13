package control

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

func (a *App) networkEgress(w http.ResponseWriter, r *http.Request, c claims) {
	network, err := a.store.GetNetwork(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	if r.Method == http.MethodGet {
		config, err := a.store.GetEgressConfig(c.TenantID, network.ID)
		if err != nil {
			writeJSON(w, http.StatusOK, EgressConfig{NetworkID: network.ID, CIDRs: []string{}})
			return
		}
		writeJSON(w, http.StatusOK, config)
		return
	}
	var in struct {
		EgressNodeID string   `json:"egress_node_id"`
		CIDRs        []string `json:"cidrs"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.EgressNodeID = strings.TrimSpace(in.EgressNodeID)
	if in.EgressNodeID != "" {
		node, err := a.store.GetNode(c.TenantID, in.EgressNodeID)
		if err != nil || node.NetworkID != network.ID {
			writeError(w, http.StatusBadRequest, "egress node must belong to this network")
			return
		}
	}
	cidrs := make([]string, 0, len(in.CIDRs))
	for _, raw := range in.CIDRs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid CIDR %q", raw))
			return
		}
		cidrs = append(cidrs, prefix.String())
	}
	config := EgressConfig{TenantID: c.TenantID, NetworkID: network.ID, EgressNodeID: in.EgressNodeID, CIDRs: cidrs, UpdatedAt: time.Now().UTC()}
	if err := a.store.UpsertEgressConfig(config); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save egress configuration")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "network.egress.update", "network", network.ID, nil)
	writeJSON(w, http.StatusOK, config)
}
