package control

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

var (
	errGeoIPUnavailable = errors.New("GeoIP database is not loaded")
	errGeoIPNotFound    = errors.New("GeoIP location was not found")
)

type geoIPLocation struct {
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

// requestPublicIP determines the client's public IPv4 address. An agent that
// resolved its own address at startup reports it in X-Agent-Public-IP, which
// is the most reliable value because it is the node's own public IP rather
// than whatever NAT or proxy egress address the reporting connection shows.
// Otherwise the connection source address is used, honoring forwarded headers
// only when the direct peer is a private/loopback proxy.
func requestPublicIP(r *http.Request) string {
	if address, ok := publicAddress(r.Header.Get("X-Agent-Public-IP")); ok && address.Is4() {
		return address.String()
	}
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
	if address, ok := publicAddress(r.RemoteAddr); ok {
		return address.String()
	}
	return ""
}

func (a *App) geoReaderForTenant(tenant string) (*geoReaderState, error) {
	a.geoMu.RLock()
	state := a.geoReaders[tenant]
	a.geoMu.RUnlock()
	if state != nil {
		return state, nil
	}
	settings, err := a.tenantSettings(tenant)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(settings.GeoIPDBPath) == "" {
		return nil, errGeoIPUnavailable
	}
	if _, err := a.loadGeoIP(tenant, settings.GeoIPDBPath); err != nil {
		return nil, err
	}
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
