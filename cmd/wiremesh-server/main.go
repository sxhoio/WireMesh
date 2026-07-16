package main

import (
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/wiremesh/wiremesh/internal/control"
)

func main() {
	address := os.Getenv("WIREMESH_ADDR")
	if address == "" {
		address = ":8080"
	}

	databaseDriver := envOrDefault("WIREMESH_DATABASE_DRIVER", "sqlite")
	databaseDSN := os.Getenv("WIREMESH_DATABASE_DSN")
	if databaseDSN == "" {
		if databaseDriver == "sqlite" {
			databaseDSN = "file:wiremesh.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
		} else {
			log.Fatal("WIREMESH_DATABASE_DSN is required for PostgreSQL")
		}
	}
	store, err := control.OpenSQLStore(databaseDriver, databaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer store.Close()

	app, err := control.NewApp(control.Config{
		MasterKey: os.Getenv("WIREMESH_MASTER_KEY"),
		Store:     store,
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("WireMesh control plane listening on %s", address)
	log.Printf("database driver: %s", databaseDriver)
	handler := withFrontend(app.Router(), os.Getenv("WIREMESH_WEB_DIR"))
	certFile, keyFile := os.Getenv("WIREMESH_TLS_CERT_FILE"), os.Getenv("WIREMESH_TLS_KEY_FILE")
	if certFile != "" && keyFile != "" {
		server := &http.Server{Addr: address, Handler: handler, TLSConfig: app.AgentTLSConfig()}
		log.Printf("agent mTLS verification enabled")
		if err := server.ListenAndServeTLS(certFile, keyFile); err != nil {
			log.Fatal(err)
		}
		return
	}
	log.Printf("WARNING: HTTP mode enables the development X-Agent-ID adapter; configure TLS for production")
	if err := http.ListenAndServe(address, handler); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func withFrontend(api http.Handler, directory string) http.Handler {
	if directory == "" {
		return api
	}
	files := http.FileServer(http.Dir(directory))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/agent/") {
			api.ServeHTTP(w, r)
			return
		}
		relative := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		candidate := filepath.Join(directory, filepath.FromSlash(relative))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(directory, "index.html"))
	})
}
