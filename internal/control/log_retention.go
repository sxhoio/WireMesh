package control

import (
	"net/http"
	"strconv"
)

const (
	defaultLogPageSize = 50
	maxLogPageSize     = 100
	maxLogOffset       = 10000

	// Agent command records and audit records are operational history rather
	// than immutable configuration. Keep a bounded amount per scope so a busy
	// installation cannot grow the database without limit.
	maxAgentLogRecords = 5000
	maxAuditRecords    = 10000
)

type NodeLogPage struct {
	Items        []NodeLog `json:"items"`
	CurrentError string    `json:"current_error,omitempty"`
	Limit        int       `json:"limit"`
	Offset       int       `json:"offset"`
	HasMore      bool      `json:"has_more"`
}

type AuditLogPage struct {
	Items   []AuditEvent `json:"items"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
	HasMore bool         `json:"has_more"`
}

func parseLogPage(r *http.Request) (int, int) {
	limit := defaultLogPageSize
	offset := 0
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 {
		limit = value
	}
	if value, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && value >= 0 {
		offset = value
	}
	if limit > maxLogPageSize {
		limit = maxLogPageSize
	}
	if offset > maxLogOffset {
		offset = maxLogOffset
	}
	return limit, offset
}
