package control

import (
	"time"

	"github.com/wiremesh/wiremesh/internal/wireproto"
)

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

type Project struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}
type User struct {
	ID           string          `json:"id"`
	TenantID     string          `json:"tenant_id"`
	Email        string          `json:"email"`
	PasswordHash string          `json:"-"`
	Name         string          `json:"name"`
	Role         Role            `json:"role"`
	LastLoginAt  time.Time       `json:"last_login_at"`
	CreatedAt    time.Time       `json:"created_at"`
	TotpSecret   EncryptedSecret `json:"-"`
	TotpEnabled  bool            `json:"-"`
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
	LocationName        string    `json:"location_name,omitempty"`
	Latitude            float64   `json:"latitude,omitempty"`
	Longitude           float64   `json:"longitude,omitempty"`
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

type PeerConfigFile struct {
	Interface string    `json:"interface"`
	Path      string    `json:"path,omitempty"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
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
	Enabled           bool                       `json:"enabled"`
	ListenPort        int                        `json:"listen_port"`
	MTU               int                        `json:"mtu"`
	Address           string                     `json:"address"`
	Endpoint          string                     `json:"endpoint"`
	Region            string                     `json:"region"`
	LocationName      string                     `json:"location_name"`
	LocationSource    string                     `json:"location_source"`
	Latitude          float64                    `json:"latitude"`
	Longitude         float64                    `json:"longitude"`
	OS                string                     `json:"os"`
	AgentVersion      string                     `json:"agent_version"`
	Labels            map[string]string          `json:"labels"`
	PublicKey         string                     `json:"public_key"`
	PrivateKey        EncryptedSecret            `json:"-"`
	WireGuard         []WireGuardInterfaceStatus `json:"wireguard"`
	PeerConfigFiles   []PeerConfigFile           `json:"-"`
	DesiredPeerConfig []PeerConfigFile           `json:"-"`
	LastSeen          time.Time                  `json:"last_seen"`
	CreatedAt         time.Time                  `json:"created_at"`
}
type TrafficSample struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	NodeID        string    `json:"node_id"`
	InterfaceName string    `json:"interface_name"`
	ReceiveBytes  int64     `json:"receive_bytes"`
	TransmitBytes int64     `json:"transmit_bytes"`
	RecordedAt    time.Time `json:"recorded_at"`
}

type TrafficPoint struct {
	RecordedAt    time.Time `json:"recorded_at"`
	ReceiveBytes  int64     `json:"receive_bytes"`
	TransmitBytes int64     `json:"transmit_bytes"`
	RXMbps        float64   `json:"rx_mbps"`
	TXMbps        float64   `json:"tx_mbps"`
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
type PeerConfig = wireproto.PeerConfig
type NodeConfig = wireproto.NodeConfig
type AgentCommand struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	NodeID      string     `json:"node_id"`
	Type        string     `json:"type"`
	State       string     `json:"state"`
	Result      string     `json:"result,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type NodeLog struct {
	ID        string    `json:"id"`
	Level     string    `json:"level"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
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

// ConfigPublishResult is safe to return from the API. The immutable revision
// itself contains private WireGuard material and must remain server-side.
type ConfigPublishResult struct {
	RevisionID     string   `json:"revision_id,omitempty"`
	NetworkID      string   `json:"network_id"`
	Version        uint64   `json:"version"`
	ChangedNodeIDs []string `json:"changed_node_ids"`
	QueuedNodeIDs  []string `json:"queued_node_ids"`
	OfflineNodeIDs []string `json:"offline_node_ids"`
	Unchanged      bool     `json:"unchanged"`
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

type AlertRule struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"-"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	ThresholdSec int       `json:"threshold_sec"`
	ChannelIDs   []string  `json:"channel_ids"`
	Enabled      bool      `json:"enabled"`
	QuietSec     int       `json:"quiet_sec"`
	ScopeType    string    `json:"scope_type"` // all | network | node
	ScopeIDs     []string  `json:"scope_ids"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AlertFired 记录规则×节点的触发状态：FiredAt 为最近一次告警时间（静默期判断用），
// Active 表示当前仍处于故障状态（用于故障恢复时发送恢复通知）。
type AlertFired struct {
	TenantID string    `json:"-"`
	AlertKey string    `json:"alert_key"`
	FiredAt  time.Time `json:"fired_at"`
	Active   bool      `json:"active"`
}

type AlertEvent struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"-"`
	RuleID    string    `json:"rule_id"`
	RuleName  string    `json:"rule_name"`
	NodeID    string    `json:"node_id"`
	NodeName  string    `json:"node_name"`
	Message   string    `json:"message"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// AccessResource 定义一个可访问资源：某个节点上的服务（目标 CIDR + 可选端口）。
// 策略允许后，源节点到网关节点的 AllowedIPs 会包含该资源的目标 CIDR（IP 级）；
// 端口作为元数据保存，供后续 Agent 防火墙规则使用。
type AccessResource struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"-"`
	NetworkID     string    `json:"network_id"`
	Name          string    `json:"name"`
	GatewayNodeID string    `json:"gateway_node_id"`
	Target        string    `json:"target"`
	Port          int       `json:"port,omitempty"`
	Protocol      string    `json:"protocol,omitempty"`
	Description   string    `json:"description,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// AccessPolicy 定义哪些源节点（按标签或显式 ID 列表）可以访问哪些资源。
type AccessPolicy struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"-"`
	NetworkID     string    `json:"network_id"`
	Name          string    `json:"name"`
	SourceLabel   string    `json:"source_label,omitempty"`
	SourceNodeIDs []string  `json:"source_node_ids"`
	ResourceIDs   []string  `json:"resource_ids"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// DNSRecord 定义网络内的私有 DNS 映射（名称 → 隧道 IP）。节点自身的
// name → address 映射由前端自动展示，手动记录用于额外的主机名/服务名。
type DNSRecord struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"-"`
	NetworkID   string    `json:"network_id"`
	Name        string    `json:"name"`
	Address     string    `json:"address"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// APIToken 是供脚本/CI 调用控制平面 API 的长期凭据。只保存 SHA-256 哈希，
// 明文仅在创建时返回一次。
type APIToken struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"-"`
	Name       string     `json:"name"`
	TokenHash  string     `json:"-"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt time.Time  `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// EgressConfig 定义网络的出口网关：其他节点到出口节点的 AllowedIPs 会加入
// CIDRs（如 0.0.0.0/0），使这些节点的对外流量经出口网关转发。
type EgressConfig struct {
	TenantID     string    `json:"-"`
	NetworkID    string    `json:"network_id"`
	EgressNodeID string    `json:"egress_node_id"`
	CIDRs        []string  `json:"cidrs"`
	UpdatedAt    time.Time `json:"updated_at"`
}
