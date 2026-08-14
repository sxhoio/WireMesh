package control

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/wiremesh/wiremesh/internal/wireproto"
)

var (
	errGeoIPUnavailable = errors.New("GeoIP database is not loaded")
	errGeoIPNotFound    = errors.New("GeoIP location was not found")
)

// geoRetryInterval bounds how often a failed GeoIP database load is retried so
// a misconfigured or missing database does not reopen the file on every
// heartbeat or lookup.
const geoRetryInterval = 5 * time.Minute

type geoIPLocation = wireproto.AgentLocation

type geoIPRecord struct {
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	Location struct {
		Latitude  float64 `maxminddb:"latitude"`
		Longitude float64 `maxminddb:"longitude"`
		TimeZone  string  `maxminddb:"time_zone"`
	} `maxminddb:"location"`
}

func localizedGeoName(names map[string]string) string {
	for _, language := range []string{"zh-CN", "zh", "en"} {
		if value := strings.TrimSpace(names[language]); value != "" {
			return value
		}
	}
	return ""
}

func joinGeoLocation(parts ...string) string {
	result := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		result = append(result, part)
	}
	return strings.Join(result, " · ")
}

func validLocationCoordinates(latitude, longitude float64) bool {
	return !math.IsNaN(latitude) && !math.IsInf(latitude, 0) && latitude >= -90 && latitude <= 90 &&
		!math.IsNaN(longitude) && !math.IsInf(longitude, 0) && longitude >= -180 && longitude <= 180
}

func validAutomaticLocationCoordinates(latitude, longitude float64) bool {
	return validLocationCoordinates(latitude, longitude) && (latitude != 0 || longitude != 0)
}

func automaticLocationSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "agent":
		return "agent"
	case "geoip":
		return "geoip"
	default:
		return ""
	}
}

func automaticLocationName(location geoIPLocation) string {
	if name := strings.TrimSpace(location.LocationName); name != "" {
		return name
	}
	return joinGeoLocation(location.Country, location.Region, location.City)
}

func parseAddress(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(strings.Trim(value, "\""))
	if value == "" {
		return netip.Addr{}, false
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	} else {
		value = strings.Trim(value, "[]")
	}
	if percent := strings.LastIndex(value, "%"); percent >= 0 {
		value = value[:percent]
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

func publicAddress(value string) (netip.Addr, bool) {
	address, ok := parseAddress(value)
	if !ok || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return netip.Addr{}, false
	}
	return address, true
}

// requestPublicIP determines the client's public IP address, preferring IPv4.
// An agent that resolved its own address at startup reports it in
// X-Agent-Public-IP, which is the most reliable value because it is the node's
// own public IP rather than whatever NAT or proxy egress address the reporting
// connection shows. Otherwise the connection source address is used (IPv4
// preferred over IPv6 so a dual-stack host is GeoIP-located consistently with
// its public IPv4 endpoint), honoring forwarded headers only when the direct
// peer is a private/loopback proxy.
func requestPublicIP(r *http.Request) string {
	// 1. Agent 自报地址（IPv4 优先；若 Agent 上报 IPv6 也接受，仅在无
	//    IPv4 时使用）
	if address, ok := publicAddress(r.Header.Get("X-Agent-Public-IP")); ok {
		return address.String()
	}
	// 2. 转发头（仅当直连来源是私网/回环代理时信任）
	remote, remoteOK := parseAddress(r.RemoteAddr)
	remoteIsProxy := !remoteOK || remote.IsPrivate() || remote.IsLoopback() || remote.IsLinkLocalUnicast()
	if remoteIsProxy {
		for _, forwarded := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
			if address, ok := publicAddress(forwarded); ok {
				return address.String()
			}
		}
		if address, ok := publicAddress(r.Header.Get("X-Real-IP")); ok {
			return address.String()
		}
	}
	// 3. 连接源地址：IPv4 优先于 IPv6（双栈主机与节点 IPv4 端点一致）
	var fallbackIPv6 string
	if address, ok := publicAddress(r.RemoteAddr); ok {
		if address.Is4() {
			return address.String()
		}
		fallbackIPv6 = address.String()
	}
	return fallbackIPv6
}

func (a *App) geoReaderForTenant(tenant string) (*geoReaderState, error) {
	a.geoMu.RLock()
	state := a.geoReaders[tenant]
	lastFailure := a.geoFailures[tenant]
	a.geoMu.RUnlock()
	if state != nil {
		return state, nil
	}
	now := time.Now()
	if !lastFailure.IsZero() && now.Sub(lastFailure) < geoRetryInterval {
		return nil, errGeoIPUnavailable
	}
	settings, err := a.tenantSettings(tenant)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(settings.GeoIPDBPath) == "" {
		return nil, errGeoIPUnavailable
	}
	if _, err := a.loadGeoIP(tenant, settings.GeoIPDBPath); err != nil {
		a.geoMu.Lock()
		a.geoFailures[tenant] = now
		a.geoMu.Unlock()
		return nil, err
	}
	a.geoMu.Lock()
	delete(a.geoFailures, tenant)
	a.geoMu.Unlock()
	a.geoMu.RLock()
	state = a.geoReaders[tenant]
	a.geoMu.RUnlock()
	if state == nil {
		return nil, errGeoIPUnavailable
	}
	return state, nil
}

func (a *App) lookupGeoIPLocation(tenant, ipText string) (geoIPLocation, error) {
	address, ok := publicAddress(ipText)
	if !ok {
		return geoIPLocation{}, fmt.Errorf("valid public IP address is required")
	}
	if _, err := a.geoReaderForTenant(tenant); err != nil {
		return geoIPLocation{}, err
	}
	var record geoIPRecord
	a.geoMu.RLock()
	state := a.geoReaders[tenant]
	if state == nil {
		a.geoMu.RUnlock()
		return geoIPLocation{}, errGeoIPUnavailable
	}
	err := state.Reader.Lookup(net.IP(address.AsSlice()), &record)
	a.geoMu.RUnlock()
	if err != nil {
		return geoIPLocation{}, fmt.Errorf("GeoIP lookup failed: %w", err)
	}
	city := localizedGeoName(record.City.Names)
	country := localizedGeoName(record.Country.Names)
	region := ""
	if len(record.Subdivisions) > 0 {
		region = localizedGeoName(record.Subdivisions[0].Names)
	}
	if !validAutomaticLocationCoordinates(record.Location.Latitude, record.Location.Longitude) {
		return geoIPLocation{}, errGeoIPNotFound
	}
	return geoIPLocation{
		PublicIP: address.String(), LocationName: joinGeoLocation(country, region, city), LocationSource: "geoip",
		Country: country, CountryCode: record.Country.ISOCode, Region: region, City: city,
		Latitude: record.Location.Latitude, Longitude: record.Location.Longitude, Timezone: record.Location.TimeZone,
	}, nil
}

func (a *App) agentLocation(w http.ResponseWriter, r *http.Request) {
	node, ok := a.agentNode(w, r)
	if !ok {
		return
	}
	publicIP := requestPublicIP(r)
	location := geoIPLocation{PublicIP: publicIP}
	if publicIP != "" {
		if resolved, err := a.geoLookup(node.TenantID, publicIP); err == nil {
			location = resolved
			location.PublicIP = publicIP
		}
	}
	writeJSON(w, http.StatusOK, location)
}

func (a *App) applyAutomaticNodeLocation(node *Node, report geoIPLocation, r *http.Request) {
	if node.LocationSource == "manual" {
		return
	}
	if source := automaticLocationSource(report.LocationSource); source != "" && validAutomaticLocationCoordinates(report.Latitude, report.Longitude) {
		node.LocationSource = source
		node.LocationName = automaticLocationName(report)
		node.Latitude = report.Latitude
		node.Longitude = report.Longitude
		if strings.TrimSpace(report.Region) != "" {
			node.Region = strings.TrimSpace(report.Region)
		}
		return
	}
	publicIP := ""
	if address, ok := publicAddress(report.PublicIP); ok {
		publicIP = address.String()
	}
	if publicIP == "" {
		publicIP = requestPublicIP(r)
	}
	if publicIP == "" {
		return
	}
	resolved, err := a.geoLookup(node.TenantID, publicIP)
	if err != nil {
		return
	}
	node.LocationSource = "geoip"
	node.LocationName = resolved.LocationName
	node.Latitude = resolved.Latitude
	node.Longitude = resolved.Longitude
	if resolved.Region != "" {
		node.Region = resolved.Region
	}
}

// adoptPublicEndpoint fills node.Endpoint from the agent's real public IPv4
// and its WireGuard listen port when the operator has not set one manually.
// The agent reports the address it discovered at startup in location.public_ip
// (X-Agent-Public-IP); the connection source address is the fallback. A
// manually configured endpoint is never overwritten.
func (a *App) adoptPublicEndpoint(node *Node, report geoIPLocation, r *http.Request) {
	if strings.TrimSpace(node.Endpoint) != "" {
		return
	}
	publicIP := ""
	if address, ok := publicAddress(report.PublicIP); ok {
		publicIP = address.String()
	}
	if publicIP == "" {
		publicIP = requestPublicIP(r)
	}
	if publicIP == "" {
		return
	}
	port := node.ListenPort
	if port < 1 || port > 65535 {
		port = defaultNodeListenPort
	}
	node.Endpoint = publicIP + ":" + fmt.Sprint(port)
}

// geoLocatePeerEndpoints GeoIP-resolves each peer's public endpoint address so
// the console can place otherwise-unknown peers (temp peers) on the map. Only
// public endpoint IPs are looked up; private/empty endpoints are left without
// coordinates and stay in the unknown-location panel. Lookups are cached per
// heartbeat so peers sharing an egress address cost a single GeoIP query.
func (a *App) geoLocatePeerEndpoints(tenant string, interfaces []WireGuardInterfaceStatus) {
	if len(interfaces) == 0 {
		return
	}
	cache := map[string]*geoIPLocation{}
	for i := range interfaces {
		for j := range interfaces[i].Peers {
			peer := &interfaces[i].Peers[j]
			address, ok := publicAddress(peer.Endpoint)
			if !ok {
				continue
			}
			ip := address.String()
			resolved, seen := cache[ip]
			if !seen {
				if location, err := a.geoLookup(tenant, ip); err == nil && validAutomaticLocationCoordinates(location.Latitude, location.Longitude) {
					found := location
					resolved = &found
				}
				cache[ip] = resolved
			}
			if resolved == nil {
				continue
			}
			peer.LocationName = automaticLocationName(*resolved)
			peer.Latitude = resolved.Latitude
			peer.Longitude = resolved.Longitude
		}
	}
}
