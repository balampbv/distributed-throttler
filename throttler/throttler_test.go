package throttler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestThrottler_Allow(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// Create a new throttler
	throttler, err := New(Config{
		RedisAddr: "localhost:6379",
	})
	if err != nil {
		t.Fatalf("Failed to create throttler: %v", err)
	}
	defer throttler.Close()

	ctx := context.Background()
	key := "test-key"
	limit := int64(10)
	window := time.Second

	// Clear any existing data for the test key
	redisKey := fmt.Sprintf("%s:%s", throttler.keyPrefix, key)
	throttler.redisClient.Del(ctx, redisKey)

	// First 10 requests should be allowed
	for i := 0; i < 10; i++ {
		allowed, err := throttler.Allow(ctx, key, limit, window)
		if err != nil {
			t.Fatalf("Failed to check if allowed: %v", err)
		}
		if !allowed {
			t.Errorf("Expected request %d to be allowed, but it was not", i+1)
		}
	}

	// 11th request should be throttled
	allowed, err := throttler.Allow(ctx, key, limit, window)
	if err != nil {
		t.Fatalf("Failed to check if allowed: %v", err)
	}
	if allowed {
		t.Error("Expected request to be throttled, but it was allowed")
	}

	// Wait for the window to pass
	time.Sleep(window + 100*time.Millisecond)

	// After the window, requests should be allowed again
	allowed, err = throttler.Allow(ctx, key, limit, window)
	if err != nil {
		t.Fatalf("Failed to check if allowed: %v", err)
	}
	if !allowed {
		t.Error("Expected request to be allowed after window, but it was throttled")
	}
}

func BenchmarkThrottler_Allow(b *testing.B) {
	// Skip if not running integration tests
	if testing.Short() {
		b.Skip("Skipping integration benchmark")
	}

	// Create a new throttler
	throttler, err := New(Config{
		RedisAddr: "localhost:6379",
	})
	if err != nil {
		b.Fatalf("Failed to create throttler: %v", err)
	}
	defer throttler.Close()

	ctx := context.Background()
	key := "benchmark-key"
	limit := int64(b.N) // Allow all operations to measure pure performance
	window := time.Minute

	// Clear any existing data for the benchmark key
	redisKey := fmt.Sprintf("%s:%s", throttler.keyPrefix, key)
	throttler.redisClient.Del(ctx, redisKey)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := throttler.Allow(ctx, key, limit, window)
		if err != nil {
			b.Fatalf("Failed to check if allowed: %v", err)
		}
	}
}

func BenchmarkThrottler_Allow_Parallel(b *testing.B) {
	// Skip if not running integration tests
	if testing.Short() {
		b.Skip("Skipping integration benchmark")
	}

	// Create a new throttler
	throttler, err := New(Config{
		RedisAddr: "localhost:6379",
	})
	if err != nil {
		b.Fatalf("Failed to create throttler: %v", err)
	}
	defer throttler.Close()

	ctx := context.Background()
	key := "benchmark-parallel-key"
	limit := int64(b.N * 2) // Allow all operations to measure pure performance
	window := time.Minute

	// Clear any existing data for the benchmark key
	redisKey := fmt.Sprintf("%s:%s", throttler.keyPrefix, key)
	throttler.redisClient.Del(ctx, redisKey)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := throttler.Allow(ctx, key, limit, window)
			if err != nil {
				b.Fatalf("Failed to check if allowed: %v", err)
			}
		}
	})
}

// BenchmarkHighThroughput simulates a high throughput scenario
// to test if the throttler can handle 500k operations per minute
func BenchmarkHighThroughput(b *testing.B) {
	// Skip if not running integration tests
	if testing.Short() {
		b.Skip("Skipping high throughput benchmark")
	}

	// Create a new throttler with auto-tuning enabled
	// to achieve the exact target of 500k ops/minute
	throttler, err := New(Config{
		RedisAddr:       "localhost:6379",
		OverheadFactor:  20.0, // Initial overhead factor
		AutoTuneEnabled: true,
		TargetOpsPerMin: 500000, // Target: 500k ops/minute
		AdjustInterval:  1 * time.Second, // Adjust every second for faster convergence
	})
	if err != nil {
		b.Fatalf("Failed to create throttler: %v", err)
	}
	defer throttler.Close()

	b.Logf("Auto-tuning enabled with target: 500,000 ops/minute")
	b.Logf("Initial overhead factor: %.2f", throttler.GetOverheadFactor())

	ctx := context.Background()

	// Target: 500k operations per minute = ~8333 ops/sec
	targetOps := 8333
	duration := 10 * time.Second // Run for 10 seconds to allow auto-tuning to converge

	// Use multiple keys to simulate different resources being throttled
	numKeys := 10
	keys := make([]string, numKeys)
	for i := 0; i < numKeys; i++ {
		keys[i] = fmt.Sprintf("high-throughput-key-%d", i)
		// Clear any existing data
		redisKey := fmt.Sprintf("%s:%s", throttler.keyPrefix, keys[i])
		throttler.redisClient.Del(ctx, redisKey)
	}

	// Set a high limit to measure throughput without throttling
	limit := int64(targetOps)
	window := time.Minute

	var wg sync.WaitGroup
	var counter int64
	var mu sync.Mutex

	// Track overhead factor adjustments
	var factorHistory []float64
	var factorMu sync.Mutex

	// Start a goroutine to track overhead factor changes
	stopTracking := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				factor := throttler.GetOverheadFactor()
				factorMu.Lock()
				factorHistory = append(factorHistory, factor)
				factorMu.Unlock()
			case <-stopTracking:
				return
			}
		}
	}()

	// Start time
	startTime := time.Now()
	endTime := startTime.Add(duration)

	// Number of goroutines to use
	numWorkers := 150 // Increased from 100 to 150 for higher throughput

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			localCounter := 0
			key := keys[workerID%numKeys] // Use a consistent key for each worker

			// No need for a separate context for each operation

			for time.Now().Before(endTime) {
				// Use Allow instead of AllowWithWait for maximum throughput
				allowed, err := throttler.Allow(ctx, key, limit, window)

				if err != nil {
					b.Logf("Worker %d: Failed to check if allowed: %v", workerID, err)
					continue
				}

				if allowed {
					localCounter++
				} else {
					// If not allowed, wait a short time before trying again
					time.Sleep(1 * time.Millisecond)
				}
			}

			mu.Lock()
			counter += int64(localCounter)
			mu.Unlock()
		}(w)
	}

	wg.Wait()
	close(stopTracking) // Stop tracking overhead factor changes

	elapsed := time.Since(startTime)

	opsPerSecond := float64(counter) / elapsed.Seconds()
	opsPerMinute := opsPerSecond * 60

	b.Logf("Performed %d operations in %v", counter, elapsed)
	b.Logf("Throughput: %.2f ops/second (%.2f ops/minute)", opsPerSecond, opsPerMinute)
	b.Logf("Final overhead factor: %.2f", throttler.GetOverheadFactor())

	// Log overhead factor history
	factorMu.Lock()
	b.Logf("Overhead factor adjustments: %v", factorHistory)
	factorMu.Unlock()

	// Calculate how close we are to the target
	targetOpsPerMinute := 500000.0
	percentOfTarget := (opsPerMinute / targetOpsPerMinute) * 100
	b.Logf("Percentage of target: %.2f%%", percentOfTarget)

	// Check if we're within 5% of the target
	if percentOfTarget < 95 || percentOfTarget > 105 {
		b.Logf("Warning: Throughput is not within 5%% of target (500k ops/minute)")
	} else {
		b.Logf("Success: Throughput is within 5%% of target (500k ops/minute)")
	}
}

// BenchmarkControlledThroughput simulates a controlled throughput scenario
// with a target of 500k operations per minute (8333 ops/sec)
// It runs for 1 hour and ensures clients wait when the throughput limit is reached
func BenchmarkControlledThroughput(b *testing.B) {
	// Skip if not running integration tests
	if testing.Short() {
		b.Skip("Skipping controlled throughput benchmark")
	}

	// Create a new throttler with auto-tuning enabled
	// to achieve the exact target of 500k ops/minute
	throttler, err := New(Config{
		RedisAddr:       "localhost:6379",
		OverheadFactor:  20.0, // Initial overhead factor
		AutoTuneEnabled: true,
		TargetOpsPerMin: 500000, // Target: 500k ops/minute
		AdjustInterval:  30 * time.Second, // Adjust every 30 seconds for stability
	})
	if err != nil {
		b.Fatalf("Failed to create throttler: %v", err)
	}
	defer throttler.Close()

	b.Logf("Auto-tuning enabled with target: 500,000 ops/minute")
	b.Logf("Initial overhead factor: %.2f", throttler.GetOverheadFactor())

	ctx := context.Background()

	// Target: 500k operations per minute = ~8333 ops/sec
	targetOpsPerMinute := int64(500000)
	targetOpsPerSecond := targetOpsPerMinute / 60

	// Run for 1 hour
	duration := 1 * time.Hour

	// Use a single key for all operations to enforce the global limit
	key := "controlled-throughput-key"

	// Clear any existing data
	redisKey := fmt.Sprintf("%s:%s", throttler.keyPrefix, key)
	throttler.redisClient.Del(ctx, redisKey)

	// Set the limit to the target ops per minute
	limit := targetOpsPerMinute
	window := time.Minute

	var wg sync.WaitGroup
	var counter int64
	var mu sync.Mutex

	// Track throttled operations
	var throttledCounter int64

	// Track statistics at regular intervals
	statsInterval := 1 * time.Minute
	lastStatsTime := time.Now()
	var lastStatsCounter int64

	// Track overhead factor adjustments
	type FactorAdjustment struct {
		Time  time.Time
		Value float64
	}
	var factorAdjustments []FactorAdjustment
	var factorMu sync.Mutex

	// Start a goroutine to track overhead factor changes
	stopTracking := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				now := time.Now()
				factor := throttler.GetOverheadFactor()
				factorMu.Lock()
				factorAdjustments = append(factorAdjustments, FactorAdjustment{
					Time:  now,
					Value: factor,
				})
				factorMu.Unlock()
			case <-stopTracking:
				return
			}
		}
	}()

	// Start time
	startTime := time.Now()
	endTime := startTime.Add(duration)

	// Number of goroutines to use
	numWorkers := 150 // Increased to 150 for higher throughput

	b.Logf("Starting controlled throughput benchmark at %v", startTime.Format(time.RFC3339))
	b.Logf("Target: %d ops/minute (%d ops/second)", targetOpsPerMinute, targetOpsPerSecond)
	b.Logf("Duration: %v", duration)
	b.Logf("Workers: %d", numWorkers)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			localCounter := 0
			localThrottledCounter := 0

			// Create a timeout context for each operation
			// This prevents waiting indefinitely if Redis is overloaded
			opTimeout := 5 * time.Second

			for time.Now().Before(endTime) {
				// Create a timeout context for this operation
				opCtx, cancel := context.WithTimeout(ctx, opTimeout)

				// Use AllowWithWait to wait if the rate limit is reached
				allowed, err := throttler.AllowWithWait(opCtx, key, limit, window)
				cancel() // Always cancel the context to prevent resource leaks

				if err != nil {
					if err == context.DeadlineExceeded {
						// Operation was throttled and timed out
						localThrottledCounter++
						// Small backoff to prevent hammering Redis
						time.Sleep(10 * time.Millisecond)
					} else {
						b.Logf("Worker %d: Error: %v", workerID, err)
					}
					continue
				}

				if allowed {
					localCounter++

					// Check if it's time to log statistics
					now := time.Now()
					if now.Sub(lastStatsTime) >= statsInterval {
						mu.Lock()
						currentCounter := counter
						currentThrottled := throttledCounter
						elapsedSinceStart := now.Sub(startTime)
						intervalCounter := currentCounter - lastStatsCounter

						// Calculate rates
						overallOpsPerSecond := float64(currentCounter) / elapsedSinceStart.Seconds()
						overallOpsPerMinute := overallOpsPerSecond * 60
						intervalOpsPerSecond := float64(intervalCounter) / statsInterval.Seconds()
						intervalOpsPerMinute := intervalOpsPerSecond * 60

						// Get current overhead factor
						currentFactor := throttler.GetOverheadFactor()

						b.Logf("Stats at %v (elapsed: %v)", now.Format(time.RFC3339), elapsedSinceStart.Round(time.Second))
						b.Logf("Total operations: %d, Throttled: %d", currentCounter, currentThrottled)
						b.Logf("Overall throughput: %.2f ops/second (%.2f ops/minute)", overallOpsPerSecond, overallOpsPerMinute)
						b.Logf("Interval throughput: %.2f ops/second (%.2f ops/minute)", intervalOpsPerSecond, intervalOpsPerMinute)
						b.Logf("Current overhead factor: %.2f", currentFactor)
						b.Logf("Percentage of target: %.2f%%", (intervalOpsPerMinute/float64(targetOpsPerMinute))*100)

						lastStatsTime = now
						lastStatsCounter = currentCounter
						mu.Unlock()
					}
				} else {
					localThrottledCounter++
				}
			}

			mu.Lock()
			counter += int64(localCounter)
			throttledCounter += int64(localThrottledCounter)
			mu.Unlock()
		}(w)
	}

	wg.Wait()
	close(stopTracking) // Stop tracking overhead factor changes

	elapsed := time.Since(startTime)

	opsPerSecond := float64(counter) / elapsed.Seconds()
	opsPerMinute := opsPerSecond * 60

	b.Logf("Benchmark completed at %v", time.Now().Format(time.RFC3339))
	b.Logf("Performed %d operations in %v", counter, elapsed)
	b.Logf("Throttled %d operations", throttledCounter)
	b.Logf("Throughput: %.2f ops/second (%.2f ops/minute)", opsPerSecond, opsPerMinute)
	b.Logf("Final overhead factor: %.2f", throttler.GetOverheadFactor())
	b.Logf("Percentage of target: %.2f%%", (opsPerMinute/float64(targetOpsPerMinute))*100)

	// Log overhead factor adjustments
	factorMu.Lock()
	b.Logf("Overhead factor adjustments (%d):", len(factorAdjustments))
	for i, adj := range factorAdjustments {
		elapsedSinceStart := adj.Time.Sub(startTime).Round(time.Second)
		b.Logf("  [%d] Time: %v (elapsed: %v), Factor: %.2f", i, adj.Time.Format(time.RFC3339), elapsedSinceStart, adj.Value)
	}
	factorMu.Unlock()

	// Calculate how close we are to the target
	percentOfTarget := (opsPerMinute / float64(targetOpsPerMinute)) * 100

	// Check if we're within 5% of the target
	if percentOfTarget < 95 || percentOfTarget > 105 {
		b.Logf("Warning: Throughput is not within 5%% of target (500k ops/minute)")
	} else {
		b.Logf("Success: Throughput is within 5%% of target (500k ops/minute)")
	}
}
