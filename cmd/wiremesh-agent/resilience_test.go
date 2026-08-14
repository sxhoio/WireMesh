package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestResponseErrorClassifiesTerminalStates：401/403 映射为身份被拒、
// 423 映射为节点停用（均属终止错误，Agent 应停止请求风暴）。
func TestResponseErrorClassifiesTerminalStates(t *testing.T) {
	cases := []struct {
		status   int
		want     error
		terminal bool
		body     string
	}{
		{status: http.StatusUnauthorized, want: errAgentIdentityRejected, terminal: true, body: `{"error":"身份验证或权限不足"}`},
		{status: http.StatusForbidden, want: errAgentIdentityRejected, terminal: true, body: `agent endpoints require TLS`},
		{status: http.StatusLocked, want: errAgentDisabled, terminal: true, body: `node is disabled`},
		{status: http.StatusNotFound, terminal: false, body: `no pending configuration`},
		{status: http.StatusBadGateway, terminal: false, body: `upstream error`},
		{status: http.StatusInternalServerError, terminal: false, body: `oops`},
	}
	for _, test := range cases {
		response := httptest.NewRecorder()
		response.WriteHeader(test.status)
		_, _ = response.WriteString(test.body)
		err := responseError(response.Result())
		if test.terminal != isTerminalAgentError(err) {
			t.Fatalf("status %d: terminal=%v, err=%v", test.status, test.terminal, err)
		}
		if test.terminal && !errors.Is(err, test.want) {
			t.Fatalf("status %d: expected %v, got %v", test.status, test.want, err)
		}
		if !test.terminal && (errors.Is(err, errAgentIdentityRejected) || errors.Is(err, errAgentDisabled)) {
			t.Fatalf("status %d: transient error must not be classified as terminal: %v", test.status, err)
		}
	}
}

// TestResponseErrorIncludesServerMessage：非终止错误保留服务端响应文本，
// 便于诊断（不会吞掉错误细节）。
func TestResponseErrorIncludesServerMessage(t *testing.T) {
	response := httptest.NewRecorder()
	response.WriteHeader(http.StatusBadRequest)
	_, _ = response.WriteString(`{"error":"invalid delivery state"}`)
	err := responseError(response.Result())
	if err == nil {
		t.Fatal("400 must produce an error")
	}
	if errors.Is(err, errAgentIdentityRejected) || errors.Is(err, errAgentDisabled) {
		t.Fatalf("400 must not be treated as terminal rejection: %v", err)
	}
	if isTerminalAgentError(err) {
		t.Fatalf("400 must not be terminal: %v", err)
	}
}

// TestResponseErrorKeepsServerReason：401 的终止错误保留服务端具体拒绝
// 原因（如证书缺失/未知节点），errors.Is 仍可匹配终止判断。
func TestResponseErrorKeepsServerReason(t *testing.T) {
	response := httptest.NewRecorder()
	response.WriteHeader(http.StatusUnauthorized)
	_, _ = response.WriteString(`{"error":"agent client certificate required"}`)
	err := responseError(response.Result())
	if !errors.Is(err, errAgentIdentityRejected) {
		t.Fatalf("401 must be identity rejection, got %v", err)
	}
	if !isTerminalAgentError(err) {
		t.Fatal("401 must remain terminal")
	}
	if !strings.Contains(err.Error(), "agent client certificate required") {
		t.Fatalf("401 must keep the server-side reason for diagnosis: %v", err)
	}
}
