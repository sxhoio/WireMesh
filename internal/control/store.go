package control

import (
	"errors"
	"sort"
	"sync"
	"time"
)

var errNotFound = errors.New("resource not found")

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
	AddPeer(PeerRelation) error
	ListPeers(string, string) []PeerRelation
	CreateRevision(ConfigRevision) error
	LatestRevision(string, string) (ConfigRevision, error)
	CreateDelivery(ConfigDelivery) error
	UpdateDelivery(ConfigDelivery) error
	ListDeliveries(string, string) []ConfigDelivery
	CreateEnrollment(EnrollmentToken) error
	ConsumeEnrollment(string) (EnrollmentToken, error)
	CreateIdentity(AgentIdentity) error
	GetIdentity(string) (AgentIdentity, error)
	AddAudit(AuditEvent) error
	ListAudit(string) []AuditEvent
	GetUserByEmail(string) (User, error)
	GetUser(string) (User, error)
	EnsureUser(User) error
}

type MemoryStore struct {
	mu          sync.RWMutex
	projects    map[string]Project
	networks    map[string]Network
	nodes       map[string]Node
	peers       map[string]PeerRelation
	revisions   map[string][]ConfigRevision
	deliveries  map[string]ConfigDelivery
	enrollments map[string]EnrollmentToken
	identities  map[string]AgentIdentity
	audits      []AuditEvent
	users       map[string]User
}

func NewMemoryStore(seed User) *MemoryStore {
	store := &MemoryStore{
		projects: map[string]Project{}, networks: map[string]Network{}, nodes: map[string]Node{}, peers: map[string]PeerRelation{}, revisions: map[string][]ConfigRevision{}, deliveries: map[string]ConfigDelivery{}, enrollments: map[string]EnrollmentToken{}, identities: map[string]AgentIdentity{}, users: map[string]User{seed.ID: seed},
	}
	return store
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
		if u.Email == email {
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
func (s *MemoryStore) EnsureUser(user User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.users[user.ID]; !exists {
		s.users[user.ID] = user
	}
	return nil
}
