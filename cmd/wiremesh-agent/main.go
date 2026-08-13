package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wiremesh/wiremesh/internal/wireproto"
)

const (
	commandPollFallbackRetryDelay = 2 * time.Second
)

var agentVersion = "0.3.6"

type enrollmentRequest = wireproto.EnrollmentRequest
type enrollmentResponse = wireproto.EnrollmentResponse

type agentState struct {
	NodeID           string `json:"node_id"`
	Server           string `json:"server"`
	PublicIP         string `json:"public_ip,omitempty"`
	CertificatePEM   string `json:"certificate_pem,omitempty"`
	PrivateKeyPEM    string `json:"private_key_pem,omitempty"`
	CAPEM            string `json:"ca_pem,omitempty"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	AppliedVersion   uint64 `json:"applied_version,omitempty"`
	AttemptedVersion uint64 `json:"attempted_version,omitempty"`
}

type agentCommand = wireproto.AgentCommand
type heartbeatRequest = wireproto.HeartbeatRequest

func main() {
	if len(os.Args) > 1 && os.Args[1] == "update-helper" {
		if err := runUpdateHelper(os.Args[2:]); err != nil {
			log.Fatalf("agent update helper: %v", err)
		}
		return
	}
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
	// Resolve the real public IPv4 once at startup and reuse it for the whole
	// process lifetime; it is refreshed only when the agent process restarts.
	publicIPEndpoint := strings.TrimSpace(os.Getenv("WIREMESH_PUBLIC_IP_URL"))
	if publicIPEndpoint == "" {
		publicIPEndpoint = "https://ipv4.ip.sb"
	}
	if publicIP, publicErr := fetchRealPublicIPv4(context.Background(), client, publicIPEndpoint); publicErr != nil {
		log.Printf("public IPv4 discovery warning: %v; the server will GeoIP-locate the connection source address instead", publicErr)
	} else {
		state.PublicIP = publicIP
		log.Printf("public IPv4 discovered at startup: %s", publicIP)
	}
	agentAPI := newAgentClient(client, state)
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
	// heartbeatMu 串行化心跳上报；configMu 串行化 WireGuard 配置应用，避免探测
	// 与命令工作 goroutine 并发执行 wg-quick 操作。
	var heartbeatMu sync.Mutex
	var configMu sync.Mutex
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
		location, locationErr := agentAPI.FetchLocation(ctx)
		if locationErr != nil {
			message := locationErr.Error()
			if message != lastLocationError {
				log.Printf("location discovery warning: %s", message)
			}
			lastLocationError = message
			return
		}
		if lastLocationError != "" {
			log.Printf("location discovery recovered")
		}
		lastLocationError = ""
		if summary := agentLocationSummary(location); summary != lastLocationSummary {
			log.Printf("location discovery completed: %s", summary)
			lastLocationSummary = summary
		}
		baseHeartbeat.Location = &location
	}
	sendHeartbeat := func() (int, string, error) {
		heartbeatMu.Lock()
		defer heartbeatMu.Unlock()
		refreshLocation()
		heartbeat := baseHeartbeat
		heartbeat.WireGuard, heartbeat.CollectionError = collectWireGuard(ctx, *interfaces, manager.runner)
		peerConfigs, peerConfigWarning := collectPeerConfigFiles(*interfaces, manager.configDir, heartbeat.WireGuard)
		heartbeat.PeerConfigs = peerConfigs
		if peerConfigWarning != "" {
			if heartbeat.CollectionError != "" {
				heartbeat.CollectionError += "; "
			}
			heartbeat.CollectionError += peerConfigWarning
		}
		if heartbeat.CollectionError != lastCollectionError {
			if heartbeat.CollectionError != "" {
				log.Printf("WireGuard collection warning: %s", heartbeat.CollectionError)
			} else if lastCollectionError != "" {
				log.Printf("WireGuard collection recovered")
			}
			lastCollectionError = heartbeat.CollectionError
		}
		if err := agentAPI.PostHeartbeat(ctx, heartbeat); err != nil {
			log.Printf("heartbeat failed: %v", err)
			return len(heartbeat.WireGuard), heartbeat.CollectionError, err
		}
		if !heartbeatAccepted {
			log.Printf("heartbeat accepted by server: node=%s wireguard_interfaces=%d", state.NodeID, len(heartbeat.WireGuard))
			heartbeatAccepted = true
		}
		return len(heartbeat.WireGuard), heartbeat.CollectionError, nil
	}
	configurationChecked := false
	reconcileConfiguration := func() (string, error) {
		configMu.Lock()
		defer configMu.Unlock()
		payload, found, err := agentAPI.PollConfig(ctx)
		if err != nil {
			log.Printf("configuration check failed: %v", err)
			return "", err
		}
		if !found {
			if !configurationChecked {
				log.Printf("no published WireGuard configuration is available for node %s", state.NodeID)
				configurationChecked = true
			}
			return "no pending configuration", nil
		}
		configurationChecked = true
		if payload.Version == 0 || payload.Version <= state.AttemptedVersion {
			return fmt.Sprintf("configuration version %d already attempted", payload.Version), nil
		}
		status, message := manager.Apply(ctx, payload.Config, state.NodeID)
		if err := agentAPI.PostConfigStatus(ctx, payload.Version, status, message); err != nil {
			log.Printf("report configuration version %d result: %v", payload.Version, err)
			return "", err
		}
		state.AttemptedVersion = payload.Version
		if status == "applied" {
			state.AppliedVersion = payload.Version
		}
		if err := saveState(statePath, state); err != nil {
			log.Printf("persist configuration state: %v", err)
		}
		log.Printf("configuration version %d: %s", payload.Version, status)
		result := fmt.Sprintf("version=%d state=%s", payload.Version, status)
		if message != "" {
			result += " message=" + message
		}
		if status != "applied" {
			if message == "" {
				message = status
			}
			return result, errors.New(message)
		}
		return result, nil
	}

	sendHeartbeat()
	reconcileConfiguration()

	processCommands := func(commands []agentCommand) {
		for _, command := range commands {
			result, commandErr := "", error(nil)
			switch command.Type {
			case "collect":
				interfaceCount, collectionError, heartbeatErr := sendHeartbeat()
				commandErr = heartbeatErr
				result = fmt.Sprintf("wireguard_interfaces=%d", interfaceCount)
				if collectionError != "" {
					result += " warning=" + collectionError
				}
			case "apply_config":
				result, commandErr = reconcileConfiguration()
			case "apply_peer_config":
				configMu.Lock()
				payload, found, err := agentAPI.PollPeerConfig(ctx)
				if err != nil {
					commandErr = err
					result = "peer_config_fetch=failed"
				} else if !found {
					result = "no pending peer config"
				} else {
					result, commandErr = manager.ApplyPeerConfigFiles(ctx, payload.Files)
				}
				configMu.Unlock()
				if commandErr == nil {
					_, _, _ = sendHeartbeat()
				}
			case "update_agent":
				result, commandErr = performAgentUpdate(ctx, agentAPI, statePath, *stateDir, *useMTLS, command.ID)
			case "connectivity_check":
				result, commandErr = connectivityCheck(ctx, *interfaces, manager.runner)
			default:
				commandErr = fmt.Errorf("unsupported command type %s", command.Type)
			}
			if errors.Is(commandErr, errAgentUpdateHandedOff) {
				log.Printf("agent command %s (%s): update helper started", command.ID, command.Type)
				continue
			}
			commandState := "completed"
			if commandErr != nil {
				commandState = "failed"
				if result != "" {
					result += "; "
				}
				result += commandErr.Error()
			}
			if err := agentAPI.PostCommandResult(ctx, command.ID, commandState, result); err != nil {
				log.Printf("report command %s result: %v", command.ID, err)
			} else {
				log.Printf("agent command %s (%s): %s", command.ID, command.Type, commandState)
			}
		}
	}

	reportTicker := time.NewTicker(*reportInterval)
	probeTicker := time.NewTicker(*probeInterval)
	defer reportTicker.Stop()
	defer probeTicker.Stop()
	commandBatches := make(chan []agentCommand)
	go func() {
		for {
			commands, longPollSupported, err := agentAPI.PollCommands(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("agent command long poll failed: %v", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(commandPollFallbackRetryDelay):
				}
				continue
			}
			if len(commands) == 0 {
				if !longPollSupported {
					select {
					case <-ctx.Done():
						return
					case <-time.After(commandPollFallbackRetryDelay):
					}
				}
				continue
			}
			select {
			case <-ctx.Done():
				return
			case commandBatches <- commands:
			}
		}
	}()
	// 命令在工作 goroutine 中串行执行，长命令（下载更新、连通性检测）不再阻塞
	// 心跳上报与配置探测；WireGuard 配置操作由 configMu 串行化。
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case commands := <-commandBatches:
				processCommands(commands)
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			log.Printf("agent stopped")
			return
		case <-reportTicker.C:
			sendHeartbeat()
		case <-probeTicker.C:
			reconcileConfiguration()
		}
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
