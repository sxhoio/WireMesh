package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestFetchPublicIPv4AcceptsPlainText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("203.0.113.9\n"))
	}))
	defer server.Close()

	address, err := fetchPublicIPv4(context.Background(), server.Client(), []string{server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if address != "203.0.113.9" {
		t.Fatalf("unexpected public IPv4: %q", address)
	}
}

func TestFetchPublicIPv4AcceptsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ip":"203.0.113.10"}`))
	}))
	defer server.Close()

	address, err := fetchPublicIPv4(context.Background(), server.Client(), []string{server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if address != "203.0.113.10" {
		t.Fatalf("unexpected JSON public IPv4: %q", address)
	}
}

func TestFetchPublicIPv4RejectsPrivateAnswers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("192.168.1.20"))
	}))
	defer server.Close()

	if _, err := fetchPublicIPv4(context.Background(), server.Client(), []string{server.URL}); err == nil {
		t.Fatal("a private address must not be accepted as the public IPv4")
	}
}

func TestFetchPublicIPv4FallsBackToNextEndpoint(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer broken.Close()
	working := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("203.0.113.11"))
	}))
	defer working.Close()

	address, err := fetchPublicIPv4(context.Background(), working.Client(), []string{broken.URL, working.URL})
	if err != nil {
		t.Fatal(err)
	}
	if address != "203.0.113.11" {
		t.Fatalf("failover endpoint was not used: %q", address)
	}
}

func TestPublicIPv4URLsOverride(t *testing.T) {
	if got := publicIPv4URLs(""); len(got) == 0 {
		t.Fatal("default public IPv4 endpoints must be configured")
	}
	got := publicIPv4URLs(" https://one.example/ip ,,https://two.example/ip ")
	if len(got) != 2 || got[0] != "https://one.example/ip" || got[1] != "https://two.example/ip" {
		t.Fatalf("override URLs were not parsed: %v", got)
	}
	if got := publicIPv4URLs(" , "); len(got) != 0 {
		t.Fatalf("blank override must produce an empty list: %v", got)
	}
}

func TestParsePublicIPv4ResponseRejectsIPv6(t *testing.T) {
	if _, err := parsePublicIPv4Response(strings.NewReader("2001:4860:4860::8888")); err == nil {
		t.Fatal("IPv6 answers are rejected until the server resolves them with GeoIP")
	}
}
