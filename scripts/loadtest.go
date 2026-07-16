package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	targetRPS     = 200
	testDuration  = 30 * time.Second
	targetUrl     = "http://localhost:8080/v1/events"
	clientTimeout = 2 * time.Second
)

func main() {
	var (
		successCount     uint64
		rateLimitCount   uint64
		serverErrorCount uint64
		connErrorCount   uint64
	)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nLoad test interrupted by user")
		cancel()
	}()

	tickerInterval := time.Second / time.Duration(targetRPS)
	ticker := time.NewTicker(tickerInterval)
	defer ticker.Stop()

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
		Timeout: clientTimeout,
	}

	var wg sync.WaitGroup

	fmt.Printf("=========================================================\n")
	fmt.Printf("STARTING LOAD TEST: %d req/s | Duration: %s\n", targetRPS, testDuration)
	fmt.Printf("EXPECTED BEHAVIOR: 429 responses should occur if rate limits trigger.\n")
	fmt.Printf("=========================================================\n\n")

	startTime := time.Now()

Loop:
	for {
		select {
		case <-ctx.Done():
			break Loop
		case <-ticker.C:
			wg.Add(1)
			go func() {
				defer wg.Done()
				executeRequest(ctx, client, &successCount, &rateLimitCount, &serverErrorCount, &connErrorCount)
			}()
		}
	}

	wg.Wait()
	actualDuration := time.Since(startTime)

	printResults(actualDuration, successCount, rateLimitCount, serverErrorCount, connErrorCount)
}

func executeRequest(ctx context.Context, client *http.Client, successCount, rateLimitCount, serverErrCount, connErrCount *uint64) {
	jsonPayload := []byte(`{"client_id":"load","event_type":"load_test","payload":{"n":1}}`)
	req, err := http.NewRequestWithContext(ctx, "POST", targetUrl, bytes.NewBuffer(jsonPayload))

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Go-Load-Tester/1.0")

	resp, err := client.Do(req)
	if err != nil {
		atomic.AddUint64(connErrCount, 1)
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusAccepted, http.StatusOK, http.StatusCreated:
		atomic.AddUint64(successCount, 1)
	case http.StatusTooManyRequests:
		atomic.AddUint64(rateLimitCount, 1)
	case http.StatusInternalServerError:
		atomic.AddUint64(serverErrCount, 1)
	default:
		atomic.AddUint64(connErrCount, 1)
	}

}

func printResults(duration time.Duration, success, limited, serverErr, connErr uint64) {
	totalRequests := success + limited + serverErr + connErr
	actualRPS := float64(totalRequests) / duration.Seconds()

	fmt.Printf("\n-------------------- TEST RESULTS --------------------\n")
	fmt.Printf("Elapsed Time:             %.2fs\n", duration.Seconds())
	fmt.Printf("Total Requests Sent:      %d\n", totalRequests)
	fmt.Printf("Actual Send Rate:         %.2f req/s\n", actualRPS)
	fmt.Printf("\n--- Breakdowns ---\n")
	fmt.Printf("200 OK (Allowed):         %d\n", success)
	fmt.Printf("429 Rate Limited:         %d  <-- [EXPECTED RATE-LIMIT BEHAVIOR]\n", limited)
	fmt.Printf("5xx Server Errors:        %d  <-- (If > 0, system is failing under stress)\n", serverErr)
	fmt.Printf("Connection/Other Errors:  %d\n", connErr)
	fmt.Printf("------------------------------------------------------\n")
}
