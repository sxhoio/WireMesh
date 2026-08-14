package control

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	errNotFound           = errors.New("resource not found")
	errAlreadyInitialized = errors.New("instance already initialized")
	errAddressConflict    = errors.New("node address is already used in this network")
)

// Store is the control-plane persistence boundary. MemoryStore is intentionally
// development-only; a PostgreSQL implementation can preserve this API.
type Store interface {
	CreateProject(Project) error
	ListProjects(string) ([]Project, error)
	GetProject(string, string) (Project, error)
	CreateNetwork(Network) error
	ListNetworks(string, string) ([]Network, error)
	GetNetwork(string, string) (Network, error)
	CreateNode(Node) error
	GetNode(string, string) (Node, error)
	GetNodeByID(string) (Node, error)
	ListNodes(string, string) ([]Node, error)
	ListNodeRefs(string, string) ([]Node, error)
	UpdateNode(Node) error
	DeleteNode(string, string) error
	AddTrafficSamples([]TrafficSample) error
	ListTrafficSamples(string, string, string, time.Time) ([]TrafficSample, error)
	AddPeer(PeerRelation) error
	ListPeers(string, string) ([]PeerRelation, error)
	CreateRevision(ConfigRevision) error
	LatestRevision(string, string) (ConfigRevision, error)
	CreateDelivery(ConfigDelivery) error
	UpdateDelivery(ConfigDelivery) error
	ListDeliveries(string, string) ([]ConfigDelivery, error)
	CreateCommand(AgentCommand) error
	ClaimCommands(string) []AgentCommand
	UpdateCommand(AgentCommand) error
	GetCommand(string) (AgentCommand, error)
	ListCommands(string, string) ([]AgentCommand, error)
	ListCommandsPage(string, string, int, int, bool) ([]AgentCommand, error)
	ClearCommands(string, string) error
	CreateEnrollment(EnrollmentToken) error
	ConsumeEnrollment(string) (EnrollmentToken, error)
	CreateIdentity(AgentIdentity) error
	GetIdentity(string) (AgentIdentity, error)
	AddAudit(AuditEvent) error
	ListAudit(string) ([]AuditEvent, error)
	HasNodeAuditAction(string, string, ...string) (bool, error)
	AddRevokedToken(RevokedToken) error
	ListRevokedTokens() ([]RevokedToken, error)
	DeleteRevokedTokensBefore(time.Time) error
	GetUserByEmail(string) (User, error)
	GetUser(string) (User, error)
	UpdateUserLastLogin(string, time.Time) error
	UpdateUserPassword(string, string) error
	HasUsers() (bool, error)
	CreateInitialAdmin(User) error
	ListUsers(string) ([]User, error)
	CreateUser(User) error
	UpdateUser(User) error
	DeleteUser(string, string) error
	GetSettings(string) (SystemSettings, error)
	UpsertSettings(SystemSettings) error
	ListNotificationChannels(string) ([]NotificationChannel, error)
	GetNotificationChannel(string, string) (NotificationChannel, error)
	CreateNotificationChannel(NotificationChannel) error
	UpdateNotificationChannel(NotificationChannel) error
	DeleteNotificationChannel(string, string) error
	AddNotificationLog(NotificationLog) error
	ListNotificationLogs(string) ([]NotificationLog, error)
	ListNotificationLogsPage(string, int, int) ([]NotificationLog, bool, error)
	ListAuditPage(string, int, int) ([]AuditEvent, error)
	ClearAudit(string) error
	CreateAlertRule(AlertRule) error
	UpdateAlertRule(AlertRule) error
	DeleteAlertRule(string, string) error
	ListAlertRules(string) ([]AlertRule, error)
	AllAlertRules() ([]AlertRule, error)
	AddAlertEvent(AlertEvent) error
	ListAlertEvents(string) ([]AlertEvent, error)
	ListAlertEventsPage(string, int, int) ([]AlertEvent, bool, error)
	ClearAlertEvents(string) error
	GetAlertFired(string, string) (AlertFired, error)
	PutAlertFired(AlertFired) error
	CreateAccessResource(AccessResource) error
	UpdateAccessResource(AccessResource) error
	DeleteAccessResource(string, string) error
	ListAccessResources(string, string) ([]AccessResource, error)
	CreateAccessPolicy(AccessPolicy) error
	UpdateAccessPolicy(AccessPolicy) error
	DeleteAccessPolicy(string, string) error
	ListAccessPolicies(string, string) ([]AccessPolicy, error)
	CreateDNSRecord(DNSRecord) error
	UpdateDNSRecord(DNSRecord) error
	DeleteDNSRecord(string, string) error
	ListDNSRecords(string, string) ([]DNSRecord, error)
	CreateAPIToken(APIToken) error
	GetAPITokenByHash(string) (APIToken, error)
	DeleteAPIToken(string, string) error
	DeleteAPITokensByCreator(string, string) error
	ListAPITokens(string) ([]APIToken, error)
	UpdateAPITokenLastUsed(string, time.Time) error
	GetEgressConfig(string, string) (EgressConfig, error)
	UpsertEgressConfig(EgressConfig) error
	CountNodes() (int, error)
	CountUsers() (int, error)
	UpdateUserMFA(string, EncryptedSecret, bool) error
	GetSSOConfig(string) (SSOConfig, error)
	UpsertSSOConfig(SSOConfig) error
	AllSSOConfigs() ([]SSOConfig, error)
}

type MemoryStore struct {
	mu               sync.RWMutex
	projects         map[string]Project
	networks         map[string]Network
	nodes            map[string]Node
	peers            map[string]PeerRelation
	revisions        map[string][]ConfigRevision
	deliveries       map[string]ConfigDelivery
	commands         map[string]AgentCommand
	enrollments      map[string]EnrollmentToken
	identities       map[string]AgentIdentity
	audits           []AuditEvent
	revokedTokens    map[string]RevokedToken
	users            map[string]User
	settings         map[string]SystemSettings
	notifications    map[string]NotificationChannel
	notificationLogs []NotificationLog
	trafficSamples   []TrafficSample
	alertRules       map[string]AlertRule
	alertEvents      []AlertEvent
	alertFired       map[string]AlertFired
	accessResources  map[string]AccessResource
	accessPolicies   map[string]AccessPolicy
	dnsRecords       map[string]DNSRecord
	apiTokens        map[string]APIToken
	apiTokenByHash   map[string]string
	egressConfigs    map[string]EgressConfig
	ssoConfigs       map[string]SSOConfig
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		projects: map[string]Project{}, networks: map[string]Network{}, nodes: map[string]Node{}, peers: map[string]PeerRelation{}, revisions: map[string][]ConfigRevision{}, deliveries: map[string]ConfigDelivery{}, commands: map[string]AgentCommand{}, enrollments: map[string]EnrollmentToken{}, identities: map[string]AgentIdentity{}, revokedTokens: map[string]RevokedToken{}, users: map[string]User{}, settings: map[string]SystemSettings{}, notifications: map[string]NotificationChannel{}, alertRules: map[string]AlertRule{}, alertFired: map[string]AlertFired{}, accessResources: map[string]AccessResource{}, accessPolicies: map[string]AccessPolicy{}, dnsRecords: map[string]DNSRecord{}, apiTokens: map[string]APIToken{}, apiTokenByHash: map[string]string{}, egressConfigs: map[string]EgressConfig{}, ssoConfigs: map[string]SSOConfig{},
	}
}
func (s *MemoryStore) CreateProject(v Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects[v.ID] = v
	return nil
}
func (s *MemoryStore) ListProjects(t string) ([]Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Project, 0)
	for _, v := range s.projects {
		if v.TenantID == t {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (s *MemoryStore) GetProject(t, id string) (Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.projects[id]
	if !ok || v.TenantID != t {
		return Project{}, errNotFound
	}
	return v, nil
}
func (s *MemoryStore) CreateNetwork(v Network) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.networks[v.ID] = v
	return nil
}
func (s *MemoryStore) ListNetworks(t, p string) ([]Network, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Network, 0)
	for _, v := range s.networks {
		if v.TenantID == t && (p == "" || v.ProjectID == p) {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *MemoryStore) GetNetwork(t, id string) (Network, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.networks[id]
	if !ok || v.TenantID != t {
		return Network{}, errNotFound
	}
	return v, nil
}
func (s *MemoryStore) CreateNode(v Node) error {
	v = normalizeNodeDefaults(v)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.nodes {
		if existing.NetworkID == v.NetworkID && existing.Address == v.Address && existing.ID != v.ID {
			return errAddressConflict
		}
	}
	s.nodes[v.ID] = v
	return nil
}
func (s *MemoryStore) GetNode(t, id string) (Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.nodes[id]
	if !ok || v.TenantID != t {
		return Node{}, errNotFound
	}
	return v, nil
}
func (s *MemoryStore) GetNodeByID(id string) (Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.nodes[id]
	if !ok {
		return Node{}, errNotFound
	}
	return v, nil
}
func (s *MemoryStore) ListNodes(t, n string) ([]Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Node, 0)
	for _, v := range s.nodes {
		if v.TenantID == t && (n == "" || v.NetworkID == n) {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *MemoryStore) ListNodeRefs(t, n string) ([]Node, error) {
	return s.ListNodes(t, n)
}
func (s *MemoryStore) UpdateNode(v Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.nodes[v.ID]; !ok {
		return errNotFound
	}
	s.nodes[v.ID] = v
	return nil
}
func (s *MemoryStore) DeleteNode(tenant, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.nodes[id]
	if !ok || v.TenantID != tenant {
		return errNotFound
	}
	delete(s.nodes, id)
	delete(s.identities, id)
	for key, peer := range s.peers {
		if peer.TenantID == tenant && (peer.SourceNodeID == id || peer.TargetNodeID == id) {
			delete(s.peers, key)
		}
	}
	for key, delivery := range s.deliveries {
		if delivery.TenantID == tenant && delivery.NodeID == id {
			delete(s.deliveries, key)
		}
	}
	s.trafficSamples = slices.DeleteFunc(s.trafficSamples, func(sample TrafficSample) bool { return sample.TenantID == tenant && sample.NodeID == id })
	for key, command := range s.commands {
		if command.TenantID == tenant && command.NodeID == id {
			delete(s.commands, key)
		}
	}
	return nil
}

func (s *MemoryStore) AddTrafficSamples(samples []TrafficSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trafficSamples = append(s.trafficSamples, samples...)
	return nil
}
func (s *MemoryStore) ListTrafficSamples(tenant, node, interfaceName string, since time.Time) ([]TrafficSample, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TrafficSample, 0)
	for _, sample := range s.trafficSamples {
		if sample.TenantID == tenant && sample.NodeID == node && sample.InterfaceName == interfaceName && !sample.RecordedAt.Before(since) {
			out = append(out, sample)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RecordedAt.Before(out[j].RecordedAt) })
	return out, nil
}

func (s *MemoryStore) AddPeer(v PeerRelation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers[v.ID] = v
	return nil
}
func (s *MemoryStore) ListPeers(t, n string) ([]PeerRelation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PeerRelation, 0)
	for _, v := range s.peers {
		if v.TenantID == t && v.NetworkID == n {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *MemoryStore) CreateRevision(v ConfigRevision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revisions[v.NetworkID] = append(s.revisions[v.NetworkID], v)
	return nil
}
func (s *MemoryStore) LatestRevision(t, n string) (ConfigRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := s.revisions[n]
	if len(items) == 0 {
		return ConfigRevision{}, errNotFound
	}
	v := items[len(items)-1]
	if v.TenantID != t {
		return ConfigRevision{}, errNotFound
	}
	return v, nil
}
func (s *MemoryStore) CreateDelivery(v ConfigDelivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveries[v.ID] = v
	return nil
}
func (s *MemoryStore) UpdateDelivery(v ConfigDelivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, current := range s.deliveries {
		if current.TenantID == v.TenantID && current.NodeID == v.NodeID && current.Version == v.Version {
			v.ID = id
			break
		}
	}
	s.deliveries[v.ID] = v
	return nil
}
func (s *MemoryStore) ListDeliveries(t, n string) ([]ConfigDelivery, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ConfigDelivery, 0)
	for _, v := range s.deliveries {
		if v.TenantID == t && (n == "" || v.NodeID == n) {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *MemoryStore) CreateCommand(v AgentCommand) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands[v.ID] = v
	s.pruneCommandsLocked(v.TenantID, v.NodeID)
	return nil
}
func (s *MemoryStore) ClaimCommands(node string) (out []AgentCommand) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, command := range s.commands {
		if command.NodeID == node && command.State == "pending" {
			command.State = "running"
			command.StartedAt = &now
			s.commands[id] = command
			out = append(out, command)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return
}
func (s *MemoryStore) UpdateCommand(v AgentCommand) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.commands[v.ID]; !ok {
		return errNotFound
	}
	s.commands[v.ID] = v
	return nil
}
func (s *MemoryStore) GetCommand(id string) (AgentCommand, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	command, ok := s.commands[id]
	if !ok {
		return AgentCommand{}, errNotFound
	}
	return command, nil
}
func (s *MemoryStore) ListCommands(tenant, node string) ([]AgentCommand, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AgentCommand, 0)
	for _, command := range s.commands {
		if command.TenantID == tenant && (node == "" || command.NodeID == node) {
			out = append(out, command)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) ListCommandsPage(tenant, node string, limit, offset int, errorsOnly bool) ([]AgentCommand, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AgentCommand, 0)
	for _, v := range s.commands {
		if v.TenantID != tenant || (node != "" && v.NodeID != node) {
			continue
		}
		if errorsOnly && v.State != "failed" {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return pageSlice(out, limit, offset), nil
}

func (s *MemoryStore) ClearCommands(tenant, node string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, command := range s.commands {
		if command.TenantID == tenant && command.NodeID == node {
			delete(s.commands, id)
		}
	}
	return nil
}

func (s *MemoryStore) CreateEnrollment(v EnrollmentToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enrollments[v.Token] = v
	return nil
}
func (s *MemoryStore) ConsumeEnrollment(token string) (EnrollmentToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.enrollments[token]
	if !ok || v.UsedAt != nil || time.Now().After(v.ExpiresAt) {
		return EnrollmentToken{}, errNotFound
	}
	now := time.Now()
	v.UsedAt = &now
	s.enrollments[token] = v
	return v, nil
}
func (s *MemoryStore) CreateIdentity(v AgentIdentity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identities[v.NodeID] = v
	return nil
}
func (s *MemoryStore) GetIdentity(n string) (AgentIdentity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.identities[n]
	if !ok {
		return AgentIdentity{}, errNotFound
	}
	return v, nil
}
func (s *MemoryStore) AddAudit(v AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audits = append(s.audits, v)
	s.pruneAuditLocked(v.TenantID)
	return nil
}
func (s *MemoryStore) ListAudit(t string) ([]AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AuditEvent, 0)
	for _, v := range s.audits {
		if v.TenantID == t {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) HasNodeAuditAction(tenant, nodeID string, actions ...string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	allowed := make(map[string]bool, len(actions))
	for _, action := range actions {
		allowed[action] = true
	}
	for _, event := range s.audits {
		if event.TenantID == tenant && event.ResourceType == "node" && event.ResourceID == nodeID && allowed[event.Action] {
			return true, nil
		}
	}
	return false, nil
}

func (s *MemoryStore) ListAuditPage(tenant string, limit, offset int) ([]AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AuditEvent, 0)
	for _, v := range s.audits {
		if v.TenantID == tenant {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return pageSlice(out, limit, offset), nil
}

func pageSlice[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return []T{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func (s *MemoryStore) ClearAudit(tenant string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audits = slices.DeleteFunc(s.audits, func(event AuditEvent) bool {
		return event.TenantID == tenant
	})
	return nil
}

func (s *MemoryStore) AddRevokedToken(v RevokedToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revokedTokens[v.TokenHash] = v
	return nil
}

func (s *MemoryStore) ListRevokedTokens() ([]RevokedToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RevokedToken, 0, len(s.revokedTokens))
	for _, v := range s.revokedTokens {
		out = append(out, v)
	}
	return out, nil
}

func (s *MemoryStore) DeleteRevokedTokensBefore(at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash, v := range s.revokedTokens {
		if v.RevokedAt.Before(at) {
			delete(s.revokedTokens, hash)
		}
	}
	return nil
}

func (s *MemoryStore) CreateAlertRule(v AlertRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alertRules[v.ID] = v
	return nil
}
func (s *MemoryStore) UpdateAlertRule(v AlertRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.alertRules[v.ID]
	if !ok || current.TenantID != v.TenantID {
		return errNotFound
	}
	s.alertRules[v.ID] = v
	return nil
}
func (s *MemoryStore) DeleteAlertRule(tenant, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.alertRules[id]
	if !ok || v.TenantID != tenant {
		return errNotFound
	}
	delete(s.alertRules, id)
	return nil
}
func (s *MemoryStore) ListAlertRules(tenant string) ([]AlertRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AlertRule, 0)
	for _, v := range s.alertRules {
		if v.TenantID == tenant {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (s *MemoryStore) AllAlertRules() ([]AlertRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AlertRule, 0, len(s.alertRules))
	for _, v := range s.alertRules {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (s *MemoryStore) AddAlertEvent(v AlertEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alertEvents = append(s.alertEvents, v)
	return nil
}
func (s *MemoryStore) ListAlertEvents(tenant string) ([]AlertEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AlertEvent, 0)
	for _, v := range s.alertEvents {
		if v.TenantID == tenant {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) ClearAlertEvents(tenant string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alertEvents = slices.DeleteFunc(s.alertEvents, func(event AlertEvent) bool {
		return event.TenantID == tenant
	})
	return nil
}

func (s *MemoryStore) ListAlertEventsPage(tenant string, limit, offset int) ([]AlertEvent, bool, error) {
	items, err := s.ListAlertEvents(tenant)
	if err != nil {
		return nil, false, err
	}
	page := pageSlice(items, limit, offset)
	return page, offset+len(page) < len(items), nil
}

func (s *MemoryStore) GetAlertFired(tenant, key string) (AlertFired, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.alertFired[tenant+"\x00"+key]
	if !ok {
		return AlertFired{}, errNotFound
	}
	return v, nil
}

func (s *MemoryStore) PutAlertFired(v AlertFired) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alertFired[v.TenantID+"\x00"+v.AlertKey] = v
	return nil
}

func (s *MemoryStore) CreateAccessResource(v AccessResource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accessResources[v.ID] = v
	return nil
}
func (s *MemoryStore) UpdateAccessResource(v AccessResource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.accessResources[v.ID]
	if !ok || current.TenantID != v.TenantID {
		return errNotFound
	}
	s.accessResources[v.ID] = v
	return nil
}
func (s *MemoryStore) DeleteAccessResource(tenant, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.accessResources[id]
	if !ok || v.TenantID != tenant {
		return errNotFound
	}
	delete(s.accessResources, id)
	return nil
}
func (s *MemoryStore) ListAccessResources(tenant, network string) ([]AccessResource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AccessResource, 0)
	for _, v := range s.accessResources {
		if v.TenantID == tenant && v.NetworkID == network {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (s *MemoryStore) CreateAccessPolicy(v AccessPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accessPolicies[v.ID] = v
	return nil
}
func (s *MemoryStore) UpdateAccessPolicy(v AccessPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.accessPolicies[v.ID]
	if !ok || current.TenantID != v.TenantID {
		return errNotFound
	}
	s.accessPolicies[v.ID] = v
	return nil
}
func (s *MemoryStore) DeleteAccessPolicy(tenant, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.accessPolicies[id]
	if !ok || v.TenantID != tenant {
		return errNotFound
	}
	delete(s.accessPolicies, id)
	return nil
}
func (s *MemoryStore) ListAccessPolicies(tenant, network string) ([]AccessPolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AccessPolicy, 0)
	for _, v := range s.accessPolicies {
		if v.TenantID == tenant && v.NetworkID == network {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) CreateDNSRecord(v DNSRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.dnsRecords {
		if existing.TenantID == v.TenantID && existing.NetworkID == v.NetworkID && existing.Name == v.Name && existing.ID != v.ID {
			return errors.New("duplicate dns record name")
		}
	}
	s.dnsRecords[v.ID] = v
	return nil
}
func (s *MemoryStore) UpdateDNSRecord(v DNSRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.dnsRecords[v.ID]
	if !ok || current.TenantID != v.TenantID {
		return errNotFound
	}
	for _, existing := range s.dnsRecords {
		if existing.TenantID == v.TenantID && existing.NetworkID == v.NetworkID && existing.Name == v.Name && existing.ID != v.ID {
			return errors.New("duplicate dns record name")
		}
	}
	s.dnsRecords[v.ID] = v
	return nil
}
func (s *MemoryStore) DeleteDNSRecord(tenant, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.dnsRecords[id]
	if !ok || v.TenantID != tenant {
		return errNotFound
	}
	delete(s.dnsRecords, id)
	return nil
}
func (s *MemoryStore) ListDNSRecords(tenant, network string) ([]DNSRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DNSRecord, 0)
	for _, v := range s.dnsRecords {
		if v.TenantID == tenant && v.NetworkID == network {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *MemoryStore) CreateAPIToken(v APIToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiTokens[v.ID] = v
	s.apiTokenByHash[v.TokenHash] = v.ID
	return nil
}
func (s *MemoryStore) GetAPITokenByHash(hash string) (APIToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.apiTokenByHash[hash]
	if !ok {
		return APIToken{}, errNotFound
	}
	return s.apiTokens[id], nil
}
func (s *MemoryStore) DeleteAPIToken(tenant, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.apiTokens[id]
	if !ok || v.TenantID != tenant {
		return errNotFound
	}
	delete(s.apiTokens, id)
	delete(s.apiTokenByHash, v.TokenHash)
	return nil
}
func (s *MemoryStore) DeleteAPITokensByCreator(tenant, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, v := range s.apiTokens {
		if v.TenantID == tenant && v.CreatedBy == userID {
			delete(s.apiTokens, id)
			delete(s.apiTokenByHash, v.TokenHash)
		}
	}
	return nil
}
func (s *MemoryStore) ListAPITokens(tenant string) ([]APIToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]APIToken, 0)
	for _, v := range s.apiTokens {
		if v.TenantID == tenant {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (s *MemoryStore) UpdateAPITokenLastUsed(id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.apiTokens[id]
	if !ok {
		return errNotFound
	}
	v.LastUsedAt = at.UTC()
	s.apiTokens[id] = v
	return nil
}

func (s *MemoryStore) GetEgressConfig(tenant, network string) (EgressConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.egressConfigs[network]
	if !ok || v.TenantID != tenant {
		return EgressConfig{}, errNotFound
	}
	return v, nil
}
func (s *MemoryStore) UpsertEgressConfig(v EgressConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.egressConfigs[v.NetworkID] = v
	return nil
}
func (s *MemoryStore) CountNodes() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.nodes), nil
}
func (s *MemoryStore) CountUsers() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users), nil
}

func (s *MemoryStore) UpdateUserMFA(id string, secret EncryptedSecret, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return errNotFound
	}
	u.TotpSecret = secret
	u.TotpEnabled = enabled
	s.users[id] = u
	return nil
}

func (s *MemoryStore) GetSSOConfig(tenant string) (SSOConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.ssoConfigs[tenant]
	if !ok {
		return SSOConfig{}, errNotFound
	}
	return v, nil
}
func (s *MemoryStore) UpsertSSOConfig(v SSOConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ssoConfigs[v.TenantID] = v
	return nil
}
func (s *MemoryStore) AllSSOConfigs() ([]SSOConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SSOConfig, 0, len(s.ssoConfigs))
	for _, v := range s.ssoConfigs {
		out = append(out, v)
	}
	return out, nil
}

func (s *MemoryStore) pruneCommandsLocked(tenant, node string) {
	items := make([]AgentCommand, 0)
	for _, command := range s.commands {
		if command.TenantID == tenant && command.NodeID == node {
			items = append(items, command)
		}
	}
	if len(items) <= maxAgentLogRecords {
		return
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	for _, command := range items[maxAgentLogRecords:] {
		delete(s.commands, command.ID)
	}
}

func (s *MemoryStore) pruneAuditLocked(tenant string) {
	items := make([]AuditEvent, 0)
	for _, event := range s.audits {
		if event.TenantID == tenant {
			items = append(items, event)
		}
	}
	if len(items) <= maxAuditRecords {
		return
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	keep := make(map[string]struct{}, maxAuditRecords)
	for _, event := range items[:maxAuditRecords] {
		keep[event.ID] = struct{}{}
	}
	s.audits = slices.DeleteFunc(s.audits, func(event AuditEvent) bool {
		if event.TenantID != tenant {
			return false
		}
		_, ok := keep[event.ID]
		return !ok
	})
}
func (s *MemoryStore) GetUserByEmail(email string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return User{}, errNotFound
}
func (s *MemoryStore) GetUser(id string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return User{}, errNotFound
	}
	return u, nil
}
func (s *MemoryStore) UpdateUserLastLogin(id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return errNotFound
	}
	u.LastLoginAt = at.UTC()
	s.users[id] = u
	return nil
}
func (s *MemoryStore) HasUsers() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users) > 0, nil
}
func (s *MemoryStore) CreateInitialAdmin(user User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.users) != 0 {
		return errAlreadyInitialized
	}
	s.users[user.ID] = user
	return nil
}

func (s *MemoryStore) ListUsers(tenant string) ([]User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, 0)
	for _, v := range s.users {
		if v.TenantID == tenant {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (s *MemoryStore) UpdateUserPassword(id, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.users[id]
	if !ok {
		return errNotFound
	}
	current.PasswordHash = passwordHash
	s.users[id] = current
	return nil
}

func (s *MemoryStore) CreateUser(v User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, current := range s.users {
		if strings.EqualFold(current.Email, v.Email) {
			return errors.New("email already exists")
		}
	}
	s.users[v.ID] = v
	return nil
}
func (s *MemoryStore) UpdateUser(v User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.users[v.ID]
	if !ok || current.TenantID != v.TenantID {
		return errNotFound
	}
	s.users[v.ID] = v
	return nil
}
func (s *MemoryStore) DeleteUser(tenant, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.users[id]
	if !ok || v.TenantID != tenant {
		return errNotFound
	}
	delete(s.users, id)
	return nil
}
func (s *MemoryStore) GetSettings(tenant string) (SystemSettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.settings[tenant]
	if !ok {
		return SystemSettings{}, errNotFound
	}
	return v, nil
}
func (s *MemoryStore) UpsertSettings(v SystemSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings[v.TenantID] = v
	return nil
}
func (s *MemoryStore) ListNotificationChannels(tenant string) ([]NotificationChannel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]NotificationChannel, 0)
	for _, v := range s.notifications {
		if v.TenantID == tenant {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (s *MemoryStore) GetNotificationChannel(tenant, id string) (NotificationChannel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.notifications[id]
	if !ok || v.TenantID != tenant {
		return NotificationChannel{}, errNotFound
	}
	return v, nil
}
func (s *MemoryStore) CreateNotificationChannel(v NotificationChannel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifications[v.ID] = v
	return nil
}
func (s *MemoryStore) UpdateNotificationChannel(v NotificationChannel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.notifications[v.ID]
	if !ok || current.TenantID != v.TenantID {
		return errNotFound
	}
	s.notifications[v.ID] = v
	return nil
}
func (s *MemoryStore) DeleteNotificationChannel(tenant, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.notifications[id]
	if !ok || v.TenantID != tenant {
		return errNotFound
	}
	delete(s.notifications, id)
	return nil
}
func (s *MemoryStore) AddNotificationLog(v NotificationLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notificationLogs = append(s.notificationLogs, v)
	return nil
}
func (s *MemoryStore) ListNotificationLogs(tenant string) ([]NotificationLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]NotificationLog, 0)
	for _, v := range s.notificationLogs {
		if v.TenantID == tenant {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) ListNotificationLogsPage(tenant string, limit, offset int) ([]NotificationLog, bool, error) {
	items, err := s.ListNotificationLogs(tenant)
	if err != nil {
		return nil, false, err
	}
	page := pageSlice(items, limit, offset)
	return page, offset+len(page) < len(items), nil
}
