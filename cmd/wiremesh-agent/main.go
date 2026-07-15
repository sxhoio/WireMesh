package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type enrollmentRequest struct {
	Token, Name, Endpoint, Region, OS, AgentVersion string
	Labels                                          map[string]string
}

type enrollmentResponse struct {
	Node struct {
		ID string `json:"id"`
	} `json:"node"`
}

func main() {
	server := flag.String("server", "http://localhost:8080", "WireMesh control plane URL")
	enrollToken := flag.String("enroll-token", "", "one-time enrollment token")
	nodeID := flag.String("node-id", "", "existing node identity")
	name := flag.String("name", "", "node name used during enrollment")
	flag.Parse()

	client := &http.Client{Timeout: 15 * time.Second}
	if *enrollToken != "" {
		if *name == "" {
			fmt.Fprintln(os.Stderr, "-name is required with -enroll-token")
			os.Exit(2)
		}
		body, _ := json.Marshal(enrollmentRequest{Token: *enrollToken, Name: *name, OS: "unknown", AgentVersion: "0.1.0", Labels: map[string]string{}})
		response, err := client.Post(*server+"/agent/v1/enroll", "application/json", bytes.NewReader(body))
		if err != nil {
			panic(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusCreated {
			fail(response.Body)
		}
		var enrolled enrollmentResponse
		_ = json.NewDecoder(response.Body).Decode(&enrolled)
		fmt.Printf("enrolled node %s\n", enrolled.Node.ID)
		fmt.Println("persist the returned certificate material before switching to mTLS transport")
		return
	}
	if *nodeID == "" {
		fmt.Fprintln(os.Stderr, "provide -enroll-token or -node-id")
		os.Exit(2)
	}
	request, _ := http.NewRequest(http.MethodGet, *server+"/agent/v1/config", nil)
	request.Header.Set("X-Agent-ID", *nodeID) // Development adapter; production transport uses mTLS.
	response, err := client.Do(request)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fail(response.Body)
	}
	data, _ := io.ReadAll(response.Body)
	fmt.Println(string(data))
}

func fail(body io.Reader) {
	data, _ := io.ReadAll(body)
	fmt.Fprintln(os.Stderr, string(data))
	os.Exit(1)
}
