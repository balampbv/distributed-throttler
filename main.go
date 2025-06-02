package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"throttler/throttler"
)

func main() {
	// Create a new throttler
	t, err := throttler.New(throttler.Config{
		RedisAddr: "localhost:6379", // Make sure Redis is running (use docker-compose up)
	})
	if err != nil {
		log.Fatalf("Failed to create throttler: %v", err)
	}
	defer t.Close()

	// Example 1: Basic throttling
	fmt.Println("Example 1: Basic throttling")
	demoBasicThrottling(t)

	// Example 2: High throughput demonstration
	fmt.Println("\nExample 2: High throughput demonstration")
	demoHighThroughput(t)

	// Example 3: Waiting for rate limit to be available
	fmt.Println("\nExample 3: Waiting for rate limit to be available")
	demoWaitForRateLimit(t)
}

// demoBasicThrottling demonstrates basic throttling functionality
func demoBasicThrottling(t *throttler.Throttler) {
	ctx := context.Background()
	key := "demo-key"
	limit := int64(5)             // Allow 5 operations
	window := 5 * time.Second     // In a 5-second window

	fmt.Printf("Throttling: %d requests per %v\n", limit, window)

	// Try 10 operations
	for i := 1; i <= 10; i++ {
		allowed, err := t.Allow(ctx, key, limit, window)
		if err != nil {
			log.Printf("Error checking throttle: %v", err)
			continue
		}

		if allowed {
			fmt.Printf("Request %d: Allowed ✅\n", i)
		} else {
			fmt.Printf("Request %d: Throttled ❌\n", i)
		}

		// Small delay between requests for demonstration
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Println("Waiting for the throttle window to expire...")
	time.Sleep(window)

	// Try one more request after the window
	allowed, err := t.Allow(ctx, key, limit, window)
	if err != nil {
		log.Printf("Error checking throttle: %v", err)
	} else if allowed {
		fmt.Println("Request after window: Allowed ✅")
	} else {
		fmt.Println("Request after window: Throttled ❌")
	}
}

// demoHighThroughput demonstrates high throughput capabilities
func demoHighThroughput(t *throttler.Throttler) {
	ctx := context.Background()

	// Target: Demonstrate a portion of 500k operations per minute
	duration := 2 * time.Second // Run for 2 seconds
	numWorkers := 10            // Number of concurrent workers

	fmt.Printf("Running high throughput test with %d workers for %v\n", numWorkers, duration)

	var wg sync.WaitGroup
	var counter int64
	var mu sync.Mutex

	// Start time
	startTime := time.Now()
	endTime := startTime.Add(duration)

	// Create multiple workers to simulate high load
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			key := fmt.Sprintf("high-throughput-demo-%d", workerID)
			limit := int64(100000) // High limit to avoid throttling
			window := time.Minute

			localCounter := 0
			for time.Now().Before(endTime) {
				_, err := t.Allow(ctx, key, limit, window)
				if err != nil {
					log.Printf("Worker %d error: %v", workerID, err)
					continue
				}
				localCounter++
			}

			mu.Lock()
			counter += int64(localCounter)
			mu.Unlock()
		}(w)
	}

	wg.Wait()
	elapsed := time.Since(startTime)

	opsPerSecond := float64(counter) / elapsed.Seconds()
	opsPerMinute := opsPerSecond * 60

	fmt.Printf("Performed %d operations in %v\n", counter, elapsed)
	fmt.Printf("Throughput: %.2f ops/second (%.2f ops/minute)\n", opsPerSecond, opsPerMinute)
	fmt.Printf("Projected to 500k ops/minute: %.2f%% of target\n", (opsPerMinute/500000)*100)
}

// demoWaitForRateLimit demonstrates the waiting functionality
func demoWaitForRateLimit(t *throttler.Throttler) {
	ctx := context.Background()
	key := "wait-demo-key"
	limit := int64(5)         // Allow 5 operations
	window := 5 * time.Second // In a 5-second window

	fmt.Printf("Throttling with waiting: %d requests per %v\n", limit, window)

	// First, clear any existing data for this key
	err := t.ClearKey(ctx, key)
	if err != nil {
		log.Printf("Error clearing key: %v", err)
	}

	// Perform operations with waiting
	for i := 1; i <= 10; i++ {
		startTime := time.Now()

		// Create a context with timeout to prevent waiting indefinitely
		timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)

		fmt.Printf("Request %d: Attempting... ", i)
		allowed, err := t.AllowWithWait(timeoutCtx, key, limit, window)
		cancel() // Always cancel the context to prevent resource leaks

		elapsed := time.Since(startTime)

		if err != nil {
			if err == context.DeadlineExceeded {
				fmt.Printf("Timed out after %v ⏱️\n", elapsed)
			} else {
				fmt.Printf("Error: %v ❌\n", err)
			}
		} else if allowed {
			fmt.Printf("Allowed after %v ✅\n", elapsed)
		} else {
			fmt.Printf("Denied after %v ❌\n", elapsed)
		}

		// Small delay between requests for demonstration
		time.Sleep(100 * time.Millisecond)
	}
}
