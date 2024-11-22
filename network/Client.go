package network

import (
	"context"
	"fmt"
	"github.com/cretz/bine/tor"
	"golang.org/x/net/proxy"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type TorCircuit struct {
	ID          int
	torInstance *tor.Tor
	dialer      proxy.Dialer
	Client      *http.Client
	currentIP   string
	mu          sync.RWMutex
}

type MultiTorClient struct {
	Circuits    []*TorCircuit
	mu          sync.RWMutex
	requestChan chan Request
	stopChan    chan struct{}
}

type Request struct {
	URL     string
	Body    string
	Method  string
	Headers map[string]string
}

// NewMultiTorClient creates a new instance with specified number of circuits
func NewMultiTorClient(circuitCount int) (*MultiTorClient, error) {
	if circuitCount <= 0 {
		return nil, fmt.Errorf("invalid circuit count: %d, must be positive", circuitCount)
	}

	client := &MultiTorClient{
		Circuits:    make([]*TorCircuit, circuitCount),
		requestChan: make(chan Request, circuitCount*2),
		stopChan:    make(chan struct{}),
	}

	var wg sync.WaitGroup
	errChan := make(chan error, circuitCount)

	for i := 0; i < circuitCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			circuit, err := initCircuit(idx)
			if err != nil {
				errChan <- fmt.Errorf("circuit %d init error: %v", idx, err)
				return
			}
			client.Circuits[idx] = circuit
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			// Clean up any initialized circuits
			for _, circuit := range client.Circuits {
				if circuit != nil && circuit.torInstance != nil {
					err := circuit.torInstance.Close()
					if err != nil {
						return nil, err
					}
				}
			}
			return nil, err
		}
	}

	return client, nil
}

func initCircuit(id int) (*TorCircuit, error) {
	// Start tor with default config
	t, err := tor.Start(nil, nil)
	if err != nil {
		return nil, err
	}

	// Wait at most a minute to start network and get
	dialCtx, dialCancel := context.WithTimeout(context.Background(), time.Minute)
	defer dialCancel()

	// Create SOCKS5 dialer
	dialer, err := t.Dialer(dialCtx, nil)
	if err != nil {
		return nil, err
	}

	// Create HTTP client - fixed to use dialer directly
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
		},
	}

	circuit := &TorCircuit{
		ID:          id,
		torInstance: t,
		dialer:      dialer,
		Client:      httpClient,
	}

	// Get initial IP
	if err := circuit.updateIP(); err != nil {
		return nil, err
	}

	return circuit, nil
}

// Start begins processing requests with specified interval
func (m *MultiTorClient) Start(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-m.stopChan:
				return
			case req := <-m.requestChan:
				// Process request concurrently for each circuit
				for _, circuit := range m.Circuits {
					go func(c *TorCircuit, r Request) {
						if err := c.MakeRequest(r); err != nil {
							log.Printf("Circuit %d request error: %v", c.ID, err)
						}
					}(circuit, req)
				}
			case <-ticker.C:
				// Rotate IPs periodically
				for _, circuit := range m.Circuits {
					go func(c *TorCircuit) {
						if err := c.RotateIP(); err != nil {
							log.Printf("Circuit %d IP rotation error: %v", c.ID, err)
						}
					}(circuit)
				}
			}
		}
	}()
}

// Stop terminates all circuits and stops processing requests
func (m *MultiTorClient) Stop() {
	close(m.stopChan)
	for _, circuit := range m.Circuits {
		if err := circuit.torInstance.Close(); err != nil {
			log.Printf("Error closing circuit %d: %v", circuit.ID, err)
		}
	}
}

// EnqueueRequest adds a new request to the processing queue
func (m *MultiTorClient) EnqueueRequest(req Request) {
	m.requestChan <- req
}

// RotateIP creates a new circuit for specified ID
func (c *TorCircuit) RotateIP() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create new SOCKS5 dialer
	dialCtx, dialCancel := context.WithTimeout(context.Background(), time.Minute)
	defer dialCancel()

	dialer, err := c.torInstance.Dialer(dialCtx, nil)
	if err != nil {
		return err
	}

	// Update client with new dialer
	c.dialer = dialer

	// Create HTTP client with the dialer without type assertion
	c.Client = &http.Client{
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
		},
	}

	return c.updateIP()
}

// MakeRequest performs HTTP request through Tor circuit
func (c *TorCircuit) MakeRequest(req Request) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	httpReq, err := http.NewRequest(req.Method, req.URL, strings.NewReader(req.Body))
	if err != nil {
		return err
	}

	// Add headers
	for k, v := range req.Headers {
		httpReq.Header.Add(k, v)
	}

	resp, err := c.Client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Read response
	_, err = io.ReadAll(resp.Body)
	return err
}

// updateIP gets and stores current IP address
func (c *TorCircuit) updateIP() error {
	resp, err := c.Client.Get("https://api.ipify.org")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	ip, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	c.currentIP = string(ip)
	return nil
}

// GetCurrentIP returns current IP address for this circuit
func (c *TorCircuit) GetCurrentIP() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentIP
}
