package wireproto

import "time"

// CommandLongPollWait is the coordination window used by Agents when long
// polling for commands and the upper bound the control plane applies to the
// `wait` query parameter. Keeping the value in one place avoids the two
// processes drifting apart.
const CommandLongPollWait = 25 * time.Second

type EnrollmentRequest struct {
	Token        string            `json:"token"`
	Name         string            `json:"name"`
	Endpoint     string            `json:"endpoint,omitempty"`
	Region       string            `json:"region,omitempty"`
	OS           string            `json:"os"`
	AgentVersion string            `json:"agent_version"`
	Labels       map[string]string `json:"labels"`
}

type EnrollmentNode struct {
	ID string `json:"id"`
}

type EnrollmentResponse struct {
	Node                   EnrollmentNode `json:"node"`
	CertificatePEM         string         `json:"certificate_pem"`
	PrivateKeyPEM          string         `json:"private_key_pem"`
	CertificateFingerprint string         `json:"certificate_fingerprint,omitempty"`
	CAPEM                  string         `json:"ca_pem"`
	ExpiresAt              string         `json:"expires_at"`
}

type AgentCommand struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	State string `json:"state"`
}

type CommandProgressRequest struct {
	Progress string `json:"progress"`
}

type CommandResultRequest struct {
	State  string `json:"state"`
	Result string `json:"result"`
}

type ConfigStatusRequest struct {
	Version uint64 `json:"version"`
	State   string `json:"state"`
	Message string `json:"message"`
}

type HeartbeatRequest struct {
	Hostname        string                     `json:"hostname"`
	OS              string                     `json:"os"`
	AgentVersion    string                     `json:"agent_version"`
	Labels          map[string]string          `json:"labels"`
	Interfaces      string                     `json:"interfaces"`
	WireGuard       []WireGuardInterfaceStatus `json:"wireguard"`
	PeerConfigs     []PeerConfigFile           `json:"peer_configs,omitempty"`
	CollectionError string                     `json:"collection_error,omitempty"`
	Location        *AgentLocation             `json:"location,omitempty"`
}

type WireGuardPeerStatus struct {
	PublicKey           string   `json:"public_key"`
	Endpoint            string   `json:"endpoint"`
	AllowedIPs          []string `json:"allowed_ips"`
	LatestHandshakeAt   string   `json:"latest_handshake_at,omitempty"`
	ReceiveBytes        int64    `json:"receive_bytes"`
	TransmitBytes       int64    `json:"transmit_bytes"`
	PersistentKeepalive int      `json:"persistent_keepalive,omitempty"`
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
	MTU        int          `json:"mtu"`
	Peers      []PeerConfig `json:"peers"`
}

type ConfigResponse struct {
	Version uint64     `json:"version"`
	Config  NodeConfig `json:"config"`
}

type PeerConfigFile struct {
	Interface string `json:"interface"`
	Path      string `json:"path,omitempty"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type PeerConfigResponse struct {
	NodeID string           `json:"node_id"`
	Files  []PeerConfigFile `json:"files"`
}

type AgentUpdateManifest struct {
	Available         bool   `json:"available"`
	Version           string `json:"version,omitempty"`
	OS                string `json:"os,omitempty"`
	Arch              string `json:"arch,omitempty"`
	Size              int64  `json:"size,omitempty"`
	SHA256            string `json:"sha256,omitempty"`
	DownloadURL       string `json:"download_url,omitempty"`
	MinAgentVersion   string `json:"min_agent_version,omitempty"`
	CurrentCompatible bool   `json:"current_compatible"`
	Error             string `json:"error,omitempty"`
}

type AgentLocation struct {
	PublicIP       string  `json:"public_ip,omitempty"`
	LocationName   string  `json:"location_name,omitempty"`
	LocationSource string  `json:"location_source,omitempty"`
	Country        string  `json:"country,omitempty"`
	CountryCode    string  `json:"country_code,omitempty"`
	Region         string  `json:"region,omitempty"`
	City           string  `json:"city,omitempty"`
	Latitude       float64 `json:"latitude,omitempty"`
	Longitude      float64 `json:"longitude,omitempty"`
	Timezone       string  `json:"timezone,omitempty"`
}
