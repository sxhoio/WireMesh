package control

import (
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

var dnsNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

func validDNSName(value string) bool {
	return len(value) <= 253 && dnsNamePattern.MatchString(value)
}

// dnsRecordFromInput 校验并规范化输入记录（不包含 ID/租户/网络信息）。
func dnsRecordFromInput(in DNSRecord) (DNSRecord, error) {
	in.Name = strings.ToLower(strings.TrimSpace(in.Name))
	if !validDNSName(in.Name) {
		return DNSRecord{}, fmt.Errorf("name must be a valid DNS hostname")
	}
	if _, err := netip.ParseAddr(strings.TrimSpace(in.Address)); err != nil {
		return DNSRecord{}, fmt.Errorf("address must be a valid IP address")
	}
	return DNSRecord{
		Name:        in.Name,
		Address:     strings.TrimSpace(in.Address),
		Description: strings.TrimSpace(in.Description),
	}, nil
}

func (a *App) dnsRecords(w http.ResponseWriter, r *http.Request, c claims) {
	network, err := a.store.GetNetwork(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	if r.Method == http.MethodGet {
		items, err := a.store.ListDNSRecords(c.TenantID, network.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list DNS records")
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	var in DNSRecord
	if !decode(w, r, &in) {
		return
	}
	record, err := dnsRecordFromInput(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	record.ID = newID("dns")
	record.TenantID = c.TenantID
	record.NetworkID = network.ID
	record.CreatedAt = time.Now().UTC()
	if err := a.store.CreateDNSRecord(record); err != nil {
		writeError(w, http.StatusConflict, "该网络中已存在同名记录")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "dns.record.create", "network", network.ID, nil)
	writeJSON(w, http.StatusCreated, record)
}

func (a *App) updateDNSRecord(w http.ResponseWriter, r *http.Request, c claims) {
	network, err := a.store.GetNetwork(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	items, err := a.store.ListDNSRecords(c.TenantID, network.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list DNS records")
		return
	}
	var current DNSRecord
	found := false
	for _, item := range items {
		if item.ID == r.PathValue("record_id") {
			current, found = item, true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "DNS record not found")
		return
	}
	var in DNSRecord
	if !decode(w, r, &in) {
		return
	}
	record, err := dnsRecordFromInput(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	record.ID = current.ID
	record.TenantID = c.TenantID
	record.NetworkID = network.ID
	record.CreatedAt = current.CreatedAt
	if err := a.store.UpdateDNSRecord(record); err != nil {
		writeError(w, http.StatusConflict, "该网络中已存在同名记录")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "dns.record.update", "network", network.ID, nil)
	writeJSON(w, http.StatusOK, record)
}

func (a *App) deleteDNSRecord(w http.ResponseWriter, r *http.Request, c claims) {
	if err := a.store.DeleteDNSRecord(c.TenantID, r.PathValue("record_id")); err != nil {
		writeError(w, http.StatusNotFound, "DNS record not found")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "dns.record.delete", "network", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}
