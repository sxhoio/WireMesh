package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func createGeolocationTestNode(t *testing.T, app *App) Node {
	t.Helper()
	project := Project{ID: "project-geo", TenantID: "tenant-geo", Name: "Geo", CreatedAt: time.Now()}
	network := Network{ID: "network-geo", TenantID: project.TenantID, ProjectID: project.ID, Name: "Geo", CIDR: "10.88.0.0/24", Topology: TopologyFullMesh, CreatedAt: time.Now()}
	if err := app.store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateNetwork(network); err != nil {
		t.Fatal(err)
	}
	node, err := app.createNode(project.TenantID, network, "geo-node", "", "", "linux/amd64", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func TestRequestPublicIPUsesForwardedAddressOnlyBehindProxy(t *testing.T) {
	direct := httptest.NewRequest(http.MethodGet, "/", nil)
	direct.RemoteAddr = "8.8.8.8:12345"
	direct.Header.Set("X-Forwarded-For", "1.1.1.1")
	if got := requestPublicIP(direct); got != "8.8.8.8" {
		t.Fatalf("direct public address was replaced by an untrusted header: %q", got)
	}

	proxied := httptest.NewRequest(http.MethodGet, "/", nil)
	proxied.RemoteAddr = "127.0.0.1:12345"
	proxied.Header.Set("X-Forwarded-For", "10.0.0.8, 8.8.4.4")
	if got := requestPublicIP(proxied); got != "8.8.4.4" {
		t.Fatalf("forwarded public address was not detected: %q", got)
	}

	ipv6 := httptest.NewRequest(http.MethodGet, "/", nil)
	ipv6.RemoteAddr = "[2001:4860:4860::8888]:12345"
	if got := requestPublicIP(ipv6); got != "2001:4860:4860::8888" {
		t.Fatalf("IPv6 public address was not detected: %q", got)
	}
}

func TestRequestPublicIPPrefersAgentReportedAddress(t *testing.T) {
	// The agent reports its own public IPv4 at startup; it must win over the
	// connection source address, which may be a NAT or proxy egress.
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "198.51.100.1:54321"
	request.Header.Set("X-Agent-Public-IP", "203.0.113.9")
	if got := requestPublicIP(request); got != "203.0.113.9" {
		t.Fatalf("agent-reported public IP was not preferred: %q", got)
	}

	// An invalid or private reported address is ignored in favor of the source.
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "198.51.100.1:54321"
	request.Header.Set("X-Agent-Public-IP", "10.0.0.5")
	if got := requestPublicIP(request); got != "198.51.100.1" {
		t.Fatalf("private agent-reported IP must be ignored: %q", got)
	}
}

func TestAgentLocationEndpointReturnsObservedGeoIP(t *testing.T) {
	app := testApp(t)
	node := createGeolocationTestNode(t, app)
	app.geoLookup = func(tenant, ip string) (geoIPLocation, error) {
		if tenant != node.TenantID || ip != "8.8.8.8" {
			t.Fatalf("unexpected GeoIP lookup: tenant=%q ip=%q", tenant, ip)
		}
		return geoIPLocation{PublicIP: ip, LocationName: "中国 · 广东 · 广州", LocationSource: "geoip", Region: "广东", City: "广州", Latitude: 23.1291, Longitude: 113.2644}, nil
	}

	request := httptest.NewRequest(http.MethodGet, "/agent/v1/location", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("X-Forwarded-For", "8.8.8.8")
	request.Header.Set("X-Agent-ID", node.ID)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("location endpoint failed: %d %s", response.Code, response.Body.String())
	}
	var location geoIPLocation
	if err := json.NewDecoder(response.Body).Decode(&location); err != nil {
		t.Fatal(err)
	}
	if location.PublicIP != "8.8.8.8" || location.LocationSource != "geoip" || location.Latitude != 23.1291 || location.Longitude != 113.2644 {
		t.Fatalf("unexpected agent location: %#v", location)
	}
}

func TestAgentHeartbeatUsesGeoIPFallbackAndPreservesManualLocation(t *testing.T) {
	app := testApp(t)
	node := createGeolocationTestNode(t, app)
	app.geoLookup = func(tenant, ip string) (geoIPLocation, error) {
		return geoIPLocation{PublicIP: ip, LocationName: "中国 · 广东 · 广州", LocationSource: "geoip", Region: "广东", Latitude: 23.1291, Longitude: 113.2644}, nil
	}

	request := httptest.NewRequest(http.MethodPost, "/agent/v1/heartbeat", strings.NewReader(`{"hostname":"geo-host","location":{"public_ip":"8.8.8.8"},"wireguard":[]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Agent-ID", node.ID)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("heartbeat failed: %d %s", response.Code, response.Body.String())
	}
	stored, err := app.store.GetNodeByID(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LocationSource != "geoip" || stored.LocationName != "中国 · 广东 · 广州" || stored.Latitude != 23.1291 || stored.Longitude != 113.2644 {
		t.Fatalf("GeoIP fallback was not stored: %#v", stored)
	}

	stored.LocationSource = "manual"
	stored.LocationName = "手动位置"
	stored.Latitude = 31.2304
	stored.Longitude = 121.4737
	if err := app.store.UpdateNode(stored); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "/agent/v1/heartbeat", strings.NewReader(`{"location":{"location_name":"自动位置","location_source":"agent","latitude":22.5431,"longitude":114.0579},"wireguard":[]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Agent-ID", node.ID)
	response = httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("manual-preservation heartbeat failed: %d %s", response.Code, response.Body.String())
	}
	stored, err = app.store.GetNodeByID(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LocationSource != "manual" || stored.LocationName != "手动位置" || stored.Latitude != 31.2304 || stored.Longitude != 121.4737 {
		t.Fatalf("automatic location overwrote manual coordinates: %#v", stored)
	}
}

func TestAgentLocationEndpointRequiresAgentIdentity(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/agent/v1/location", nil)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized location request, got %d: %s", response.Code, response.Body.String())
	}
}

func TestAgentHeartbeatGeoIPsClientReportedPublicIP(t *testing.T) {
	app := testApp(t)
	node := createGeolocationTestNode(t, app)
	app.geoLookup = func(tenant, ip string) (geoIPLocation, error) {
		if tenant != node.TenantID || ip != "203.0.113.9" {
			t.Fatalf("GeoIP must use the client-reported public IP, got tenant=%q ip=%q", tenant, ip)
		}
		return geoIPLocation{PublicIP: ip, LocationName: "中国 · 上海", LocationSource: "geoip", Region: "上海", Latitude: 31.2304, Longitude: 121.4737}, nil
	}

	// The connection source address differs from the agent-reported address,
	// as it would behind NAT; the reported one must win.
	request := httptest.NewRequest(http.MethodPost, "/agent/v1/heartbeat", strings.NewReader(`{"location":{"public_ip":"203.0.113.9"},"wireguard":[]}`))
	request.RemoteAddr = "198.51.100.1:54321"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Agent-ID", node.ID)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("heartbeat failed: %d %s", response.Code, response.Body.String())
	}
	stored, err := app.store.GetNodeByID(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LocationSource != "geoip" || stored.LocationName != "中国 · 上海" || stored.Latitude != 31.2304 || stored.Longitude != 121.4737 {
		t.Fatalf("client-reported public IP was not GeoIP located: %#v", stored)
	}
}

func TestLegacyAgentHeartbeatUsesObservedPublicIP(t *testing.T) {
	app := testApp(t)
	node := createGeolocationTestNode(t, app)
	app.geoLookup = func(tenant, ip string) (geoIPLocation, error) {
		if tenant != node.TenantID || ip != "8.8.4.4" {
			t.Fatalf("unexpected GeoIP lookup: tenant=%q ip=%q", tenant, ip)
		}
		return geoIPLocation{PublicIP: ip, LocationName: "美国 · 加利福尼亚州", LocationSource: "geoip", Region: "加利福尼亚州", Latitude: 37.751, Longitude: -97.822}, nil
	}

	request := httptest.NewRequest(http.MethodPost, "/agent/v1/heartbeat", strings.NewReader(`{"wireguard":[]}`))
	request.RemoteAddr = "8.8.4.4:54321"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Agent-ID", node.ID)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("legacy heartbeat failed: %d %s", response.Code, response.Body.String())
	}
	stored, err := app.store.GetNodeByID(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LocationSource != "geoip" || stored.LocationName != "美国 · 加利福尼亚州" || stored.Latitude != 37.751 || stored.Longitude != -97.822 {
		t.Fatalf("legacy agent was not automatically located: %#v", stored)
	}
}

func TestAutomaticLocationRejectsZeroCoordinatesAndUnknownSources(t *testing.T) {
	if validAutomaticLocationCoordinates(0, 0) {
		t.Fatal("automatic location must not place unknown nodes at 0,0")
	}
	if source := automaticLocationSource("untrusted"); source != "" {
		t.Fatalf("unexpected automatic location source: %q", source)
	}
}
