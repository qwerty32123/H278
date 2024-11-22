package main

import (
	"H278/service"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Set up logging
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Create new logger
	logger, err := service.NewResponseLogger(
		"xx://xx-xx.xx.xxx.com/xxx/xx",
		"h278",    // Shared memory name
		1024*1024, // 1MB shared memory size
	)
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Stop()

	// Use as before
	logger.Start()

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for interrupt signal
	<-sigChan

	log.Println("Shutting down gracefully...")
	logger.Stop()
	log.Println("Shutdown complete")
}
