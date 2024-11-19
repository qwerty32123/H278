package service

import (
	"H278/network"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
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
	responses map[string][]byte // Store responses with timestamp as key
}

type ResponseData struct {
	ClientID     int
	RequestTime  time.Time
	SubCategory  int
	PublicIP     string
	ResponseSize int
}

func NewResponseLogger(targetURL string) (*ResponseLogger, error) {
	logger := &ResponseLogger{
		clients:   make([]*network.MultiTorClient, 4),
		targetURL: targetURL,
		stopChan:  make(chan struct{}),
		responses: make(map[string][]byte),
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

	// Create data directory if it doesn't exist
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Printf("Error creating data directory: %v", err)
		return
	}

	timestamp := time.Now().Format("20060102150405.000")
	filename := fmt.Sprintf("data/response_%d_%s.bin", clientID, timestamp)

	// Save only response data to file
	if err := os.WriteFile(filename, responseData, 0644); err != nil {
		log.Printf("Error saving response to file: %v", err)
	}
	data := ResponseData{
		ClientID:     clientID,
		RequestTime:  time.Now(),
		SubCategory:  subCategory,
		PublicIP:     publicIP,
		ResponseSize: len(responseData),
	}

	// Store binary response
	l.responses[filename] = responseData

	// Save to file

	jsonData, _ := json.MarshalIndent(data, "", "  ")
	log.Printf("Request sent:\n%s\n", string(jsonData))
}
func (l *ResponseLogger) getPublicIP(client *network.MultiTorClient) string {
	resp, err := client.Circuits[0].Client.Get("https://api.ipify.org")
	if err != nil {
		return "unknown"
	}
	defer resp.Body.Close()

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

func (l *ResponseLogger) Stop() {
	close(l.stopChan)
	for _, client := range l.clients {
		if client != nil {
			client.Stop()
		}
	}
	l.waitGroup.Wait()
}
