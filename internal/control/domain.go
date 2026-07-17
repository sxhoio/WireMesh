package control

import "time"

type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

type Topology string

const (
	TopologyFullMesh Topology = "full_mesh"
	TopologyHubSpoke Topology = "hub_spoke"
	TopologyCustom   Topology = "custom"
)

type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}
type Project struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}
type User struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Name         string    `json:"name"`
	Role         Role      `json:"role"`
	LastLoginAt  time.Time `json:"last_login_at"`
	CreatedAt    time.Time `json:"created_at"`
}
type Network struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	CIDR      string    `json:"cidr"`
	DNS       string    `json:"dns"`
	Topology  Topology  `json:"topology"`
	CreatedAt time.Time `json:"created_at"`
}
type WireGuardPeerStatus struct {
	PublicKey           string    `json:"public_key"`
	Endpoint            string    `json:"endpoint"`
	AllowedIPs          []string  `json:"allowed_ips"`
	LatestHandshakeAt   time.Time `json:"latest_handshake_at,omitempty"`
	ReceiveBytes        int64     `json:"receive_bytes"`
	TransmitBytes       int64     `json:"transmit_bytes"`
	PersistentKeepalive int       `json:"persistent_keepalive,omitempty"`
}

type WireGuardInterfaceStatus struct {
	Name       string                `json:"name"`
	PublicKey  string                `json:"public_key"`
	ListenPort int                   `json:"listen_port"`
	Addresses  []string              `json:"addresses"`
	MTU        int                   `json:"mtu"`
	Up         bool                  `json:"up"`
	Peers      []WireGuardPeerStatus `json:"peers"`
}

type Node struct {
	ID                string                     `json:"id"`
	TenantID          string                     `json:"tenant_id"`
	ProjectID         string                     `json:"project_id"`
	NetworkID         string                     `json:"network_id"`
	Name              string                     `json:"name"`
	Hostname          string                     `json:"hostname"`
	InterfaceSelector string                     `json:"interface_selector"`
	CollectionError   string                     `json:"collection_error,omitempty"`
	Address           string                     `json:"address"`
	Endpoint          string                     `json:"endpoint"`
	Region            string                     `json:"region"`
	OS                string                     `json:"os"`
	AgentVersion      string                     `json:"agent_version"`
	Labels            map[string]string          `json:"labels"`
	PublicKey         string                     `json:"public_key"`
	PrivateKey        EncryptedSecret            `json:"-"`
	WireGuard         []WireGuardInterfaceStatus `json:"wireguard"`
	LastSeen          time.Time                  `json:"last_seen"`
	CreatedAt         time.Time                  `json:"created_at"`
}
type PeerRelation struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	NetworkID    string    `json:"network_id"`
	SourceNodeID string    `json:"source_node_id"`
	TargetNodeID string    `json:"target_node_id"`
	CreatedAt    time.Time `json:"created_at"`
}
type EnrollmentToken struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	ProjectID string     `json:"project_id"`
	NetworkID string     `json:"network_id"`
	Token     string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at"`
}
type AgentIdentity struct {
	NodeID                 string    `json:"node_id"`
	CertificatePEM         string    `json:"certificate_pem"`
	CertificateFingerprint string    `json:"certificate_fingerprint"`
	ExpiresAt              time.Time `json:"expires_at"`
}
type PeerConfig struct {
	NodeID     string   `json:"node_id"`
	PublicKey  string   `json:"public_key"`
	Endpoint   string   `json:"endpoint"`
	AllowedIPs []string `json:"allowed_ips"`
}
type NodeConfig struct {
	NodeID     string       `json:"node_id"`
	NetworkID  string       `json:"network_id"`
	Address    string       `json:"address"`
	PrivateKey string       `json:"private_key"`
	ListenPort int          `json:"listen_port"`
	Peers      []PeerConfig `json:"peers"`
}
type ConfigRevision struct {
	ID        string                `json:"id"`
	TenantID  string                `json:"tenant_id"`
	ProjectID string                `json:"project_id"`
	NetworkID string                `json:"network_id"`
	Version   uint64                `json:"version"`
	Configs   map[string]NodeConfig `json:"configs"`
	CreatedAt time.Time             `json:"created_at"`
}
type ConfigDelivery struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	NodeID    string    `json:"node_id"`
	Version   uint64    `json:"version"`
	State     string    `json:"state"`
	Message   string    `json:"message"`
	UpdatedAt time.Time `json:"updated_at"`
}
type AuditEvent struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id"`
	ActorID      string            `json:"actor_id"`
	Action       string            `json:"action"`
	ResourceType string            `json:"resource_type"`
	ResourceID   string            `json:"resource_id"`
	Metadata     map[string]string `json:"metadata"`
	CreatedAt    time.Time         `json:"created_at"`
}

type NetworkDefaults struct {
	DNS             string `json:"dns"`
	Port            int    `json:"port"`
	MTU             int    `json:"mtu"`
	Keepalive       int    `json:"keepalive"`
	DefaultTopology string `json:"defaultTopology"`
}

type StatusRules struct {
	AgentOfflineSec int `json:"agentOfflineSec"`
	HandshakeSec    int `json:"handshakeSec"`
	RedFailCount    int `json:"redFailCount"`
}

type CollectionSettings struct {
	ReportSec     int `json:"reportSec"`
	ProbeSec      int `json:"probeSec"`
	MapRefreshSec int `json:"mapRefreshSec"`
}

type RetentionSettings struct {
	RawDays    int `json:"rawDays"`
	HourlyDays int `json:"hourlyDays"`
	DailyDays  int `json:"dailyDays"`
}

type AgentSettings struct {
	Labels        string `json:"labels"`
	UpgradePolicy string `json:"upgradePolicy"`
}

type SystemSettings struct {
	TenantID          string             `json:"-"`
	DashboardName     string             `json:"dashboardName"`
	SessionTimeoutMin int                `json:"sessionTimeoutMin"`
	NetDefaults       NetworkDefaults    `json:"netDefaults"`
	StatusRules       StatusRules        `json:"statusRules"`
	Collect           CollectionSettings `json:"collect"`
	Retention         RetentionSettings  `json:"retention"`
	Agent             AgentSettings      `json:"agent"`
	GeoIPDBPath       string             `json:"-"`
	UpdatedAt         time.Time          `json:"updatedAt"`
}

type NotificationChannel struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"-"`
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Target    EncryptedSecret `json:"-"`
	Enabled   bool            `json:"enabled"`
	AllAgents bool            `json:"-"`
	AgentIDs  []string        `json:"-"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type NotificationLog struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"-"`
	ChannelID   string    `json:"channelId"`
	ChannelName string    `json:"channelName"`
	ChannelType string    `json:"channelType"`
	AgentName   string    `json:"agentName"`
	Message     string    `json:"message"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}
