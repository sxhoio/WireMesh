package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// defaultPublicIPv4URLs are queried in order when the agent resolves its own
// public IPv4 address. WIREMESH_PUBLIC_IP_URL overrides the list (comma-separated).
var defaultPublicIPv4URLs = []string{
	"https://api.ipify.org",
	"https://ifconfig.me/ip",
	"https://ipinfo.io/ip",
}

type agentLocation struct {
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

func (location agentLocation) validCoordinates() bool {
	return !math.IsNaN(location.Latitude) && !math.IsInf(location.Latitude, 0) && location.Latitude >= -90 && location.Latitude <= 90 &&
		!math.IsNaN(location.Longitude) && !math.IsInf(location.Longitude, 0) && location.Longitude >= -180 && location.Longitude <= 180 &&
		(location.Latitude != 0 || location.Longitude != 0)
}

func (location agentLocation) summary() string {
	if location.LocationSource != "" {
		name := location.LocationName
		if name == "" {
			name = fmt.Sprintf("%.4f,%.4f", location.Latitude, location.Longitude)
		}
		return fmt.Sprintf("source=%s public_ip=%s location=%s", location.LocationSource, location.PublicIP, name)
	}
	if location.PublicIP != "" {
		return fmt.Sprintf("source=public-ip-only public_ip=%s", location.PublicIP)
	}
	return "source=unknown"
}

func fetchAgentLocation(ctx context.Context, client *http.Client, state agentState) (agentLocation, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, state.Server+"/agent/v1/location", nil)
	if err != nil {
		return agentLocation{}, err
	}
	setDevelopmentIdentity(request, state)
	response, err := client.Do(request)
	if err != nil {
		return agentLocation{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return agentLocation{}, responseError(response)
	}
	var location agentLocation
	if err := json.NewDecoder(response.Body).Decode(&location); err != nil {
		return agentLocation{}, fmt.Errorf("decode location response: %w", err)
	}
	location.PublicIP = strings.TrimSpace(location.PublicIP)
	if location.PublicIP != "" {
		if address, err := netip.ParseAddr(location.PublicIP); err != nil || !address.IsValid() {
			location.PublicIP = ""
		}
	}
	location.LocationName = strings.TrimSpace(location.LocationName)
	location.LocationSource = strings.ToLower(strings.TrimSpace(location.LocationSource))
	if location.LocationSource != "agent" && location.LocationSource != "geoip" {
		location.LocationSource = ""
	}
	if location.LocationSource != "" && !location.validCoordinates() {
		location.LocationName = ""
		location.LocationSource = ""
		location.Latitude = 0
		location.Longitude = 0
	}
	return location, nil
}

func publicIPv4URLs(override string) []string {
	if strings.TrimSpace(override) == "" {
		return defaultPublicIPv4URLs
	}
	urls := make([]string, 0, 4)
	for _, item := range strings.Split(override, ",") {
		if item = strings.TrimSpace(item); item != "" {
			urls = append(urls, item)
		}
	}
	return urls
}

// parsePublicIPv4Response accepts a public IPv4 address either as a plain-text
// body or as a JSON object with an "ip"/"query" field, which covers the common
// IP echo services. Private, loopback, and link-local answers are rejected so
// a proxy or captive portal cannot downgrade GeoIP to a useless location.
func parsePublicIPv4Response(body io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(body, 4096))
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(data))
	if strings.HasPrefix(text, "{") {
		var payload struct {
			IP    string `json:"ip"`
			Query string `json:"query"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return "", fmt.Errorf("decode public IP response: %w", err)
		}
		text = strings.TrimSpace(payload.IP)
		if text == "" {
			text = strings.TrimSpace(payload.Query)
		}
	}
	address, err := netip.ParseAddr(text)
	if err != nil || !address.Is4() {
		return "", fmt.Errorf("response is not an IPv4 address: %q", text)
	}
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return "", fmt.Errorf("response is not a public IPv4 address: %s", text)
	}
	return address.String(), nil
}

// fetchPublicIPv4 resolves the machine's real public IPv4 address by asking
// external echo services directly. GeoIP on the server's observed source
// address is wrong whenever the agent sits behind NAT or a forward proxy.
func fetchPublicIPv4(ctx context.Context, client *http.Client, urls []string) (string, error) {
	if len(urls) == 0 {
		return "", errors.New("no public IPv4 discovery URLs configured")
	}
	var errs []string
	for _, endpoint := range urls {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", endpoint, err))
			continue
		}
		response, err := client.Do(request)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", endpoint, err))
			continue
		}
		address, parseErr := parsePublicIPv4Response(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			errs = append(errs, fmt.Sprintf("%s: returned %s", endpoint, response.Status))
			continue
		}
		if parseErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", endpoint, parseErr))
			continue
		}
		return address, nil
	}
	return "", fmt.Errorf("public IPv4 discovery failed: %s", strings.Join(errs, "; "))
}

// fetchPublicIPv4WithTimeout gives discovery its own short deadline so a slow
// or blocked echo service cannot delay heartbeat reporting.
func fetchPublicIPv4WithTimeout(ctx context.Context, client *http.Client, urls []string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		return fetchPublicIPv4(ctx, client, urls)
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return fetchPublicIPv4(discoveryCtx, client, urls)
}
