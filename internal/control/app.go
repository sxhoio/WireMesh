package control

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/mail"
	"reflect"
	"strings"
	"sync"
	"time"
)

type Config struct {
	MasterKey       string
	Store           Store
	Database        *DatabaseManager
	DatabaseDriver  string
	AgentBinaryPath string
	AgentVersion    string
}
type App struct {
	store           Store
	database        *DatabaseManager
	databaseDriver  string
	box             *SecretBox
	auth            *Authenticator
	ca              *x509.Certificate
	caKey           any
	geoMu           sync.RWMutex
	geoReaders      map[string]*geoReaderState
	geoLookup       func(string, string) (geoIPLocation, error)
	commandMu       sync.Mutex
	commandWakeups  map[string]chan struct{}
	agentBinaryPath string
	agentVersion    string
}

func NewApp(cfg Config) (*App, error) {
	box, err := NewSecretBox(cfg.MasterKey)
	if err != nil {
		return nil, err
	}
	store := cfg.Store
	if store == nil {
		store = NewMemoryStore()
	}
	app := &App{
		store:           store,
		database:        cfg.Database,
		databaseDriver:  cfg.DatabaseDriver,
		box:             box,
		geoReaders:      map[string]*geoReaderState{},
		commandWakeups:  map[string]chan struct{}{},
		agentBinaryPath: cfg.AgentBinaryPath,
		agentVersion:    strings.TrimSpace(cfg.AgentVersion),
	}
	app.geoLookup = app.lookupGeoIPLocation
	app.auth = newAuthenticator(store, cfg.MasterKey+"-auth")
	if err := app.newCertificateAuthority(); err != nil {
		return nil, err
	}
	return app, nil
}

func (a *App) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /api/v1/setup/status", a.setupStatus)
	mux.HandleFunc("GET /api/v1/setup/database", a.databaseStatus)
	mux.HandleFunc("POST /api/v1/setup/database/test", a.testDatabase)
	mux.HandleFunc("POST /api/v1/setup/database", a.configureDatabase)
	mux.HandleFunc("POST /api/v1/setup", a.setup)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("GET /api/v1/auth/me", a.withUser(RoleViewer, a.me))
	mux.HandleFunc("GET /api/v1/projects", a.withUser(RoleViewer, a.projects))
	mux.HandleFunc("POST /api/v1/projects", a.withUser(RoleAdmin, a.projects))
	mux.HandleFunc("GET /api/v1/networks", a.withUser(RoleViewer, a.networks))
	mux.HandleFunc("POST /api/v1/networks", a.withUser(RoleOperator, a.networks))
	mux.HandleFunc("GET /api/v1/nodes", a.withUser(RoleViewer, a.nodes))
	mux.HandleFunc("POST /api/v1/nodes", a.withUser(RoleOperator, a.nodes))
	mux.HandleFunc("GET /api/v1/nodes/{id}", a.withUser(RoleViewer, a.nodeByID))
	mux.HandleFunc("PATCH /api/v1/nodes/{id}", a.withUser(RoleOperator, a.updateNode))
	mux.HandleFunc("DELETE /api/v1/nodes/{id}", a.withUser(RoleAdmin, a.deleteNode))
	mux.HandleFunc("GET /api/v1/nodes/{id}/peer-config", a.withUser(RoleViewer, a.nodePeerConfig))
	mux.HandleFunc("PUT /api/v1/nodes/{id}/peer-config", a.withUser(RoleOperator, a.updateNodePeerConfig))
	mux.HandleFunc("POST /api/v1/nodes/{id}/collect", a.withUser(RoleOperator, a.createNodeCommand("collect")))
	mux.HandleFunc("POST /api/v1/nodes/collect", a.withUser(RoleOperator, a.collectNodes))
	mux.HandleFunc("POST /api/v1/nodes/{id}/update-agent", a.withUser(RoleOperator, a.createNodeCommand("update_agent")))
	mux.HandleFunc("POST /api/v1/nodes/update-agent", a.withUser(RoleOperator, a.updateAgents))
	mux.HandleFunc("POST /api/v1/nodes/{id}/connectivity-check", a.withUser(RoleOperator, a.createNodeCommand("connectivity_check")))
	mux.HandleFunc("GET /api/v1/nodes/{id}/logs", a.withUser(RoleViewer, a.nodeLogs))
	mux.HandleFunc("DELETE /api/v1/nodes/{id}/logs", a.withUser(RoleOperator, a.clearNodeLogs))
	mux.HandleFunc("GET /api/v1/nodes/{id}/traffic", a.withUser(RoleViewer, a.nodeTraffic))
	mux.HandleFunc("POST /api/v1/networks/{id}/peers", a.withUser(RoleOperator, a.addPeer))
	mux.HandleFunc("POST /api/v1/networks/{id}/publish", a.withUser(RoleOperator, a.publish))
	mux.HandleFunc("GET /api/v1/deliveries", a.withUser(RoleViewer, a.deliveries))
	mux.HandleFunc("GET /api/v1/audit", a.withUser(RoleAdmin, a.audit))
	mux.HandleFunc("DELETE /api/v1/audit", a.withUser(RoleAdmin, a.clearAudit))
	mux.HandleFunc("GET /api/v1/settings", a.withUser(RoleViewer, a.settings))
	mux.HandleFunc("PUT /api/v1/settings", a.withUser(RoleAdmin, a.settings))
	mux.HandleFunc("GET /api/v1/settings/geoip", a.withUser(RoleViewer, a.geoIPStatus))
	mux.HandleFunc("PUT /api/v1/settings/geoip", a.withUser(RoleAdmin, a.updateGeoIP))
	mux.HandleFunc("POST /api/v1/settings/geoip/reload", a.withUser(RoleAdmin, a.reloadGeoIP))
	mux.HandleFunc("GET /api/v1/settings/geoip/lookup", a.withUser(RoleViewer, a.lookupGeoIP))
	mux.HandleFunc("GET /api/v1/settings/notifications", a.withUser(RoleViewer, a.notificationChannels))
	mux.HandleFunc("POST /api/v1/settings/notifications", a.withUser(RoleAdmin, a.notificationChannels))
	mux.HandleFunc("PUT /api/v1/settings/notifications/{id}", a.withUser(RoleAdmin, a.updateNotificationChannel))
	mux.HandleFunc("DELETE /api/v1/settings/notifications/{id}", a.withUser(RoleAdmin, a.deleteNotificationChannel))
	mux.HandleFunc("POST /api/v1/settings/notifications/{id}/test", a.withUser(RoleAdmin, a.testNotificationChannel))
	mux.HandleFunc("GET /api/v1/settings/notification-logs", a.withUser(RoleViewer, a.notificationLogs))
	mux.HandleFunc("GET /api/v1/users", a.withUser(RoleAdmin, a.users))
	mux.HandleFunc("POST /api/v1/users", a.withUser(RoleAdmin, a.users))
	mux.HandleFunc("GET /api/v1/agent/update", a.withUser(RoleViewer, a.agentUpdateInfo))
	mux.HandleFunc("POST /api/v1/agent/enrollment-tokens", a.withUser(RoleAdmin, a.createEnrollment))
	mux.HandleFunc("GET /agent/install.sh", a.agentInstallScript)
	mux.HandleFunc("GET /agent/uninstall.sh", a.agentUninstallScript)
	mux.HandleFunc("GET /agent/download", a.agentDownload)
	mux.HandleFunc("POST /agent/v1/enroll", a.enroll)
	mux.HandleFunc("GET /agent/v1/update", a.agentUpdate)
	mux.HandleFunc("GET /agent/v1/config", a.agentConfig)
	mux.HandleFunc("GET /agent/v1/peer-config", a.agentPeerConfig)
	mux.HandleFunc("GET /agent/v1/location", a.agentLocation)
	mux.HandleFunc("POST /agent/v1/status", a.agentStatus)
	mux.HandleFunc("POST /agent/v1/heartbeat", a.agentHeartbeat)
	mux.HandleFunc("GET /agent/v1/commands", a.agentCommands)
	mux.HandleFunc("POST /agent/v1/commands/{id}/progress", a.agentCommandProgress)
	mux.HandleFunc("POST /agent/v1/commands/{id}/result", a.agentCommandResult)
	return cors(mux)
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) setupStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := a.store.HasUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read setup status")
		return
	}
	status := DatabaseStatus{Configured: true, Driver: a.databaseDriver}
	if a.database != nil {
		status = a.database.Status()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"initialized":           initialized,
		"database_configured":   status.Configured,
		"database_driver":       status.Driver,
		"database_configurable": a.database != nil,
	})
}

func (a *App) setup(w http.ResponseWriter, r *http.Request) {
	if a.database != nil && !a.database.Status().Configured {
		writeError(w, http.StatusConflict, "configure a database before creating the administrator")
		return
	}
	var in struct {
		Email    string
		Name     string
		Password string
	}
	if !decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	parsedEmail, err := mail.ParseAddress(in.Email)
	if err != nil || strings.ToLower(parsedEmail.Address) != in.Email {
		writeError(w, http.StatusBadRequest, "valid email is required")
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(in.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	passwordHash, err := hashPassword(in.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to secure password")
		return
	}
	user := User{
		ID:           newID("usr"),
		TenantID:     newID("tenant"),
		Email:        in.Email,
		Name:         in.Name,
		Role:         RoleAdmin,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now().UTC(),
	}
	var createErr error
	if a.database != nil {
		createErr = a.database.CreateInitialAdmin(user)
	} else {
		createErr = a.store.CreateInitialAdmin(user)
	}
	if err := createErr; err != nil {
		if errors.Is(err, errAlreadyInitialized) {
			writeError(w, http.StatusConflict, "WireMesh is already initialized")
			return
		}
		if errors.Is(err, errDatabaseNotConfigured) {
			writeError(w, http.StatusConflict, "configure a database before creating the administrator")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create administrator")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": publicUser(user)})
}
func (a *App) withUser(required Role, next func(http.ResponseWriter, *http.Request, claims)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		c, err := a.auth.Parse(header)
		if err != nil || !allowed(c.Role, required) {
			writeError(w, http.StatusUnauthorized, "authentication or permission denied")
			return
		}
		next(w, r, c)
	}
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !decode(w, r, &in) {
		return
	}
	token, user, err := a.auth.Login(in.Email, in.Password)
	if err != nil {
		if errors.Is(err, errLoginPersistence) {
			writeError(w, http.StatusInternalServerError, "failed to record login")
		} else {
			writeError(w, http.StatusUnauthorized, err.Error())
		}
		return
	}
	a.auditEvent(user.TenantID, user.ID, "auth.login", "user", user.ID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": publicUser(user)})
}
func (a *App) me(w http.ResponseWriter, r *http.Request, c claims) {
	user, _ := a.store.GetUser(c.Subject)
	writeJSON(w, http.StatusOK, publicUser(user))
}
func publicUser(u User) map[string]any {
	var lastLoginAt any
	if !u.LastLoginAt.IsZero() {
		lastLoginAt = u.LastLoginAt
	}
	return map[string]any{"id": u.ID, "tenant_id": u.TenantID, "email": u.Email, "name": u.Name, "role": u.Role, "last_login_at": lastLoginAt}
}

func (a *App) projects(w http.ResponseWriter, r *http.Request, c claims) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, a.store.ListProjects(c.TenantID))
		return
	}
	var in struct{ Name, Description string }
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeError(w, 400, "name is required")
		return
	}
	v := Project{ID: newID("prj"), TenantID: c.TenantID, Name: in.Name, Description: in.Description, CreatedAt: time.Now()}
	if err := a.store.CreateProject(v); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create project")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "project.create", "project", v.ID, nil)
	writeJSON(w, 201, v)
}
func (a *App) networks(w http.ResponseWriter, r *http.Request, c claims) {
	if r.Method == http.MethodGet {
		writeJSON(w, 200, a.store.ListNetworks(c.TenantID, r.URL.Query().Get("project_id")))
		return
	}
	var in struct {
		ProjectID string   `json:"project_id"`
		Name      string   `json:"name"`
		CIDR      string   `json:"cidr"`
		DNS       string   `json:"dns"`
		Topology  Topology `json:"topology"`
	}
	if !decode(w, r, &in) {
		return
	}
	if _, err := a.store.GetProject(c.TenantID, in.ProjectID); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if in.Topology == "" {
		in.Topology = TopologyFullMesh
	}
	if _, err := AllocateAddress(in.CIDR, nil); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if in.Topology != TopologyFullMesh && in.Topology != TopologyHubSpoke && in.Topology != TopologyCustom {
		writeError(w, 400, "invalid topology")
		return
	}
	v := Network{ID: newID("net"), TenantID: c.TenantID, ProjectID: in.ProjectID, Name: in.Name, CIDR: in.CIDR, DNS: in.DNS, Topology: in.Topology, CreatedAt: time.Now()}
	if err := a.store.CreateNetwork(v); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create network")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "network.create", "network", v.ID, nil)
	writeJSON(w, 201, v)
}
func (a *App) nodes(w http.ResponseWriter, r *http.Request, c claims) {
	if r.Method == http.MethodGet {
		writeJSON(w, 200, a.store.ListNodes(c.TenantID, r.URL.Query().Get("network_id")))
		return
	}
	var in struct {
		NetworkID    string            `json:"network_id"`
		Name         string            `json:"name"`
		Endpoint     string            `json:"endpoint"`
		Region       string            `json:"region"`
		OS           string            `json:"os"`
		AgentVersion string            `json:"agent_version"`
		Labels       map[string]string `json:"labels"`
	}
	if !decode(w, r, &in) {
		return
	}
	network, err := a.store.GetNetwork(c.TenantID, in.NetworkID)
	if err != nil {
		writeError(w, 404, "network not found")
		return
	}
	node, err := a.createNode(c.TenantID, network, in.Name, in.Endpoint, in.Region, in.OS, in.AgentVersion, in.Labels)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "node.create", "node", node.ID, nil)
	writeJSON(w, 201, node)
}

func (a *App) addPeer(w http.ResponseWriter, r *http.Request, c claims) {
	networkID := r.PathValue("id")
	network, err := a.store.GetNetwork(c.TenantID, networkID)
	if err != nil {
		writeError(w, 404, "network not found")
		return
	}
	if network.Topology != TopologyCustom {
		writeError(w, 409, "manual peers require custom topology")
		return
	}
	var in struct {
		SourceNodeID string `json:"source_node_id"`
		TargetNodeID string `json:"target_node_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	source, e1 := a.store.GetNode(c.TenantID, in.SourceNodeID)
	target, e2 := a.store.GetNode(c.TenantID, in.TargetNodeID)
	if e1 != nil || e2 != nil || source.NetworkID != networkID || target.NetworkID != networkID || source.ID == target.ID {
		writeError(w, 400, "invalid peer relationship")
		return
	}
	v := PeerRelation{ID: newID("peer"), TenantID: c.TenantID, NetworkID: networkID, SourceNodeID: source.ID, TargetNodeID: target.ID, CreatedAt: time.Now()}
	if err := a.store.AddPeer(v); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create peer")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "peer.create", "network", networkID, nil)
	writeJSON(w, 201, v)
}

func (a *App) publish(w http.ResponseWriter, r *http.Request, c claims) {
	network, err := a.store.GetNetwork(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, 404, "network not found")
		return
	}
	result, err := a.publishNetwork(c.TenantID, network)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	action := "config.publish"
	if result.Unchanged {
		action = "config.publish.noop"
	}
	a.auditEvent(c.TenantID, c.Subject, action, "network", network.ID, map[string]string{
		"version":       fmt.Sprint(result.Version),
		"changed_nodes": fmt.Sprint(len(result.ChangedNodeIDs)),
		"offline_nodes": fmt.Sprint(len(result.OfflineNodeIDs)),
	})
	status := http.StatusCreated
	if result.Unchanged {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func (a *App) publishNetwork(tenantID string, network Network) (ConfigPublishResult, error) {
	allNodes := a.store.ListNodes(tenantID, network.ID)
	nodes := make([]Node, 0, len(allNodes))
	for _, node := range allNodes {
		if node.Enabled {
			nodes = append(nodes, node)
		}
	}
	if len(nodes) == 0 {
		return ConfigPublishResult{}, fmt.Errorf("network has no enabled nodes")
	}
	configs, err := CompileTopology(network, nodes, a.store.ListPeers(tenantID, network.ID), a.box)
	if err != nil {
		return ConfigPublishResult{}, err
	}
	previous, previousErr := a.store.LatestRevision(tenantID, network.ID)
	if previousErr != nil && !errors.Is(previousErr, errNotFound) {
		return ConfigPublishResult{}, fmt.Errorf("read latest configuration revision: %w", previousErr)
	}
	deliveryTargets := make(map[string]bool, len(nodes))
	changedNodeIDs := make([]string, 0, len(nodes))
	queuedNodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if errors.Is(previousErr, errNotFound) || !reflect.DeepEqual(previous.Configs[node.ID], configs[node.ID]) {
			deliveryTargets[node.ID] = true
			changedNodeIDs = append(changedNodeIDs, node.ID)
			queuedNodeIDs = append(queuedNodeIDs, node.ID)
		} else if !a.nodeHasAppliedConfigVersion(tenantID, node.ID, previous.Version) {
			deliveryTargets[node.ID] = true
			queuedNodeIDs = append(queuedNodeIDs, node.ID)
		}
	}

	revision := previous
	if len(changedNodeIDs) > 0 {
		revision = ConfigRevision{ID: newID("rev"), TenantID: tenantID, ProjectID: network.ProjectID, NetworkID: network.ID, Version: previous.Version + 1, Configs: configs, CreatedAt: time.Now()}
		if errors.Is(previousErr, errNotFound) {
			revision.Version = 1
		}
		if err := a.store.CreateRevision(revision); err != nil {
			return ConfigPublishResult{}, fmt.Errorf("create configuration revision: %w", err)
		}
	}
	if len(changedNodeIDs) == 0 && len(queuedNodeIDs) == 0 {
		return ConfigPublishResult{
			RevisionID: previous.ID, NetworkID: network.ID, Version: previous.Version,
			ChangedNodeIDs: []string{}, QueuedNodeIDs: []string{}, OfflineNodeIDs: []string{}, Unchanged: true,
		}, nil
	}
	settings, settingsErr := a.tenantSettings(tenantID)
	if settingsErr != nil {
		return ConfigPublishResult{}, fmt.Errorf("read delivery settings: %w", settingsErr)
	}
	offlineAfter := time.Duration(settings.StatusRules.AgentOfflineSec) * time.Second
	result := ConfigPublishResult{
		RevisionID: revision.ID, NetworkID: network.ID, Version: revision.Version,
		ChangedNodeIDs: changedNodeIDs, QueuedNodeIDs: make([]string, 0, len(queuedNodeIDs)), OfflineNodeIDs: []string{},
		Unchanged: len(changedNodeIDs) == 0,
	}
	commands := make([]AgentCommand, 0, len(queuedNodeIDs))
	for _, node := range nodes {
		if !deliveryTargets[node.ID] {
			continue
		}
		if !a.nodeHasConfigDelivery(tenantID, node.ID, revision.Version) {
			if err := a.store.CreateDelivery(ConfigDelivery{ID: newID("delivery"), TenantID: tenantID, NodeID: node.ID, Version: revision.Version, State: "pending", UpdatedAt: time.Now()}); err != nil {
				return ConfigPublishResult{}, fmt.Errorf("create configuration delivery: %w", err)
			}
		}
		commands = append(commands, AgentCommand{ID: newID("cmd"), TenantID: tenantID, NodeID: node.ID, Type: "apply_config", State: "pending", CreatedAt: time.Now()})
		result.QueuedNodeIDs = append(result.QueuedNodeIDs, node.ID)
		if node.LastSeen.IsZero() || time.Since(node.LastSeen) > offlineAfter {
			result.OfflineNodeIDs = append(result.OfflineNodeIDs, node.ID)
		}
	}
	if err := a.createAgentCommandsParallel(commands); err != nil {
		return ConfigPublishResult{}, fmt.Errorf("queue configuration delivery: %w", err)
	}
	return result, nil
}

func (a *App) nodeHasAppliedConfigVersion(tenantID, nodeID string, version uint64) bool {
	for _, delivery := range a.store.ListDeliveries(tenantID, nodeID) {
		if delivery.Version == version && delivery.State == "applied" {
			return true
		}
	}
	return false
}

func (a *App) nodeHasConfigDelivery(tenantID, nodeID string, version uint64) bool {
	for _, delivery := range a.store.ListDeliveries(tenantID, nodeID) {
		if delivery.Version == version {
			return true
		}
	}
	return false
}

func (a *App) nodeHasOpenConfigDelivery(tenantID, nodeID string, version uint64) bool {
	for _, delivery := range a.store.ListDeliveries(tenantID, nodeID) {
		if delivery.Version != version {
			continue
		}
		return delivery.State == "pending" || delivery.State == "failed" || delivery.State == "rolled_back"
	}
	return false
}
func (a *App) deliveries(w http.ResponseWriter, r *http.Request, c claims) {
	writeJSON(w, 200, a.store.ListDeliveries(c.TenantID, r.URL.Query().Get("node_id")))
}
func (a *App) audit(w http.ResponseWriter, r *http.Request, c claims) {
	limit, offset := parseLogPage(r)
	items := a.store.ListAuditPage(c.TenantID, limit+1, offset)
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	if items == nil {
		items = []AuditEvent{}
	}
	writeJSON(w, http.StatusOK, AuditLogPage{Items: items, Limit: limit, Offset: offset, HasMore: hasMore})
}

func (a *App) clearAudit(w http.ResponseWriter, r *http.Request, c claims) {
	if err := a.store.ClearAudit(c.TenantID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear audit logs")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) createEnrollment(w http.ResponseWriter, r *http.Request, c claims) {
	var in struct {
		ProjectID  string `json:"project_id"`
		NetworkID  string `json:"network_id"`
		TTLMinutes int    `json:"ttl_minutes"`
	}
	if !decode(w, r, &in) {
		return
	}
	network, err := a.store.GetNetwork(c.TenantID, in.NetworkID)
	if err != nil || network.ProjectID != in.ProjectID {
		writeError(w, 400, "network does not belong to project")
		return
	}
	if in.TTLMinutes <= 0 || in.TTLMinutes > 1440 {
		in.TTLMinutes = 30
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes(32))
	v := EnrollmentToken{ID: newID("enroll"), TenantID: c.TenantID, ProjectID: in.ProjectID, NetworkID: in.NetworkID, Token: token, ExpiresAt: time.Now().Add(time.Duration(in.TTLMinutes) * time.Minute)}
	if err := a.store.CreateEnrollment(v); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create enrollment token")
		return
	}
	a.auditEvent(c.TenantID, c.Subject, "agent.enrollment_token.create", "network", in.NetworkID, nil)
	writeJSON(w, 201, map[string]any{"token": token, "expires_at": v.ExpiresAt, "network_id": in.NetworkID})
}

func (a *App) enroll(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token        string            `json:"token"`
		Name         string            `json:"name"`
		Endpoint     string            `json:"endpoint"`
		Region       string            `json:"region"`
		OS           string            `json:"os"`
		AgentVersion string            `json:"agent_version"`
		Labels       map[string]string `json:"labels"`
	}
	if !decode(w, r, &in) {
		return
	}
	enrollment, err := a.store.ConsumeEnrollment(in.Token)
	if err != nil {
		writeError(w, 401, "invalid or expired enrollment token")
		return
	}
	network, err := a.store.GetNetwork(enrollment.TenantID, enrollment.NetworkID)
	if err != nil {
		writeError(w, 404, "network not found")
		return
	}
	node, err := a.createNode(enrollment.TenantID, network, in.Name, in.Endpoint, in.Region, in.OS, in.AgentVersion, in.Labels)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	cert, key, fingerprint, expires, err := a.issueAgentCertificate(node.ID)
	if err != nil {
		writeError(w, 500, "failed to issue agent certificate")
		return
	}
	if err := a.store.CreateIdentity(AgentIdentity{NodeID: node.ID, CertificatePEM: cert, CertificateFingerprint: fingerprint, ExpiresAt: expires}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist agent identity")
		return
	}
	a.auditEvent(enrollment.TenantID, node.ID, "agent.enroll", "node", node.ID, nil)
	writeJSON(w, 201, map[string]any{"node": node, "certificate_pem": cert, "private_key_pem": key, "certificate_fingerprint": fingerprint, "expires_at": expires, "ca_pem": a.caPEM()})
}

// agentNode authorizes an agent by its enrolled mTLS certificate when it reaches
// the server directly. X-Agent-ID also supports local HTTP and TLS-terminating
// reverse proxies; proxy deployments must keep the backend listener private.
func (a *App) agentNode(w http.ResponseWriter, r *http.Request) (Node, bool) {
	nodeID := r.Header.Get("X-Agent-ID")
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		certificateNodeID := r.TLS.PeerCertificates[0].Subject.CommonName
		if nodeID != "" && nodeID != certificateNodeID {
			writeError(w, http.StatusUnauthorized, "agent identity mismatch")
			return Node{}, false
		}
		nodeID = certificateNodeID
	}
	if nodeID == "" {
		writeError(w, 401, "missing agent identity")
		return Node{}, false
	}
	if node, err := a.store.GetNodeByID(nodeID); err == nil {
		return node, true
	}
	writeError(w, 401, "unknown agent identity")
	return Node{}, false
}

// AgentTLSConfig verifies enrolled client certificates while allowing browsers
// to call the user-facing API on the same HTTPS listener without a certificate.
func (a *App) AgentTLSConfig() *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(a.ca)
	return &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: pool}
}
func (a *App) agentConfig(w http.ResponseWriter, r *http.Request) {
	node, ok := a.agentNode(w, r)
	if !ok {
		return
	}
	if !node.Enabled {
		writeError(w, http.StatusLocked, "node is disabled")
		return
	}
	revision, err := a.store.LatestRevision(node.TenantID, node.NetworkID)
	if err != nil {
		writeError(w, 404, "no published configuration")
		return
	}
	config, ok := revision.Configs[node.ID]
	if !ok {
		writeError(w, 404, "node not included in published configuration")
		return
	}
	if !a.nodeHasOpenConfigDelivery(node.TenantID, node.ID, revision.Version) {
		writeError(w, 404, "no pending configuration for this node")
		return
	}
	a.auditEvent(node.TenantID, node.ID, "agent.config.read", "node", node.ID, map[string]string{"version": fmt.Sprint(revision.Version)})
	writeJSON(w, 200, map[string]any{"version": revision.Version, "config": config})
}
func (a *App) agentStatus(w http.ResponseWriter, r *http.Request) {
	node, ok := a.agentNode(w, r)
	if !ok {
		return
	}
	var in struct {
		Version        uint64
		State, Message string
	}
	if !decode(w, r, &in) {
		return
	}
	if in.State != "applied" && in.State != "failed" && in.State != "rolled_back" {
		writeError(w, 400, "invalid delivery state")
		return
	}
	node.LastSeen = time.Now()
	a.store.UpdateNode(node)
	delivery := ConfigDelivery{ID: newID("delivery"), TenantID: node.TenantID, NodeID: node.ID, Version: in.Version, State: in.State, Message: in.Message, UpdatedAt: time.Now()}
	a.store.UpdateDelivery(delivery)
	a.auditEvent(node.TenantID, node.ID, "agent.config."+in.State, "node", node.ID, map[string]string{"version": fmt.Sprint(in.Version)})
	writeJSON(w, 200, map[string]string{"status": "recorded"})
}

func (a *App) agentHeartbeat(w http.ResponseWriter, r *http.Request) {
	node, ok := a.agentNode(w, r)
	if !ok {
		return
	}
	var in struct {
		Hostname        string                     `json:"hostname"`
		OS              string                     `json:"os"`
		AgentVersion    string                     `json:"agent_version"`
		Labels          map[string]string          `json:"labels"`
		Interfaces      string                     `json:"interfaces"`
		WireGuard       []WireGuardInterfaceStatus `json:"wireguard"`
		PeerConfigs     *[]PeerConfigFile          `json:"peer_configs,omitempty"`
		CollectionError string                     `json:"collection_error,omitempty"`
		Location        geoIPLocation              `json:"location,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	node.LastSeen = time.Now()
	if strings.TrimSpace(in.Hostname) != "" {
		node.Hostname = strings.TrimSpace(in.Hostname)
	}
	node.InterfaceSelector = strings.TrimSpace(in.Interfaces)
	node.CollectionError = strings.TrimSpace(in.CollectionError)
	if strings.TrimSpace(in.OS) != "" {
		node.OS = strings.TrimSpace(in.OS)
	}
	if strings.TrimSpace(in.AgentVersion) != "" {
		node.AgentVersion = strings.TrimSpace(in.AgentVersion)
	}
	if in.Labels != nil {
		node.Labels = in.Labels
	}
	adoptedInterface := ""
	adoptedConfiguration := false
	if in.WireGuard != nil {
		node.WireGuard = in.WireGuard
		a.geoLocatePeerEndpoints(node.TenantID, node.WireGuard)
		adoptedInterface, adoptedConfiguration = a.adoptReportedNodeConfiguration(&node)
	}
	if in.PeerConfigs != nil {
		node.PeerConfigFiles = sanitizeAgentPeerConfigFiles(*in.PeerConfigs, node.LastSeen)
	}
	a.applyAutomaticNodeLocation(&node, in.Location, r)
	a.adoptPublicEndpoint(&node, in.Location, r)
	if err := a.store.UpdateNode(node); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record agent heartbeat")
		return
	}
	if adoptedConfiguration {
		a.auditEvent(node.TenantID, node.ID, "agent.config.observed", "node", node.ID, map[string]string{
			"address": node.Address, "interface": adoptedInterface, "listen_port": fmt.Sprint(node.ListenPort), "mtu": fmt.Sprint(node.MTU),
		})
	}
	samples := make([]TrafficSample, 0, len(node.WireGuard))
	for _, iface := range node.WireGuard {
		var rx, tx int64
		for _, peer := range iface.Peers {
			rx += peer.ReceiveBytes
			tx += peer.TransmitBytes
		}
		samples = append(samples, TrafficSample{ID: newID("traffic"), TenantID: node.TenantID, NodeID: node.ID, InterfaceName: iface.Name, ReceiveBytes: rx, TransmitBytes: tx, RecordedAt: node.LastSeen})
	}
	if err := a.store.AddTrafficSamples(samples); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record traffic samples")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "recorded", "server_time": node.LastSeen})
}

func (a *App) createNode(tenantID string, network Network, name, endpoint, region, os, agentVersion string, labels map[string]string) (Node, error) {
	if strings.TrimSpace(name) == "" {
		return Node{}, errors.New("node name is required")
	}
	if labels == nil {
		labels = map[string]string{}
	}
	existing := a.store.ListNodes(tenantID, network.ID)
	allocated := make([]string, 0, len(existing))
	for _, node := range existing {
		allocated = append(allocated, node.Address)
	}
	address, err := AllocateAddress(network.CIDR, allocated)
	if err != nil {
		return Node{}, err
	}
	curve := ecdh.X25519()
	private, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return Node{}, err
	}
	privateText := base64.StdEncoding.EncodeToString(private.Bytes())
	secret, err := a.box.Encrypt([]byte(privateText))
	if err != nil {
		return Node{}, err
	}
	node := Node{ID: newID("node"), TenantID: tenantID, ProjectID: network.ProjectID, NetworkID: network.ID, Name: name, Enabled: true, ListenPort: defaultNodeListenPort, MTU: defaultNodeMTU, Address: address, Endpoint: endpoint, Region: region, OS: os, AgentVersion: agentVersion, Labels: labels, PublicKey: base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()), PrivateKey: secret, WireGuard: []WireGuardInterfaceStatus{}, CreatedAt: time.Now()}
	if err := a.store.CreateNode(node); err != nil {
		return Node{}, err
	}
	return node, nil
}

func (a *App) newCertificateAuthority() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "WireMesh Agent CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(5, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	a.ca = cert
	a.caKey = key
	return nil
}
func (a *App) issueAgentCertificate(nodeID string) (string, string, string, time.Time, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	expires := time.Now().AddDate(1, 0, 0)
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: nodeID}, NotBefore: time.Now().Add(-time.Minute), NotAfter: expires, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, a.ca, &key.PublicKey, a.caKey)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	privateDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	fingerprint := sha256.Sum256(der)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateDER})), hex.EncodeToString(fingerprint[:]), expires, nil
}
func (a *App) caPEM() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.ca.Raw}))
}
func (a *App) auditEvent(tenant, actor, action, resourceType, resourceID string, metadata map[string]string) {
	a.store.AddAudit(AuditEvent{ID: newID("audit"), TenantID: tenant, ActorID: actor, Action: action, ResourceType: resourceType, ResourceID: resourceID, Metadata: metadata, CreatedAt: time.Now()})
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeError(w, 400, "invalid JSON request")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	value := reflect.ValueOf(v)
	if value.IsValid() && value.Kind() == reflect.Slice && value.IsNil() {
		v = reflect.MakeSlice(value.Type(), 0, 0).Interface()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func newID(prefix string) string {
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(randomBytes(12))
}
func randomBytes(n int) []byte { v := make([]byte, n); _, _ = rand.Read(v); return v }
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Agent-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
