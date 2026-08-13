package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/wiremesh/wiremesh/internal/wireproto"
)

type agentClient struct {
	httpClient *http.Client
	state      agentState
}

func newAgentClient(httpClient *http.Client, state agentState) agentClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	state.Server = strings.TrimRight(strings.TrimSpace(state.Server), "/")
	return agentClient{httpClient: httpClient, state: state}
}

func (client agentClient) endpoint(path string) string {
	if strings.HasPrefix(strings.ToLower(path), "http://") || strings.HasPrefix(strings.ToLower(path), "https://") {
		return path
	}
	if strings.HasPrefix(path, "/") {
		return client.state.Server + path
	}
	return client.state.Server + "/" + path
}

func (client agentClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, method, client.endpoint(path), body)
	if err != nil {
		return nil, err
	}
	setDevelopmentIdentity(request, client.state)
	return request, nil
}

func (client agentClient) doJSON(ctx context.Context, method, path string, payload, target any, maxBody int64, okStatuses ...int) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	request, err := client.newRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if !statusAllowed(response.StatusCode, okStatuses) {
		return response, responseError(response)
	}
	if target != nil {
		reader := io.Reader(response.Body)
		if maxBody > 0 {
			reader = io.LimitReader(response.Body, maxBody)
		}
		if err := json.NewDecoder(reader).Decode(target); err != nil {
			return response, err
		}
	}
	return response, nil
}

func statusAllowed(status int, allowed []int) bool {
	if len(allowed) == 0 {
		return status >= 200 && status < 300
	}
	for _, value := range allowed {
		if status == value {
			return true
		}
	}
	return false
}

func (client agentClient) ResolveControlPlaneURL(ctx context.Context, server string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(server, "/")+"/healthz", nil)
	if err != nil {
		return "", err
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", responseError(response)
	}
	var health struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&health); err != nil {
		return "", fmt.Errorf("decode health response: %w", err)
	}
	if health.Status != "ok" {
		return "", fmt.Errorf("unexpected health status %q", health.Status)
	}

	finalURL := *response.Request.URL
	if !strings.EqualFold(request.URL.Hostname(), finalURL.Hostname()) {
		return "", fmt.Errorf("control plane redirected to a different host %q", finalURL.Hostname())
	}
	if strings.EqualFold(request.URL.Scheme, "https") && !strings.EqualFold(finalURL.Scheme, "https") {
		return "", errors.New("control plane attempted to downgrade HTTPS to HTTP")
	}
	if !strings.HasSuffix(finalURL.Path, "/healthz") {
		return "", fmt.Errorf("health check redirected to unexpected path %q", finalURL.Path)
	}
	finalURL.Path = strings.TrimSuffix(finalURL.Path, "/healthz")
	finalURL.RawPath = ""
	finalURL.RawQuery = ""
	finalURL.Fragment = ""
	return strings.TrimRight(finalURL.String(), "/"), nil
}

func (client agentClient) Enroll(ctx context.Context, token, name string, labels map[string]string) (agentState, error) {
	payload := enrollmentRequest{
		Token: token, Name: name, OS: runtime.GOOS + "/" + runtime.GOARCH,
		AgentVersion: agentVersion, Labels: labels,
	}
	var enrolled enrollmentResponse
	if _, err := client.doJSON(ctx, http.MethodPost, "/agent/v1/enroll", payload, &enrolled, 0, http.StatusCreated); err != nil {
		return agentState{}, err
	}
	if enrolled.Node.ID == "" || enrolled.CertificatePEM == "" || enrolled.PrivateKeyPEM == "" {
		return agentState{}, errors.New("control plane returned incomplete agent identity")
	}
	return agentState{
		NodeID: enrolled.Node.ID, Server: client.state.Server, CertificatePEM: enrolled.CertificatePEM,
		PrivateKeyPEM: enrolled.PrivateKeyPEM, CAPEM: enrolled.CAPEM, ExpiresAt: enrolled.ExpiresAt,
	}, nil
}

func (client agentClient) PollCommands(ctx context.Context) ([]agentCommand, bool, error) {
	var commands []agentCommand
	response, err := client.doJSON(ctx, http.MethodGet, "/agent/v1/commands?wait="+wireproto.CommandLongPollWait.String(), nil, &commands, 0, http.StatusOK)
	if err != nil {
		return nil, false, err
	}
	if commands == nil {
		commands = []agentCommand{}
	}
	return commands, response.Header.Get("X-WireMesh-Command-Long-Poll") == "true", nil
}

func (client agentClient) PostCommandResult(ctx context.Context, id, commandState, result string) error {
	_, err := client.doJSON(ctx, http.MethodPost, "/agent/v1/commands/"+id+"/result", wireproto.CommandResultRequest{State: commandState, Result: result}, nil, 0, http.StatusOK)
	return err
}

func (client agentClient) PostCommandProgress(ctx context.Context, id, progress string) error {
	_, err := client.doJSON(ctx, http.MethodPost, "/agent/v1/commands/"+id+"/progress", wireproto.CommandProgressRequest{Progress: progress}, nil, 0, http.StatusOK)
	return err
}

func (client agentClient) PostHeartbeat(ctx context.Context, heartbeat heartbeatRequest) error {
	_, err := client.doJSON(ctx, http.MethodPost, "/agent/v1/heartbeat", heartbeat, nil, 0, http.StatusOK)
	return err
}

func (client agentClient) PostConfigStatus(ctx context.Context, version uint64, status, message string) error {
	_, err := client.doJSON(ctx, http.MethodPost, "/agent/v1/status", wireproto.ConfigStatusRequest{Version: version, State: status, Message: message}, nil, 0, http.StatusOK)
	return err
}

func (client agentClient) PollConfig(ctx context.Context) (configResponse, bool, error) {
	payload, found, err := pollOptionalJSON[configResponse](ctx, client, "/agent/v1/config", http.StatusNotFound, http.StatusLocked)
	if err != nil {
		return configResponse{}, false, err
	}
	if !found {
		return configResponse{}, false, nil
	}
	return payload, true, nil
}

func (client agentClient) PollPeerConfig(ctx context.Context) (peerConfigResponse, bool, error) {
	payload, found, err := pollOptionalJSON[peerConfigResponse](ctx, client, "/agent/v1/peer-config", http.StatusNotFound)
	if err != nil {
		return peerConfigResponse{}, false, err
	}
	if !found {
		return peerConfigResponse{}, false, nil
	}
	if payload.Files == nil {
		payload.Files = []peerConfigFile{}
	}
	return payload, true, nil
}

func pollOptionalJSON[T any](ctx context.Context, client agentClient, path string, absentStatuses ...int) (T, bool, error) {
	var payload T
	request, err := client.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return payload, false, err
	}
	reply, err := client.httpClient.Do(request)
	if err != nil {
		return payload, false, err
	}
	defer reply.Body.Close()
	for _, status := range absentStatuses {
		if reply.StatusCode == status {
			return payload, false, nil
		}
	}
	if reply.StatusCode != http.StatusOK {
		return payload, false, responseError(reply)
	}
	if err := json.NewDecoder(reply.Body).Decode(&payload); err != nil {
		return payload, false, err
	}
	return payload, true, nil
}

func (client agentClient) PollUpdateManifest(ctx context.Context) (agentUpdateManifest, error) {
	endpoint := "/agent/v1/update?os=" + runtime.GOOS + "&arch=" + runtime.GOARCH + "&current_version=" + url.QueryEscape(agentVersion)
	var manifest agentUpdateManifest
	if _, err := client.doJSON(ctx, http.MethodGet, endpoint, nil, &manifest, 1<<20, http.StatusOK); err != nil {
		return agentUpdateManifest{}, err
	}
	return manifest, nil
}

func resolveControlPlaneURL(server string, httpClient *http.Client) (string, error) {
	return newAgentClient(httpClient, agentState{}).ResolveControlPlaneURL(context.Background(), server)
}

func enroll(httpClient *http.Client, server, token, name string, labels map[string]string) (agentState, error) {
	return newAgentClient(httpClient, agentState{Server: server}).Enroll(context.Background(), token, name, labels)
}

func authenticatedClient(state agentState, useMTLS bool) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if state.CAPEM != "" {
		roots.AppendCertsFromPEM([]byte(state.CAPEM))
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}
	if useMTLS && (state.CertificatePEM != "" || state.PrivateKeyPEM != "") {
		if state.CertificatePEM == "" || state.PrivateKeyPEM == "" {
			return nil, errors.New("incomplete enrolled client certificate material")
		}
		certificate, err := tls.X509KeyPair([]byte(state.CertificatePEM), []byte(state.PrivateKeyPEM))
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Timeout: 35 * time.Second, Transport: transport}, nil
}

func pollAgentCommands(ctx context.Context, httpClient *http.Client, state agentState) ([]agentCommand, bool, error) {
	return newAgentClient(httpClient, state).PollCommands(ctx)
}

func postAgentCommandResult(ctx context.Context, httpClient *http.Client, state agentState, id, commandState, result string) error {
	return newAgentClient(httpClient, state).PostCommandResult(ctx, id, commandState, result)
}

func postAgentCommandProgress(ctx context.Context, httpClient *http.Client, state agentState, id, progress string) error {
	return newAgentClient(httpClient, state).PostCommandProgress(ctx, id, progress)
}

func postHeartbeat(ctx context.Context, httpClient *http.Client, state agentState, heartbeat heartbeatRequest) error {
	return newAgentClient(httpClient, state).PostHeartbeat(ctx, heartbeat)
}

func postConfigStatus(ctx context.Context, httpClient *http.Client, state agentState, version uint64, status, message string) error {
	return newAgentClient(httpClient, state).PostConfigStatus(ctx, version, status, message)
}

func pollConfig(ctx context.Context, httpClient *http.Client, state agentState) (configResponse, bool, error) {
	return newAgentClient(httpClient, state).PollConfig(ctx)
}

func pollPeerConfig(ctx context.Context, httpClient *http.Client, state agentState) (peerConfigResponse, bool, error) {
	return newAgentClient(httpClient, state).PollPeerConfig(ctx)
}

func pollAgentUpdateManifest(ctx context.Context, httpClient *http.Client, state agentState) (agentUpdateManifest, error) {
	return newAgentClient(httpClient, state).PollUpdateManifest(ctx)
}

func setDevelopmentIdentity(request *http.Request, state agentState) {
	if state.NodeID != "" {
		request.Header.Set("X-Agent-ID", state.NodeID)
	}
	if state.PublicIP != "" {
		request.Header.Set("X-Agent-Public-IP", state.PublicIP)
	}
}

func responseError(response *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	message := strings.TrimSpace(string(data))
	if message == "" {
		message = response.Status
	}
	return fmt.Errorf("control plane returned %s: %s", response.Status, message)
}
