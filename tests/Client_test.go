package tests

import (
	"H278/network"
	"net/http"
	"testing"
	"time"
)

func TestNewMultiTorClient(t *testing.T) {
	tests := []struct {
		name         string
		circuitCount int
		wantErr      bool
	}{
		{"valid count", 2, false},
		{"zero circuits", 0, true},
		{"negative count", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil && !tt.wantErr {
					t.Errorf("NewMultiTorClient() panicked: %v", r)
				}
			}()

			client, err := network.NewMultiTorClient(tt.circuitCount)
			if tt.wantErr {
				if err == nil {
					t.Error("NewMultiTorClient() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("NewMultiTorClient() unexpected error: %v", err)
				return
			}
			if len(client.Circuits) != tt.circuitCount {
				t.Errorf("NewMultiTorClient() got %d circuits, want %d", len(client.Circuits), tt.circuitCount)
			}
		})
	}
}
func TestCircuitRotation(t *testing.T) {
	client, err := network.NewMultiTorClient(1)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Stop()

	circuit := client.Circuits[0]
	initialIP := circuit.GetCurrentIP()

	if err := circuit.RotateIP(); err != nil {
		t.Fatalf("Failed to rotate IP: %v", err)
	}

	newIP := circuit.GetCurrentIP()
	if initialIP == newIP {
		t.Error("IP did not change after rotation")
	}
}

func TestRequestProcessing(t *testing.T) {
	client, err := network.NewMultiTorClient(1)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Stop()

	client.Start(time.Second * 5)

	req := network.Request{
		URL:     "https://api.ipify.org",
		Method:  http.MethodGet,
		Headers: map[string]string{"User-Agent": "Test Client"},
	}

	client.EnqueueRequest(req)
	// Allow time for request processing
	time.Sleep(time.Second * 2)
}

func TestConcurrentRequests(t *testing.T) {
	client, err := network.NewMultiTorClient(2)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Stop()

	client.Start(time.Second * 5)

	requests := []network.Request{
		{URL: "https://api.ipify.org", Method: http.MethodGet},
		{URL: "https://api.ipify.org", Method: http.MethodGet},
	}

	for _, req := range requests {
		client.EnqueueRequest(req)
	}

	// Allow time for concurrent processing
	time.Sleep(time.Second * 3)
}
func TestErrorHandling(t *testing.T) {
	tests := []struct {
		name    string
		request network.Request
		wantErr bool
	}{
		{
			name: "invalid URL",
			request: network.Request{
				URL:    "invalid-url",
				Method: http.MethodGet,
			},
			wantErr: true,
		},
		{
			name: "unreachable host",
			request: network.Request{
				URL:    "http://this-domain-definitely-does-not-exist.com",
				Method: http.MethodGet,
			},
			wantErr: true,
		},
	}

	client, err := network.NewMultiTorClient(1)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Stop()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.Circuits[0].MakeRequest(tt.request)
			if (err != nil) != tt.wantErr {
				t.Errorf("makeRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
