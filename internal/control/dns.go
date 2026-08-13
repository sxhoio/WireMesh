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
	in.Name = strings.ToLower(strings.TrimSpace(in.Name))
	if !validDNSName(in.Name) {
		writeError(w, http.StatusBadRequest, "name must be a valid DNS hostname")
		return
	}
	if _, err := netip.ParseAddr(strings.TrimSpace(in.Address)); err != nil {
		writeError(w, http.StatusBadRequest, "address must be a valid IP address")
		return
	}
	record := DNSRecord{
		ID: newID("dns"), TenantID: c.TenantID, NetworkID: network.ID,
		Name: in.Name, Address: strings.TrimSpace(in.Address),
		Description: strings.TrimSpace(in.Description), CreatedAt: time.Now().UTC(),
	}
	if err := a.store.CreateDNSRecord(record); err != nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("failed to create DNS record: %v", err))
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "dns.record.create", "network", network.ID, nil)
	writeJSON(w, http.StatusCreated, record)
}

func (a *App) deleteDNSRecord(w http.ResponseWriter, r *http.Request, c claims) {
	if err := a.store.DeleteDNSRecord(c.TenantID, r.PathValue("record_id")); err != nil {
		writeError(w, http.StatusNotFound, "DNS record not found")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "dns.record.delete", "network", r.PathValue("id"), nil)
	w.WriteHeader(http.StatusNoContent)
}
