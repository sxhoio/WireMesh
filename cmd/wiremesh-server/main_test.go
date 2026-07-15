package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithFrontend(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("wiremesh console"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "assets", "app.js"), []byte("console.log('ok')"), 0o600); err != nil {
		t.Fatal(err)
	}

	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	handler := withFrontend(api, directory)

	for _, route := range []string{"/healthz", "/api/v1/projects", "/agent/v1/config"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		if response.Code != http.StatusAccepted {
			t.Fatalf("%s reached frontend instead of API: %d", route, response.Code)
		}
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/networks/example", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "wiremesh console") {
		t.Fatalf("SPA fallback failed: %d %q", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset caching failed: %d %q", response.Code, response.Header().Get("Cache-Control"))
	}
}
