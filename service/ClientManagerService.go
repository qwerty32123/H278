package service

import (
	"H278/Memory/Windows" // Add this import for your shared memory package
	"H278/network"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"
)

type ResponseLogger struct {
	clients   []*network.MultiTorClient
	targetURL string
	stopChan  chan struct{}
	waitGroup sync.WaitGroup
	logMutex  sync.Mutex
	responses map[string][]byte
	sharedMem *Windows.SharedMemoryClient
}

type ResponseData struct {
	ClientID     int
	RequestTime  time.Time
	SubCategory  int
	PublicIP     string
	ResponseSize int
}

func NewResponseLogger(targetURL string, sharedMemName string, sharedMemSize int) (*ResponseLogger, error) {
	// Initialize shared memory
	sharedMem, err := Windows.NewSharedMemoryClient(sharedMemName, sharedMemSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create shared memory client: %v", err)
	}

	logger := &ResponseLogger{
		clients:   make([]*network.MultiTorClient, 4),
		targetURL: targetURL,
		stopChan:  make(chan struct{}),
		responses: make(map[string][]byte),
		sharedMem: sharedMem,
	}

	for i := 0; i < 4; i++ {
		client, err := network.NewMultiTorClient(1)
		if err != nil {
			logger.Stop()
			return nil, err
		}
		client.Start(time.Second * 5)
		logger.clients[i] = client
	}

	return logger, nil
}

func (l *ResponseLogger) logRequest(clientID int, subCategory int, publicIP string, responseData []byte) {
	l.logMutex.Lock()
	defer l.logMutex.Unlock()

	data := ResponseData{
		ClientID:     clientID,
		RequestTime:  time.Now(),
		SubCategory:  subCategory,
		PublicIP:     publicIP,
		ResponseSize: len(responseData),
	}

	// Convert metadata to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Printf("Error marshaling response data: %v", err)
		return
	}

	if err := l.sharedMem.WriteData(responseData); err != nil {
		log.Printf("Error writing to shared memory: %v", err)
		return
	}

	log.Printf("Request sent:\n%s\n", string(jsonData))
}

func (l *ResponseLogger) Stop() {
	close(l.stopChan)
	for _, client := range l.clients {
		if client != nil {
			client.Stop()
		}
	}
	if l.sharedMem != nil {
		l.sharedMem.Close()
	}
	l.waitGroup.Wait()
}

func (l *ResponseLogger) getPublicIP(client *network.MultiTorClient) string {
	resp, err := client.Circuits[0].Client.Get("https://api.ipify.org")
	if err != nil {
		return "unknown"
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {

		}
	}(resp.Body)

	ip, err := io.ReadAll(resp.Body)
	if err != nil {
		return "unknown"
	}
	return string(ip)
}

func (l *ResponseLogger) runClientWithLogging(index int) {
	defer l.waitGroup.Done()

	rSource := rand.NewSource(time.Now().UnixNano())
	r := rand.New(rSource)
	client := l.clients[index]

	for {
		select {
		case <-l.stopChan:
			return
		default:
			delay := time.Duration(600+r.Intn(201)) * time.Millisecond

			subCategory := index + 1
			jsonBody := fmt.Sprintf(`{
                "keyType": 0,
                "mainCategory": 55,
                "subCategory": %d
            }`, subCategory)

			req := network.Request{
				URL:    l.targetURL,
				Method: "POST",
				Body:   jsonBody,
				Headers: map[string]string{
					"Connection":   "keep-alive",
					"User-Agent":   "BlackDesert",
					"Content-Type": "application/json",
				},
			}

			// Get public IP before making request
			publicIP := l.getPublicIP(client)

			// Make the request
			resp, err := client.Circuits[0].Client.Post(req.URL, req.Headers["Content-Type"], strings.NewReader(req.Body))
			if err != nil {
				log.Printf("Client %d error: %v", index, err)
				time.Sleep(delay)
				continue
			}

			// Read response body
			responseData, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				log.Printf("Client %d error reading response: %v", index, err)
				time.Sleep(delay)
				continue
			}

			l.logRequest(index, subCategory, publicIP, responseData)
			time.Sleep(delay)
		}
	}
}
func (l *ResponseLogger) Start() {
	for i := range l.clients {
		l.waitGroup.Add(1)
		go l.runClientWithLogging(i)
	}
}
