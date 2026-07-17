package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchAgentLocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent/v1/location" || r.Header.Get("X-Agent-ID") != "node-location" {
			t.Fatalf("unexpected location request: %s agent=%q", r.URL.Path, r.Header.Get("X-Agent-ID"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"public_ip":"203.0.113.8","location_name":"中国 · 广东 · 广州","location_source":"geoip","latitude":23.1291,"longitude":113.2644}`))
	}))
	defer server.Close()

	location, err := fetchAgentLocation(context.Background(), server.Client(), agentState{NodeID: "node-location", Server: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if location.PublicIP != "203.0.113.8" || location.LocationSource != "geoip" || location.Latitude != 23.1291 || location.Longitude != 113.2644 {
		t.Fatalf("unexpected location: %#v", location)
	}
}

func TestFetchAgentLocationKeepsPublicIPWhenGeoIPIsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"public_ip":"198.51.100.10"}`))
	}))
	defer server.Close()

	location, err := fetchAgentLocation(context.Background(), server.Client(), agentState{NodeID: "node-location", Server: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if location.PublicIP != "198.51.100.10" || location.LocationSource != "" {
		t.Fatalf("unexpected unknown location response: %#v", location)
	}
}

func TestFetchAgentLocationDropsInvalidAutomaticCoordinates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"public_ip":"198.51.100.20","location_name":"未知位置","location_source":"geoip","latitude":0,"longitude":0}`))
	}))
	defer server.Close()

	location, err := fetchAgentLocation(context.Background(), server.Client(), agentState{NodeID: "node-location", Server: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if location.PublicIP != "198.51.100.20" || location.LocationSource != "" || location.LocationName != "" {
		t.Fatalf("invalid automatic location was retained: %#v", location)
	}
}
