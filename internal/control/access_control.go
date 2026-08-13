package control

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

func (a *App) accessResourceFromInput(tenantID string, network Network, in AccessResource) (AccessResource, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 80 {
		return AccessResource{}, fmt.Errorf("name is required and must not exceed 80 characters")
	}
	gateway, err := a.store.GetNode(tenantID, in.GatewayNodeID)
	if err != nil || gateway.NetworkID != network.ID {
		return AccessResource{}, fmt.Errorf("gateway node must belong to this network")
	}
	in.Target = strings.TrimSpace(in.Target)
	if in.Target == "" {
		in.Target = gateway.Address + "/32"
	}
	prefix, err := netip.ParsePrefix(in.Target)
	if err != nil || !prefix.Addr().Is4() {
		return AccessResource{}, fmt.Errorf("target must be a valid IPv4 CIDR")
	}
	if in.Port < 0 || in.Port > 65535 {
		return AccessResource{}, fmt.Errorf("port must be between 0 and 65535")
	}
	in.Protocol = strings.ToLower(strings.TrimSpace(in.Protocol))
	if in.Protocol != "" && in.Protocol != "tcp" && in.Protocol != "udp" && in.Protocol != "any" {
		return AccessResource{}, fmt.Errorf("protocol must be tcp, udp, any, or empty")
	}
	return AccessResource{
		TenantID: tenantID, NetworkID: network.ID, Name: in.Name,
		GatewayNodeID: gateway.ID, Target: prefix.String(), Port: in.Port,
		Protocol: in.Protocol, Description: strings.TrimSpace(in.Description),
	}, nil
}

func (a *App) accessPolicyFromInput(tenantID string, network Network, current AccessPolicy, in AccessPolicy) (AccessPolicy, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 80 {
		return AccessPolicy{}, fmt.Errorf("name is required and must not exceed 80 characters")
	}
	in.SourceLabel = strings.TrimSpace(in.SourceLabel)
	if in.SourceLabel != "" {
		if _, _, ok := strings.Cut(in.SourceLabel, "="); !ok {
			return AccessPolicy{}, fmt.Errorf("source_label must be key=value")
		}
	}
	sources := make([]string, 0, len(in.SourceNodeIDs))
	seen := map[string]bool{}
	for _, id := range in.SourceNodeIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		node, err := a.store.GetNode(tenantID, id)
		if err != nil || node.NetworkID != network.ID {
			return AccessPolicy{}, fmt.Errorf("source node %s does not belong to this network", id)
		}
		seen[id] = true
		sources = append(sources, id)
	}
	resources, err := a.store.ListAccessResources(tenantID, network.ID)
	if err != nil {
		return AccessPolicy{}, fmt.Errorf("read access resources: %w", err)
	}
	known := map[string]bool{}
	for _, resource := range resources {
		known[resource.ID] = true
	}
	resourceIDs := make([]string, 0, len(in.ResourceIDs))
	for _, id := range in.ResourceIDs {
		id = strings.TrimSpace(id)
		if id == "" || !known[id] {
			return AccessPolicy{}, fmt.Errorf("resource %s does not exist", id)
		}
		resourceIDs = append(resourceIDs, id)
	}
	return AccessPolicy{
		ID: current.ID, TenantID: tenantID, NetworkID: network.ID, Name: in.Name,
		SourceLabel: in.SourceLabel, SourceNodeIDs: sources, ResourceIDs: resourceIDs,
		Enabled: in.Enabled, CreatedAt: current.CreatedAt, UpdatedAt: current.UpdatedAt,
	}, nil
}

func (a *App) accessResources(w http.ResponseWriter, r *http.Request, c claims) {
	network, err := a.store.GetNetwork(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	if r.Method == http.MethodGet {
		items, err := a.store.ListAccessResources(c.TenantID, network.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list access resources")
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	var in AccessResource
	if !decode(w, r, &in) {
		return
	}
	resource, err := a.accessResourceFromInput(c.TenantID, network, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resource.ID, resource.CreatedAt = newID("acres"), time.Now().UTC()
	if err := a.store.CreateAccessResource(resource); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create access resource")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "access.resource.create", "network", network.ID, nil)
	writeJSON(w, http.StatusCreated, resource)
}

func (a *App) updateAccessResource(w http.ResponseWriter, r *http.Request, c claims) {
	network, err := a.store.GetNetwork(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	items, err := a.store.ListAccessResources(c.TenantID, network.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list access resources")
		return
	}
	var current AccessResource
	found := false
	for _, item := range items {
		if item.ID == r.PathValue("resource_id") {
			current, found = item, true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "access resource not found")
		return
	}
	var in AccessResource
	if !decode(w, r, &in) {
		return
	}
	resource, err := a.accessResourceFromInput(c.TenantID, network, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resource.ID, resource.NetworkID, resource.CreatedAt = current.ID, network.ID, current.CreatedAt
	if err := a.store.UpdateAccessResource(resource); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update access resource")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "access.resource.update", "network", network.ID, nil)
	writeJSON(w, http.StatusOK, resource)
}

func (a *App) deleteAccessResource(w http.ResponseWriter, r *http.Request, c claims) {
	// 被策略引用的资源不允许直接删除，避免策略静默失效。
	policies, err := a.store.ListAccessPolicies(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list access policies")
		return
	}
	referenced := make([]string, 0)
	for _, policy := range policies {
		for _, resourceID := range policy.ResourceIDs {
			if resourceID == r.PathValue("resource_id") {
				referenced = append(referenced, policy.Name)
				break
			}
		}
	}
	if len(referenced) > 0 {
		writeError(w, http.StatusConflict, "resource is referenced by policies: "+strings.Join(referenced, ", "))
		return
	}
	if err := a.store.DeleteAccessResource(c.TenantID, r.PathValue("resource_id")); err != nil {
		writeError(w, http.StatusNotFound, "access resource not found")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "access.resource.delete", "network", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) accessPolicies(w http.ResponseWriter, r *http.Request, c claims) {
	network, err := a.store.GetNetwork(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	if r.Method == http.MethodGet {
		items, err := a.store.ListAccessPolicies(c.TenantID, network.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list access policies")
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	var in AccessPolicy
	if !decode(w, r, &in) {
		return
	}
	policy, err := a.accessPolicyFromInput(c.TenantID, network, AccessPolicy{}, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC()
	policy.ID, policy.CreatedAt, policy.UpdatedAt = newID("acpol"), now, now
	if err := a.store.CreateAccessPolicy(policy); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create access policy")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "access.policy.create", "network", network.ID, nil)
	writeJSON(w, http.StatusCreated, policy)
}

func (a *App) updateAccessPolicy(w http.ResponseWriter, r *http.Request, c claims) {
	network, err := a.store.GetNetwork(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	items, err := a.store.ListAccessPolicies(c.TenantID, network.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list access policies")
		return
	}
	var current AccessPolicy
	found := false
	for _, item := range items {
		if item.ID == r.PathValue("policy_id") {
			current, found = item, true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "access policy not found")
		return
	}
	var in AccessPolicy
	if !decode(w, r, &in) {
		return
	}
	policy, err := a.accessPolicyFromInput(c.TenantID, network, current, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	policy.ID, policy.CreatedAt, policy.UpdatedAt = current.ID, current.CreatedAt, time.Now().UTC()
	if err := a.store.UpdateAccessPolicy(policy); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update access policy")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "access.policy.update", "network", network.ID, nil)
	writeJSON(w, http.StatusOK, policy)
}

func (a *App) deleteAccessPolicy(w http.ResponseWriter, r *http.Request, c claims) {
	if err := a.store.DeleteAccessPolicy(c.TenantID, r.PathValue("policy_id")); err != nil {
		writeError(w, http.StatusNotFound, "access policy not found")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "access.policy.delete", "network", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}
