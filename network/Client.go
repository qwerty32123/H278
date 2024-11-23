package network

import (
	"context"
	"fmt"
	"github.com/cretz/bine/tor"
	"golang.org/x/net/proxy"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
	closed      bool
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
	// Find available ports
	basePort := 9050 + (id * 10) // More spacing between ports
	var dnsPort, socksPort int

	// Find first available pair of ports
	for i := 0; i < 100; i++ { // Try up to 100 port combinations
		testSocksPort := basePort + i
		testDnsPort := basePort + i + 1

		if isPortAvailable(testSocksPort) && isPortAvailable(testDnsPort) {
			socksPort = testSocksPort
			dnsPort = testDnsPort
			break
		}
	}

	if socksPort == 0 || dnsPort == 0 {
		return nil, fmt.Errorf("could not find available ports for circuit %d", id)
	}

	// Create data directory for this circuit
	dataDir, err := os.MkdirTemp("", fmt.Sprintf("tor-circuit-%d-", id))
	if err != nil {
		return nil, fmt.Errorf("failed to create data directory: %v", err)
	}

	// Start tor with improved configuration
	conf := &tor.StartConf{
		ProcessCreator: nil,
		DebugWriter:    nil,
		NoHush:         false,
		DataDir:        dataDir, // Separate data directory for each instance
		ExtraArgs: []string{
			"--DNSPort", fmt.Sprintf("%d", dnsPort),
			"--SocksPort", fmt.Sprintf("%d", socksPort),
			"--AutomapHostsOnResolve", "1",
			"--AutomapHostsSuffixes", ".exit,.onion",
			"--GeoIPFile", filepath.Join(dataDir, "geoip"), // Set explicit paths
			"--GeoIPv6File", filepath.Join(dataDir, "geoip6"),
			"--DataDirectory", dataDir, // Explicit data directory
			"--PidFile", filepath.Join(dataDir, "pid"), // Separate PID file
			"--CircuitBuildTimeout", "30", // Faster circuit building
			"--LearnCircuitBuildTimeout", "0", // Disable automatic timing learning
		},
	}

	// Create logger for this circuit
	logFile, err := os.CreateTemp("", fmt.Sprintf("tor-circuit-%d-*.log", id))
	if err != nil {
		os.RemoveAll(dataDir)
		return nil, fmt.Errorf("failed to create log file: %v", err)
	}
	conf.DebugWriter = logFile

	t, err := tor.Start(nil, conf)
	if err != nil {
		os.RemoveAll(dataDir)
		logFile.Close()
		return nil, fmt.Errorf("failed to start tor: %v", err)
	}

	// Wait for the tor instance to be fully ready
	time.Sleep(time.Second * 5) // Give Tor time to initialize

	dialCtx, dialCancel := context.WithTimeout(context.Background(), time.Minute)
	defer dialCancel()

	// Create SOCKS5 dialer
	dialer, err := t.Dialer(dialCtx, nil)
	if err != nil {
		os.RemoveAll(dataDir)
		logFile.Close()
		t.Close()
		return nil, fmt.Errorf("failed to create dialer: %v", err)
	}

	transport := &http.Transport{
		DialContext:       dialer.DialContext,
		DisableKeepAlives: true,
		ForceAttemptHTTP2: false,
		Proxy:             nil,
		IdleConnTimeout:   30 * time.Second,
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   time.Minute * 2,
	}

	circuit := &TorCircuit{
		ID:          id,
		torInstance: t,
		dialer:      dialer,
		Client:      httpClient,
	}

	// Add retry logic with longer initial wait
	maxRetries := 5 // Increased retries
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		// Exponential backoff
		backoff := time.Second * time.Duration(math.Pow(2, float64(i)))
		time.Sleep(backoff)

		if err := circuit.verifyTorConnection(); err != nil {
			lastErr = err
			log.Printf("Circuit %d: Verification attempt %d failed: %v", id, i+1, err)
			continue
		}
		log.Printf("Circuit %d: Successfully verified Tor connection", id)
		lastErr = nil
		break
	}

	if lastErr != nil {
		os.RemoveAll(dataDir)
		logFile.Close()
		t.Close()
		return nil, fmt.Errorf("failed to verify tor connection after %d attempts: %v", maxRetries, lastErr)
	}

	// Get initial IP with retry
	maxIPRetries := 3
	for i := 0; i < maxIPRetries; i++ {
		if err := circuit.getCurrentIP(); err != nil {
			log.Printf("Circuit %d: IP fetch attempt %d failed: %v", id, i+1, err)
			time.Sleep(time.Second * 2)
			continue
		}
		log.Printf("Circuit %d: Successfully obtained IP: %s", id, circuit.GetCurrentIP())
		break
	}

	return circuit, nil
}

// Helper function to check port availability
func isPortAvailable(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// Add cleanup method to TorCircuit
func (c *TorCircuit) Cleanup() {
	if c.torInstance != nil {
		dataDir := c.torInstance.DataDir
		c.Close()
		if dataDir != "" {
			os.RemoveAll(dataDir)
		}
	}
}

// verifyTorConnection checks if we're actually using Tor
func (c *TorCircuit) verifyTorConnection() error {
	resp, err := c.Client.Get("https://check.torproject.org/api/ip")
	if err != nil {
		return fmt.Errorf("failed to check tor connection: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	if !strings.Contains(string(body), `"IsTor":true`) {
		return fmt.Errorf("connection is not using tor network")
	}

	return nil
}

// getCurrentIP gets and stores current IP address
func (c *TorCircuit) getCurrentIP() error {
	resp, err := c.Client.Get("https://api.ipify.org")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	ip, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.currentIP = string(ip)
	c.mu.Unlock()
	return nil
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

// RotateIP creates a new circuit with DNS leak protection
func (c *TorCircuit) RotateIP() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), time.Minute)
	defer dialCancel()

	// Create new SOCKS5 dialer
	dialer, err := c.torInstance.Dialer(dialCtx, nil)
	if err != nil {
		return fmt.Errorf("failed to create new dialer: %v", err)
	}

	// Update client with new secure transport
	transport := &http.Transport{
		DialContext:       dialer.DialContext,
		DisableKeepAlives: true,
		ForceAttemptHTTP2: false,
	}

	c.dialer = dialer
	c.Client = &http.Client{
		Transport: transport,
		Timeout:   time.Minute * 2,
	}

	// Verify the new circuit
	if err := c.verifyTorConnection(); err != nil {
		return fmt.Errorf("failed to verify new circuit: %v", err)
	}

	return c.getCurrentIP()
}
func (c *TorCircuit) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	// Cancel any pending requests
	if c.Client != nil && c.Client.Transport != nil {
		if transport, ok := c.Client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}

	// Close Tor instance
	if c.torInstance != nil {
		if err := c.torInstance.Close(); err != nil {
			return fmt.Errorf("failed to close tor instance: %v", err)
		}
	}

	c.closed = true
	return nil
}

// MakeRequest performs HTTP request through Tor circuit
func (c *TorCircuit) MakeRequest(req Request) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("circuit is closed")
	}
	c.mu.RUnlock()

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

func (m *MultiTorClient) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	close(m.stopChan)

	for _, circuit := range m.Circuits {
		if circuit != nil {
			circuit.Cleanup()
		}
	}

	close(m.requestChan)
	for range m.requestChan {
		// Drain remaining requests
	}
}
func (m *MultiTorClient) VerifyAllCircuits() error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(m.Circuits))

	for _, circuit := range m.Circuits {
		wg.Add(1)
		go func(c *TorCircuit) {
			defer wg.Done()
			if err := c.verifyTorConnection(); err != nil {
				errChan <- fmt.Errorf("circuit %d verification failed: %v", c.ID, err)
			}
		}(circuit)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}

// GetCurrentIP returns current IP address for this circuit
func (c *TorCircuit) GetCurrentIP() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentIP
}
