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
)

// Store is the control-plane persistence boundary. MemoryStore is intentionally
// development-only; a PostgreSQL implementation can preserve this API.
type Store interface {
	CreateProject(Project) error
	ListProjects(string) []Project
	GetProject(string, string) (Project, error)
	CreateNetwork(Network) error
	ListNetworks(string, string) []Network
	GetNetwork(string, string) (Network, error)
	CreateNode(Node) error
	GetNode(string, string) (Node, error)
	GetNodeByID(string) (Node, error)
	ListNodes(string, string) []Node
	UpdateNode(Node) error
	DeleteNode(string, string) error
	AddTrafficSamples([]TrafficSample) error
	ListTrafficSamples(string, string, string, time.Time) []TrafficSample
	AddPeer(PeerRelation) error
	ListPeers(string, string) []PeerRelation
	CreateRevision(ConfigRevision) error
	LatestRevision(string, string) (ConfigRevision, error)
	CreateDelivery(ConfigDelivery) error
	UpdateDelivery(ConfigDelivery) error
	ListDeliveries(string, string) []ConfigDelivery
	CreateCommand(AgentCommand) error
	ClaimCommands(string) []AgentCommand
	UpdateCommand(AgentCommand) error
	ListCommands(string, string) []AgentCommand
	CreateEnrollment(EnrollmentToken) error
	ConsumeEnrollment(string) (EnrollmentToken, error)
	CreateIdentity(AgentIdentity) error
	GetIdentity(string) (AgentIdentity, error)
	AddAudit(AuditEvent) error
	ListAudit(string) []AuditEvent
	GetUserByEmail(string) (User, error)
	GetUser(string) (User, error)
	UpdateUserLastLogin(string, time.Time) error
	HasUsers() (bool, error)
	CreateInitialAdmin(User) error
	ListUsers(string) []User
	CreateUser(User) error
	GetSettings(string) (SystemSettings, error)
	UpsertSettings(SystemSettings) error
	ListNotificationChannels(string) []NotificationChannel
	GetNotificationChannel(string, string) (NotificationChannel, error)
	CreateNotificationChannel(NotificationChannel) error
	UpdateNotificationChannel(NotificationChannel) error
	DeleteNotificationChannel(string, string) error
	AddNotificationLog(NotificationLog) error
	ListNotificationLogs(string) []NotificationLog
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
	users            map[string]User
	settings         map[string]SystemSettings
	notifications    map[string]NotificationChannel
	notificationLogs []NotificationLog
	trafficSamples   []TrafficSample
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		projects: map[string]Project{}, networks: map[string]Network{}, nodes: map[string]Node{}, peers: map[string]PeerRelation{}, revisions: map[string][]ConfigRevision{}, deliveries: map[string]ConfigDelivery{}, commands: map[string]AgentCommand{}, enrollments: map[string]EnrollmentToken{}, identities: map[string]AgentIdentity{}, users: map[string]User{}, settings: map[string]SystemSettings{}, notifications: map[string]NotificationChannel{},
	}
}
func (s *MemoryStore) CreateProject(v Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects[v.ID] = v
	return nil
}
func (s *MemoryStore) ListProjects(t string) (out []Project) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.projects {
		if v.TenantID == t {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return
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
func (s *MemoryStore) ListNetworks(t, p string) (out []Network) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.networks {
		if v.TenantID == t && v.ProjectID == p {
			out = append(out, v)
		}
	}
	return
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
func (s *MemoryStore) ListNodes(t, n string) (out []Node) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.nodes {
		if v.TenantID == t && (n == "" || v.NetworkID == n) {
			out = append(out, v)
		}
	}
	return
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
func (s *MemoryStore) ListTrafficSamples(tenant, node, interfaceName string, since time.Time) (out []TrafficSample) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sample := range s.trafficSamples {
		if sample.TenantID == tenant && sample.NodeID == node && sample.InterfaceName == interfaceName && !sample.RecordedAt.Before(since) {
			out = append(out, sample)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RecordedAt.Before(out[j].RecordedAt) })
	return
}

func (s *MemoryStore) AddPeer(v PeerRelation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers[v.ID] = v
	return nil
}
func (s *MemoryStore) ListPeers(t, n string) (out []PeerRelation) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.peers {
		if v.TenantID == t && v.NetworkID == n {
			out = append(out, v)
		}
	}
	return
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
func (s *MemoryStore) ListDeliveries(t, n string) (out []ConfigDelivery) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.deliveries {
		if v.TenantID == t && (n == "" || v.NodeID == n) {
			out = append(out, v)
		}
	}
	return
}
func (s *MemoryStore) CreateCommand(v AgentCommand) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands[v.ID] = v
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
func (s *MemoryStore) ListCommands(tenant, node string) (out []AgentCommand) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, command := range s.commands {
		if command.TenantID == tenant && (node == "" || command.NodeID == node) {
			out = append(out, command)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return
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
	return nil
}
func (s *MemoryStore) ListAudit(t string) (out []AuditEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.audits {
		if v.TenantID == t {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return
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

func (s *MemoryStore) ListUsers(tenant string) (out []User) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.users {
		if v.TenantID == tenant {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return
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
func (s *MemoryStore) ListNotificationChannels(tenant string) (out []NotificationChannel) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.notifications {
		if v.TenantID == tenant {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return
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
func (s *MemoryStore) ListNotificationLogs(tenant string) (out []NotificationLog) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.notificationLogs {
		if v.TenantID == tenant {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return
}
