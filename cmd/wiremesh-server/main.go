package main

import (
	"log"
	"net/http"
	"os"

	"github.com/wiremesh/wiremesh/internal/control"
)

func main() {
	address := os.Getenv("WIREMESH_ADDR")
	if address == "" {
		address = ":8080"
	}

	app, err := control.NewApp(control.Config{MasterKey: os.Getenv("WIREMESH_MASTER_KEY")})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("WireMesh control plane listening on %s", address)
	log.Printf("development login: admin@wiremesh.local / wiremesh-dev")
	certFile, keyFile := os.Getenv("WIREMESH_TLS_CERT_FILE"), os.Getenv("WIREMESH_TLS_KEY_FILE")
	if certFile != "" && keyFile != "" {
		server := &http.Server{Addr: address, Handler: app.Router(), TLSConfig: app.AgentTLSConfig()}
		log.Printf("agent mTLS verification enabled")
		if err := server.ListenAndServeTLS(certFile, keyFile); err != nil {
			log.Fatal(err)
		}
		return
	}
	log.Printf("WARNING: HTTP mode enables the development X-Agent-ID adapter; configure TLS for production")
	if err := http.ListenAndServe(address, app.Router()); err != nil {
		log.Fatal(err)
	}
}
