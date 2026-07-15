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
	"strings"
	"time"
)

type Config struct {
	MasterKey string
	Store     Store
}
type App struct {
	store Store
	box   *SecretBox
	auth  *Authenticator
	ca    *x509.Certificate
	caKey any
}

func NewApp(cfg Config) (*App, error) {
	box, err := NewSecretBox(cfg.MasterKey)
	if err != nil {
		return nil, err
	}
	seed := User{ID: "usr_admin", TenantID: "tenant_demo", Email: "admin@wiremesh.local", Name: "WireMesh Administrator", Role: RoleAdmin, PasswordHash: hashPassword("wiremesh-dev"), CreatedAt: time.Now()}
	store := cfg.Store
	if store == nil {
		store = NewMemoryStore(seed)
	}
	app := &App{store: store, box: box}
	app.auth = newAuthenticator(store, cfg.MasterKey+"-auth")
	if err := app.newCertificateAuthority(); err != nil {
		return nil, err
	}
	return app, nil
}

func (a *App) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("GET /api/v1/auth/me", a.withUser(RoleViewer, a.me))
	mux.HandleFunc("GET /api/v1/projects", a.withUser(RoleViewer, a.projects))
	mux.HandleFunc("POST /api/v1/projects", a.withUser(RoleAdmin, a.projects))
	mux.HandleFunc("GET /api/v1/networks", a.withUser(RoleViewer, a.networks))
	mux.HandleFunc("POST /api/v1/networks", a.withUser(RoleOperator, a.networks))
	mux.HandleFunc("GET /api/v1/nodes", a.withUser(RoleViewer, a.nodes))
	mux.HandleFunc("POST /api/v1/nodes", a.withUser(RoleOperator, a.nodes))
	mux.HandleFunc("POST /api/v1/networks/{id}/peers", a.withUser(RoleOperator, a.addPeer))
	mux.HandleFunc("POST /api/v1/networks/{id}/publish", a.withUser(RoleOperator, a.publish))
	mux.HandleFunc("GET /api/v1/deliveries", a.withUser(RoleViewer, a.deliveries))
	mux.HandleFunc("GET /api/v1/audit", a.withUser(RoleAdmin, a.audit))
	mux.HandleFunc("POST /api/v1/agent/enrollment-tokens", a.withUser(RoleAdmin, a.createEnrollment))
	mux.HandleFunc("POST /agent/v1/enroll", a.enroll)
	mux.HandleFunc("GET /agent/v1/config", a.agentConfig)
	mux.HandleFunc("POST /agent/v1/status", a.agentStatus)
	return cors(mux)
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
		writeError(w, http.StatusUnauthorized, err.Error())
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
	return map[string]any{"id": u.ID, "tenant_id": u.TenantID, "email": u.Email, "name": u.Name, "role": u.Role}
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
	a.store.CreateProject(v)
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
	a.store.CreateNetwork(v)
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
	a.store.AddPeer(v)
	a.auditEvent(c.TenantID, c.Subject, "peer.create", "network", networkID, nil)
	writeJSON(w, 201, v)
}

func (a *App) publish(w http.ResponseWriter, r *http.Request, c claims) {
	network, err := a.store.GetNetwork(c.TenantID, r.PathValue("id"))
	if err != nil {
		writeError(w, 404, "network not found")
		return
	}
	nodes := a.store.ListNodes(c.TenantID, network.ID)
	if len(nodes) == 0 {
		writeError(w, 409, "network has no nodes")
		return
	}
	configs, err := CompileTopology(network, nodes, a.store.ListPeers(c.TenantID, network.ID), a.box)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	previous, _ := a.store.LatestRevision(c.TenantID, network.ID)
	revision := ConfigRevision{ID: newID("rev"), TenantID: c.TenantID, ProjectID: network.ProjectID, NetworkID: network.ID, Version: previous.Version + 1, Configs: configs, CreatedAt: time.Now()}
	a.store.CreateRevision(revision)
	for _, node := range nodes {
		a.store.CreateDelivery(ConfigDelivery{ID: newID("delivery"), TenantID: c.TenantID, NodeID: node.ID, Version: revision.Version, State: "pending", UpdatedAt: time.Now()})
	}
	a.auditEvent(c.TenantID, c.Subject, "config.publish", "network", network.ID, map[string]string{"version": fmt.Sprint(revision.Version)})
	writeJSON(w, 201, revision)
}
func (a *App) deliveries(w http.ResponseWriter, r *http.Request, c claims) {
	writeJSON(w, 200, a.store.ListDeliveries(c.TenantID, r.URL.Query().Get("node_id")))
}
func (a *App) audit(w http.ResponseWriter, r *http.Request, c claims) {
	writeJSON(w, 200, a.store.ListAudit(c.TenantID))
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
	a.store.CreateEnrollment(v)
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
	a.store.CreateIdentity(AgentIdentity{NodeID: node.ID, CertificatePEM: cert, CertificateFingerprint: fingerprint, ExpiresAt: expires})
	a.auditEvent(enrollment.TenantID, node.ID, "agent.enroll", "node", node.ID, nil)
	writeJSON(w, 201, map[string]any{"node": node, "certificate_pem": cert, "private_key_pem": key, "certificate_fingerprint": fingerprint, "expires_at": expires, "ca_pem": a.caPEM()})
}

// agentNode authorizes a mTLS-authenticated agent in production. The X-Agent-ID
// header is a local-development adapter so the included sample agent works over HTTP.
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
	node := Node{ID: newID("node"), TenantID: tenantID, ProjectID: network.ProjectID, NetworkID: network.ID, Name: name, Address: address, Endpoint: endpoint, Region: region, OS: os, AgentVersion: agentVersion, Labels: labels, PublicKey: base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()), PrivateKey: secret, CreatedAt: time.Now()}
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
