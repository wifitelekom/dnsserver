package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dnsdist-collector/collector"
	"dnsdist-collector/model"
)

func main() {
	socketPath := flag.String("socket", "/run/dnsdist/dnstap.sock", "Path to dnstap unix socket")
	// HTTP address for ClickHouse (e.g. 8123)
	clickhouseAddr := flag.String("clickhouse", "127.0.0.1:8123", "ClickHouse HTTP address")
	bufferSize := flag.Int("buffer", 50000, "Size of the log channel buffer")
	flag.Parse()

	log.Printf("Starting dnsdist-collector... Socket: %s, ClickHouse HTTP: %s\n", *socketPath, *clickhouseAddr)

	logChan := make(chan model.DNSLog, *bufferSize)

	// Initialize ClickHouse Writer
	writer, err := collector.NewClickHouseWriter(*clickhouseAddr, logChan)
	if err != nil {
		log.Fatalf("Failed to initialize ClickHouse writer: %v", err)
	}

	// Initialize Dnstap Listener
	listener := collector.NewDnsTapListener(*socketPath, logChan)

	// Start Writer Worker
	// We wait on writer.Done channel
	go writer.Worker()

	// Start Listener
	if err := listener.Start(); err != nil {
		log.Fatalf("Failed to start listener: %v", err)
	}
	log.Println("Listening for dnstap streams...")

	// Metrics ticker
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			dropped := listener.Dropped.Load()
			log.Printf("Metrics: Dropped=%d BufferLen=%d\n", dropped, len(logChan))
		}
	}()

	// Wait for SIGTERM or SIGINT
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigChan
	log.Printf("Received signal %v, shutting down...\n", sig)

	// Graceful Shutdown Sequence

	// 1) Stop Listener (closes socket, waits for all active handlers to finish)
	log.Println("Stopping listener...")
	listener.Stop()
	log.Println("Listener stopped.")

	// 2) Close channel (no new logs will be sent)
	close(logChan)
	log.Println("Channel closed, draining buffer...")

	// 3) Wait for Writer to drain channel and finish
	<-writer.Done
	log.Println("Writer finished. Shutdown complete.")
}
