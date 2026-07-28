package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/wiremesh/wiremesh/internal/wireproto"
)

type agentLocation = wireproto.AgentLocation

func validAgentLocationCoordinates(location agentLocation) bool {
	return !math.IsNaN(location.Latitude) && !math.IsInf(location.Latitude, 0) && location.Latitude >= -90 && location.Latitude <= 90 &&
		!math.IsNaN(location.Longitude) && !math.IsInf(location.Longitude, 0) && location.Longitude >= -180 && location.Longitude <= 180 &&
		(location.Latitude != 0 || location.Longitude != 0)
}

func agentLocationSummary(location agentLocation) string {
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
	return newAgentClient(client, state).FetchLocation(ctx)
}

func (client agentClient) FetchLocation(ctx context.Context) (agentLocation, error) {
	var location agentLocation
	if _, err := client.doJSON(ctx, http.MethodGet, "/agent/v1/location", nil, &location, 0, http.StatusOK); err != nil {
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
	if location.LocationSource != "" && !validAgentLocationCoordinates(location) {
		location.LocationName = ""
		location.LocationSource = ""
		location.Latitude = 0
		location.Longitude = 0
	}
	return location, nil
}

// fetchRealPublicIPv4 resolves the machine's real public IPv4 address once, at
// agent startup. The result is attached to every subsequent report so the
// control plane GeoIP-locates the node's own address instead of whatever NAT
// or proxy egress address the reporting connection happens to show. It is not
// refreshed on a timer: a single request at startup avoids the periodic
// outbound pattern that makes a background agent look like C2.
func fetchRealPublicIPv4(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	if strings.TrimSpace(endpoint) == "" {
		return "", fmt.Errorf("no public IPv4 discovery endpoint configured")
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(discoveryCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("public IPv4 discovery returned %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(data))
	if strings.HasPrefix(text, "{") {
		var payload struct {
			IP       string `json:"ip"`
			Query    string `json:"query"`
			PublicIP string `json:"public_ip"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return "", fmt.Errorf("decode public IPv4 response: %w", err)
		}
		for _, candidate := range []string{payload.IP, payload.Query, payload.PublicIP} {
			if text = strings.TrimSpace(candidate); text != "" {
				break
			}
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
