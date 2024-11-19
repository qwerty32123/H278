// Package tests IpTest.go
package tests

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/cretz/bine/tor"
	"golang.org/x/net/context"
)

func TestTorIPCheck(t *testing.T) {
	// Start tor with default config
	fmt.Println("Starting Tor...")
	torInstance, err := tor.Start(nil, nil)
	if err != nil {
		t.Fatalf("Failed to start Tor: %v", err)
	}
	defer torInstance.Close()

	// Create a Tor-enabled HTTP client
	dialCtx := context.Background()
	dialer, err := torInstance.Dialer(dialCtx, nil)
	if err != nil {
		t.Fatalf("Failed to create Tor dialer: %v", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
		},
	}

	// Start timing
	start := time.Now()

	// Make the request to get public IP
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Read the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	// Calculate elapsed time
	elapsed := time.Since(start)

	fmt.Printf("Public IP: %s\n", string(body))
	fmt.Printf("Time taken: %d ms\n", elapsed.Milliseconds())

	// Add some basic assertions
	if len(body) == 0 {
		t.Error("Expected non-empty IP address")
	}

	if elapsed > 30*time.Second {
		t.Error("Request took too long (more than 30 seconds)")
	}
}
