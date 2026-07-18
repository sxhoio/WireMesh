package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/netip"
	"strings"
)

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
