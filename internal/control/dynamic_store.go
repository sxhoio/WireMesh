package control

import (
	"sync/atomic"
	"time"
)

type storeSnapshot struct {
	store Store
}

// SwitchableStore lets the setup flow replace the bootstrap store atomically.
// Each operation captures one immutable snapshot, so in-flight requests keep
// using a consistent store while a newly configured database becomes active.
type SwitchableStore struct {
	current atomic.Pointer[storeSnapshot]
}

func NewSwitchableStore(store Store) *SwitchableStore {
	s := &SwitchableStore{}
	s.Switch(store)
	return s
}

func (s *SwitchableStore) Switch(store Store) {
	if store == nil {
		store = NewMemoryStore()
	}
	s.current.Store(&storeSnapshot{store: store})
}

func (s *SwitchableStore) store() Store { return s.current.Load().store }

func (s *SwitchableStore) CreateProject(v Project) error { return s.store().CreateProject(v) }
func (s *SwitchableStore) ListProjects(tenant string) []Project {
	return s.store().ListProjects(tenant)
}
func (s *SwitchableStore) GetProject(tenant, id string) (Project, error) {
	return s.store().GetProject(tenant, id)
}
func (s *SwitchableStore) CreateNetwork(v Network) error { return s.store().CreateNetwork(v) }
func (s *SwitchableStore) ListNetworks(tenant, project string) []Network {
	return s.store().ListNetworks(tenant, project)
}
func (s *SwitchableStore) GetNetwork(tenant, id string) (Network, error) {
	return s.store().GetNetwork(tenant, id)
}
func (s *SwitchableStore) CreateNode(v Node) error { return s.store().CreateNode(v) }
func (s *SwitchableStore) GetNode(tenant, id string) (Node, error) {
	return s.store().GetNode(tenant, id)
}
func (s *SwitchableStore) GetNodeByID(id string) (Node, error) { return s.store().GetNodeByID(id) }
func (s *SwitchableStore) ListNodes(tenant, network string) []Node {
	return s.store().ListNodes(tenant, network)
}
func (s *SwitchableStore) UpdateNode(v Node) error { return s.store().UpdateNode(v) }
func (s *SwitchableStore) DeleteNode(tenant, id string) error {
	return s.store().DeleteNode(tenant, id)
}
func (s *SwitchableStore) AddTrafficSamples(v []TrafficSample) error {
	return s.store().AddTrafficSamples(v)
}
func (s *SwitchableStore) ListTrafficSamples(tenant, node, iface string, since time.Time) []TrafficSample {
	return s.store().ListTrafficSamples(tenant, node, iface, since)
}
func (s *SwitchableStore) AddPeer(v PeerRelation) error { return s.store().AddPeer(v) }
func (s *SwitchableStore) ListPeers(tenant, network string) []PeerRelation {
	return s.store().ListPeers(tenant, network)
}
func (s *SwitchableStore) CreateRevision(v ConfigRevision) error { return s.store().CreateRevision(v) }
func (s *SwitchableStore) LatestRevision(tenant, network string) (ConfigRevision, error) {
	return s.store().LatestRevision(tenant, network)
}
func (s *SwitchableStore) CreateDelivery(v ConfigDelivery) error { return s.store().CreateDelivery(v) }
func (s *SwitchableStore) UpdateDelivery(v ConfigDelivery) error { return s.store().UpdateDelivery(v) }
func (s *SwitchableStore) ListDeliveries(tenant, node string) []ConfigDelivery {
	return s.store().ListDeliveries(tenant, node)
}
func (s *SwitchableStore) CreateCommand(v AgentCommand) error { return s.store().CreateCommand(v) }
func (s *SwitchableStore) ClaimCommands(node string) []AgentCommand {
	return s.store().ClaimCommands(node)
}
func (s *SwitchableStore) UpdateCommand(v AgentCommand) error { return s.store().UpdateCommand(v) }
func (s *SwitchableStore) ListCommands(tenant, node string) []AgentCommand {
	return s.store().ListCommands(tenant, node)
}
func (s *SwitchableStore) ListCommandsPage(tenant, node string, limit, offset int, errorsOnly bool) []AgentCommand {
	return s.store().ListCommandsPage(tenant, node, limit, offset, errorsOnly)
}
func (s *SwitchableStore) ClearCommands(tenant, node string) error {
	return s.store().ClearCommands(tenant, node)
}
func (s *SwitchableStore) CreateEnrollment(v EnrollmentToken) error {
	return s.store().CreateEnrollment(v)
}
func (s *SwitchableStore) ConsumeEnrollment(token string) (EnrollmentToken, error) {
	return s.store().ConsumeEnrollment(token)
}
func (s *SwitchableStore) CreateIdentity(v AgentIdentity) error { return s.store().CreateIdentity(v) }
func (s *SwitchableStore) GetIdentity(node string) (AgentIdentity, error) {
	return s.store().GetIdentity(node)
}
func (s *SwitchableStore) AddAudit(v AuditEvent) error          { return s.store().AddAudit(v) }
func (s *SwitchableStore) ListAudit(tenant string) []AuditEvent { return s.store().ListAudit(tenant) }
func (s *SwitchableStore) GetUserByEmail(email string) (User, error) {
	return s.store().GetUserByEmail(email)
}
func (s *SwitchableStore) GetUser(id string) (User, error) { return s.store().GetUser(id) }
func (s *SwitchableStore) UpdateUserLastLogin(id string, at time.Time) error {
	return s.store().UpdateUserLastLogin(id, at)
}
func (s *SwitchableStore) HasUsers() (bool, error)         { return s.store().HasUsers() }
func (s *SwitchableStore) CreateInitialAdmin(v User) error { return s.store().CreateInitialAdmin(v) }

func (s *SwitchableStore) ListUsers(tenant string) []User { return s.store().ListUsers(tenant) }
func (s *SwitchableStore) CreateUser(v User) error        { return s.store().CreateUser(v) }
func (s *SwitchableStore) GetSettings(tenant string) (SystemSettings, error) {
	return s.store().GetSettings(tenant)
}
func (s *SwitchableStore) UpsertSettings(v SystemSettings) error { return s.store().UpsertSettings(v) }
func (s *SwitchableStore) ListNotificationChannels(tenant string) []NotificationChannel {
	return s.store().ListNotificationChannels(tenant)
}
func (s *SwitchableStore) GetNotificationChannel(tenant, id string) (NotificationChannel, error) {
	return s.store().GetNotificationChannel(tenant, id)
}
func (s *SwitchableStore) CreateNotificationChannel(v NotificationChannel) error {
	return s.store().CreateNotificationChannel(v)
}
func (s *SwitchableStore) UpdateNotificationChannel(v NotificationChannel) error {
	return s.store().UpdateNotificationChannel(v)
}
func (s *SwitchableStore) DeleteNotificationChannel(tenant, id string) error {
	return s.store().DeleteNotificationChannel(tenant, id)
}
func (s *SwitchableStore) AddNotificationLog(v NotificationLog) error {
	return s.store().AddNotificationLog(v)
}
func (s *SwitchableStore) ListNotificationLogs(tenant string) []NotificationLog {
	return s.store().ListNotificationLogs(tenant)
}
func (s *SwitchableStore) ListAuditPage(tenant string, limit, offset int) []AuditEvent {
	return s.store().ListAuditPage(tenant, limit, offset)
}
func (s *SwitchableStore) ClearAudit(tenant string) error {
	return s.store().ClearAudit(tenant)
}
