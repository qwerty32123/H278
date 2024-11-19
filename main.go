package main

import (
	"H278/service"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func bytesToHexString(bytes []byte) string {
	hexStr := strings.Builder{}
	for i, b := range bytes {
		if i > 0 {
			hexStr.WriteString(" ")
		}
		hexStr.WriteString(fmt.Sprintf("%02X", b))
	}
	return hexStr.String()
}

func main() {
	// Set up logging
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Create new logger
	logger, err := service.NewResponseLogger("https://eu-trade.naeu.playblackdesert.com/Trademarket/GetWorldMarketList")
	if err != nil {
		log.Fatal(err)
	}

	// Start the logger
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
