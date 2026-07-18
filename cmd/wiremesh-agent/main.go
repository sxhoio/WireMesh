package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const agentVersion = "0.3.2"

type enrollmentRequest struct {
	Token        string            `json:"token"`
	Name         string            `json:"name"`
	Endpoint     string            `json:"endpoint,omitempty"`
	Region       string            `json:"region,omitempty"`
	OS           string            `json:"os"`
	AgentVersion string            `json:"agent_version"`
	Labels       map[string]string `json:"labels"`
}

type enrollmentResponse struct {
	Node struct {
		ID string `json:"id"`
	} `json:"node"`
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	CAPEM          string `json:"ca_pem"`
	ExpiresAt      string `json:"expires_at"`
}

type agentState struct {
	NodeID           string `json:"node_id"`
	Server           string `json:"server"`
	CertificatePEM   string `json:"certificate_pem,omitempty"`
	PrivateKeyPEM    string `json:"private_key_pem,omitempty"`
	CAPEM            string `json:"ca_pem,omitempty"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	AppliedVersion   uint64 `json:"applied_version,omitempty"`
	AttemptedVersion uint64 `json:"attempted_version,omitempty"`
}

type agentCommand struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	State string `json:"state"`
}

type heartbeatRequest struct {
	Hostname        string                     `json:"hostname"`
	OS              string                     `json:"os"`
	AgentVersion    string                     `json:"agent_version"`
	Labels          map[string]string          `json:"labels"`
	Interfaces      string                     `json:"interfaces"`
	WireGuard       []wireGuardInterfaceStatus `json:"wireguard"`
	CollectionError string                     `json:"collection_error,omitempty"`
	Location        *agentLocation             `json:"location,omitempty"`
}

func main() {
	server := flag.String("server", "http://localhost:8080", "WireMesh control plane URL")
	enrollToken := flag.String("enroll-token", "", "one-time enrollment token")
	tokenFile := flag.String("token-file", "", "file containing a one-time enrollment token")
	nodeID := flag.String("node-id", "", "existing node identity for HTTP development mode")
	name := flag.String("name", "", "node name used during enrollment")
	labelsText := flag.String("labels", "", "comma-separated labels (key=value)")
	interfaces := flag.String("interfaces", "auto", "WireGuard interface selection")
	stateDir := flag.String("state-dir", "/var/lib/wiremesh-agent", "directory used for enrolled identity material")
	reportInterval := flag.Duration("report-interval", 10*time.Second, "heartbeat interval")
	probeInterval := flag.Duration("probe-interval", 15*time.Second, "configuration polling interval")
	useMTLS := flag.Bool("mtls", false, "use the enrolled client certificate for HTTPS")
	flag.Parse()

	if *reportInterval < time.Second || *probeInterval < time.Second {
		log.Fatal("report and probe intervals must be at least one second")
	}
	*server = strings.TrimRight(strings.TrimSpace(*server), "/")
	if *server == "" {
		log.Fatal("server URL is required")
	}
	statePath := filepath.Join(*stateDir, "identity.json")
	state, err := loadState(statePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("load agent identity: %v", err)
	}
	if state.NodeID == "" && *nodeID != "" {
		state = agentState{NodeID: *nodeID, Server: *server}
	}
	probeState := state
	probeState.Server = *server
	probeClient, probeErr := authenticatedClient(probeState, *useMTLS)
	if probeErr != nil {
		log.Printf("warning: configure control plane URL discovery transport: %v", probeErr)
	} else if resolvedServer, resolveErr := resolveControlPlaneURL(*server, probeClient); resolveErr != nil {
		log.Printf("warning: control plane URL discovery failed for %s: %v", *server, resolveErr)
	} else if resolvedServer != *server {
		log.Printf("control plane URL redirected from %s to %s; using the final URL for agent requests", *server, resolvedServer)
		*server = resolvedServer
	}
	if state.NodeID == "" {
		token, err := readEnrollmentToken(*enrollToken, *tokenFile)
		if err != nil {
			log.Fatal(err)
		}
		if strings.TrimSpace(*name) == "" {
			log.Fatal("name is required for first-time enrollment")
		}
		state, err = enroll(probeClient, *server, token, strings.TrimSpace(*name), parseLabels(*labelsText))
		if err != nil {
			log.Fatalf("enroll agent: %v", err)
		}
		if err := saveState(statePath, state); err != nil {
			log.Fatalf("persist agent identity: %v", err)
		}
		if *tokenFile != "" {
			if err := os.Remove(*tokenFile); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("warning: remove consumed enrollment token file: %v", err)
			}
		}
		log.Printf("enrolled node %s", state.NodeID)
	}
	if state.Server != *server {
		previousServer := state.Server
		state.Server = *server
		if err := saveState(statePath, state); err != nil {
			log.Fatalf("persist control plane URL: %v", err)
		}
		if previousServer != "" {
			log.Printf("control plane URL changed from %s to %s", previousServer, state.Server)
		}
	}

	mtlsActive := *useMTLS && state.CertificatePEM != "" && state.PrivateKeyPEM != ""
	if *useMTLS && !mtlsActive {
		log.Printf("warning: --mtls requested but enrolled certificate material is unavailable; using the Agent identity header")
	}
	client, err := authenticatedClient(state, *useMTLS)
	if err != nil {
		log.Fatalf("configure agent transport: %v", err)
	}
	hostname, _ := os.Hostname()
	baseHeartbeat := heartbeatRequest{
		Hostname: hostname, OS: runtime.GOOS + "/" + runtime.GOARCH,
		AgentVersion: agentVersion, Labels: parseLabels(*labelsText), Interfaces: *interfaces,
	}
	manager := wireGuardManager{runner: execCommandRunner{}, configDir: "/etc/wireguard"}
	transportMode := "HTTP development identity"
	if mtlsActive && strings.HasPrefix(strings.ToLower(state.Server), "https://") {
		transportMode = "HTTPS mutual TLS with Agent identity header fallback"
	} else if mtlsActive {
		transportMode = "HTTP redirect with mutual TLS and Agent identity header fallback"
	} else if strings.HasPrefix(strings.ToLower(state.Server), "https://") {
		transportMode = "HTTPS Agent identity header"
	}
	log.Printf("agent started: node=%s server=%s transport=%s interfaces=%s report_interval=%s probe_interval=%s",
		state.NodeID, state.Server, transportMode, *interfaces, reportInterval.String(), probeInterval.String())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	heartbeatAccepted := false
	lastCollectionError := ""
	lastLocationAttempt := time.Time{}
	lastLocationError := ""
	lastLocationSummary := ""
	refreshLocation := func() {
		if !lastLocationAttempt.IsZero() && time.Since(lastLocationAttempt) < 30*time.Minute {
			return
		}
		lastLocationAttempt = time.Now()
		location, locationErr := fetchAgentLocation(ctx, client, state)
		if locationErr != nil {
			message := locationErr.Error()
			if message != lastLocationError {
				log.Printf("location discovery warning: %s", message)
			}
			lastLocationError = message
			return
		}
		if publicIP, publicErr := fetchPublicIPv4WithTimeout(ctx, client, publicIPv4URLs(os.Getenv("WIREMESH_PUBLIC_IP_URL")), 8*time.Second); publicErr != nil {
			log.Printf("public IPv4 discovery warning: %v; falling back to the server-observed address", publicErr)
		} else {
			if publicIP != location.PublicIP {
				log.Printf("public IPv4 discovered by the agent: %s (server observed %s)", publicIP, location.PublicIP)
			}
			location.PublicIP = publicIP
			// The server-resolved coordinates describe the observed address, not
			// the discovered one; drop them so the server re-resolves via GeoIP.
			location.LocationName = ""
			location.LocationSource = ""
			location.Latitude = 0
			location.Longitude = 0
		}
		if lastLocationError != "" {
			log.Printf("location discovery recovered")
		}
		lastLocationError = ""
		if summary := location.summary(); summary != lastLocationSummary {
			log.Printf("location discovery completed: %s", summary)
			lastLocationSummary = summary
		}
		baseHeartbeat.Location = &location
	}
	sendHeartbeat := func() {
		refreshLocation()
		heartbeat := baseHeartbeat
		heartbeat.WireGuard, heartbeat.CollectionError = collectWireGuard(ctx, *interfaces, manager.runner)
		if heartbeat.CollectionError != lastCollectionError {
			if heartbeat.CollectionError != "" {
				log.Printf("WireGuard collection warning: %s", heartbeat.CollectionError)
			} else if lastCollectionError != "" {
				log.Printf("WireGuard collection recovered")
			}
			lastCollectionError = heartbeat.CollectionError
		}
		if err := postHeartbeat(ctx, client, state, heartbeat); err != nil {
			log.Printf("heartbeat failed: %v", err)
			return
		}
		if !heartbeatAccepted {
			log.Printf("heartbeat accepted by server: node=%s wireguard_interfaces=%d", state.NodeID, len(heartbeat.WireGuard))
			heartbeatAccepted = true
		}
	}
	configurationChecked := false
	reconcileConfiguration := func() {
		payload, found, err := pollConfig(ctx, client, state)
		if err != nil {
			log.Printf("configuration check failed: %v", err)
			return
		}
		if !found {
			if !configurationChecked {
				log.Printf("no published WireGuard configuration is available for node %s", state.NodeID)
				configurationChecked = true
			}
			return
		}
		configurationChecked = true
		if payload.Version == 0 || payload.Version <= state.AttemptedVersion {
			return
		}
		status, message := manager.Apply(ctx, payload.Config, state.NodeID)
		if err := postConfigStatus(ctx, client, state, payload.Version, status, message); err != nil {
			log.Printf("report configuration version %d result: %v", payload.Version, err)
			return
		}
		state.AttemptedVersion = payload.Version
		if status == "applied" {
			state.AppliedVersion = payload.Version
		}
		if err := saveState(statePath, state); err != nil {
			log.Printf("persist configuration state: %v", err)
		}
		log.Printf("configuration version %d: %s", payload.Version, status)
	}

	sendHeartbeat()
	reconcileConfiguration()

	processCommands := func() {
		commands, err := pollAgentCommands(ctx, client, state)
		if err != nil {
			log.Printf("agent command check failed: %v", err)
			return
		}
		for _, command := range commands {
			result, commandErr := "", error(nil)
			switch command.Type {
			case "collect":
				heartbeat := baseHeartbeat
				heartbeat.WireGuard, heartbeat.CollectionError = collectWireGuard(ctx, *interfaces, manager.runner)
				commandErr = postHeartbeat(ctx, client, state, heartbeat)
				result = fmt.Sprintf("wireguard_interfaces=%d", len(heartbeat.WireGuard))
				if heartbeat.CollectionError != "" {
					result += " warning=" + heartbeat.CollectionError
				}
			case "connectivity_check":
				result, commandErr = connectivityCheck(ctx, *interfaces, manager.runner)
			default:
				commandErr = fmt.Errorf("unsupported command type %s", command.Type)
			}
			commandState := "completed"
			if commandErr != nil {
				commandState = "failed"
				if result != "" {
					result += "; "
				}
				result += commandErr.Error()
			}
			if err := postAgentCommandResult(ctx, client, state, command.ID, commandState, result); err != nil {
				log.Printf("report command %s result: %v", command.ID, err)
			} else {
				log.Printf("agent command %s (%s): %s", command.ID, command.Type, commandState)
			}
		}
	}

	processCommands()

	reportTicker := time.NewTicker(*reportInterval)
	probeTicker := time.NewTicker(*probeInterval)
	defer reportTicker.Stop()
	defer probeTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("agent stopped")
			return
		case <-reportTicker.C:
			sendHeartbeat()
		case <-probeTicker.C:
			reconcileConfiguration()
			processCommands()
		}
	}
}

func resolveControlPlaneURL(server string, client *http.Client) (string, error) {
	request, err := http.NewRequest(http.MethodGet, server+"/healthz", nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
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

func enroll(client *http.Client, server, token, name string, labels map[string]string) (agentState, error) {
	body, err := json.Marshal(enrollmentRequest{
		Token: token, Name: name, OS: runtime.GOOS + "/" + runtime.GOARCH,
		AgentVersion: agentVersion, Labels: labels,
	})
	if err != nil {
		return agentState{}, err
	}
	response, err := client.Post(server+"/agent/v1/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		return agentState{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return agentState{}, responseError(response)
	}
	var enrolled enrollmentResponse
	if err := json.NewDecoder(response.Body).Decode(&enrolled); err != nil {
		return agentState{}, err
	}
	if enrolled.Node.ID == "" || enrolled.CertificatePEM == "" || enrolled.PrivateKeyPEM == "" {
		return agentState{}, errors.New("control plane returned incomplete agent identity")
	}
	return agentState{
		NodeID: enrolled.Node.ID, Server: server, CertificatePEM: enrolled.CertificatePEM,
		PrivateKeyPEM: enrolled.PrivateKeyPEM, CAPEM: enrolled.CAPEM, ExpiresAt: enrolled.ExpiresAt,
	}, nil
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
	return &http.Client{Timeout: 20 * time.Second, Transport: transport}, nil
}

func pollAgentCommands(ctx context.Context, client *http.Client, state agentState) ([]agentCommand, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, state.Server+"/agent/v1/commands", nil)
	if err != nil {
		return nil, err
	}
	setDevelopmentIdentity(request, state)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, responseError(response)
	}
	var commands []agentCommand
	if err := json.NewDecoder(response.Body).Decode(&commands); err != nil {
		return nil, err
	}
	if commands == nil {
		commands = []agentCommand{}
	}
	return commands, nil
}

func postAgentCommandResult(ctx context.Context, client *http.Client, state agentState, id, commandState, result string) error {
	body, err := json.Marshal(map[string]string{"state": commandState, "result": result})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, state.Server+"/agent/v1/commands/"+id+"/result", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	setDevelopmentIdentity(request, state)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return responseError(response)
	}
	return nil
}

func postHeartbeat(ctx context.Context, client *http.Client, state agentState, heartbeat heartbeatRequest) error {
	body, err := json.Marshal(heartbeat)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, state.Server+"/agent/v1/heartbeat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	setDevelopmentIdentity(request, state)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return responseError(response)
	}
	return nil
}

func postConfigStatus(ctx context.Context, client *http.Client, state agentState, version uint64, status, message string) error {
	body, err := json.Marshal(map[string]any{"version": version, "state": status, "message": message})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, state.Server+"/agent/v1/status", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	setDevelopmentIdentity(request, state)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return responseError(response)
	}
	return nil
}

func pollConfig(ctx context.Context, client *http.Client, state agentState) (configResponse, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, state.Server+"/agent/v1/config", nil)
	if err != nil {
		return configResponse{}, false, err
	}
	setDevelopmentIdentity(request, state)
	response, err := client.Do(request)
	if err != nil {
		return configResponse{}, false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusLocked {
		return configResponse{}, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return configResponse{}, false, responseError(response)
	}
	var payload configResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return configResponse{}, false, err
	}
	return payload, true, nil
}

func setDevelopmentIdentity(request *http.Request, state agentState) {
	if state.NodeID != "" {
		request.Header.Set("X-Agent-ID", state.NodeID)
	}
}

func readEnrollmentToken(value, filename string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value), nil
	}
	if filename == "" {
		return "", errors.New("provide --enroll-token or --token-file for first-time enrollment")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("read enrollment token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("enrollment token file is empty")
	}
	return token, nil
}

func parseLabels(value string) map[string]string {
	labels := map[string]string{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, labelValue, found := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if !found {
			labelValue = "true"
		}
		labels[key] = strings.TrimSpace(labelValue)
	}
	return labels
}

func loadState(filename string) (agentState, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return agentState{}, err
	}
	var state agentState
	if err := json.Unmarshal(data, &state); err != nil {
		return agentState{}, err
	}
	return state, nil
}

func saveState(filename string, state agentState) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary := filename + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, filename)
}

func responseError(response *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	message := strings.TrimSpace(string(data))
	if message == "" {
		message = response.Status
	}
	return fmt.Errorf("control plane returned %s: %s", response.Status, message)
}
