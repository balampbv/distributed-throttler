# High Performance Distributed Throttler

A Redis-based distributed throttler for Go applications that can handle high throughput rate limiting (500k+ operations per minute).

## Features

- Distributed rate limiting using Redis
- High performance design using Redis sorted sets and pipelining
- Sliding window algorithm for accurate rate limiting
- Configurable rate limits and time windows
- Thread-safe for concurrent use
- Support for waiting until rate limit is available
- Auto-tuning mechanism to achieve exact configured throughput
- Comprehensive benchmarks and tests

## Requirements

- Go 1.18 or higher
- Redis 6.0 or higher
- Docker and Docker Compose (for easy setup)

## Installation

```bash
go get github.com/yourusername/throttler
```

Or clone the repository:

```bash
git clone https://github.com/yourusername/throttler.git
cd throttler
```

## Quick Start

1. Start Redis using Docker Compose:

```bash
docker-compose up -d
```

2. Run the example application:

```bash
go run main.go
```

## Usage

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "throttler/throttler"
)

func main() {
    // Create a new throttler
    t, err := throttler.New(throttler.Config{
        RedisAddr: "localhost:6379",
    })
    if err != nil {
        log.Fatalf("Failed to create throttler: %v", err)
    }
    defer t.Close()

    ctx := context.Background()
    key := "user-123"           // Unique identifier for the rate limit
    limit := int64(100)         // Allow 100 operations
    window := time.Minute       // In a 1-minute window

    // Check if operation is allowed
    allowed, err := t.Allow(ctx, key, limit, window)
    if err != nil {
        log.Printf("Error checking throttle: %v", err)
        return
    }

    if allowed {
        fmt.Println("Operation allowed")
        // Perform the operation
    } else {
        fmt.Println("Operation throttled")
        // Handle throttling (e.g., return 429 Too Many Requests)
    }

    // Alternatively, use AllowWithWait to wait until the operation is allowed
    allowed, err = t.AllowWithWait(ctx, key, limit, window)
    if err != nil {
        log.Printf("Error waiting for throttle: %v", err)
        return
    }

    // The operation is now allowed (or the context was canceled)
    if allowed {
        fmt.Println("Operation allowed after waiting")
        // Perform the operation
    } else {
        fmt.Println("Operation canceled while waiting")
        // Handle cancellation
    }
}
```

## Configuration Options

The throttler can be configured with the following options:

```go
config := throttler.Config{
    // Basic configuration
    RedisAddr:     "localhost:6379", // Redis server address
    RedisPassword: "password",       // Redis password (if any)
    RedisDB:       0,                // Redis database number
    KeyPrefix:     "app:throttler:", // Prefix for Redis keys

    // Performance tuning
    OverheadFactor: 1.0,            // Factor to adjust the limit (default: 1.0)

    // Auto-tuning configuration
    AutoTuneEnabled: true,           // Enable auto-tuning (default: false)
    TargetOpsPerMin: 500000,         // Target operations per minute (500k)
    AdjustInterval:  10 * time.Second, // Interval to adjust overhead factor
}
```

## Benchmarking

The project includes comprehensive benchmarks to test the throttler's performance. You can run the benchmarks using the provided script:

```bash
# Make the script executable
chmod +x run_benchmarks.sh

# Run all benchmarks
./run_benchmarks.sh

# Run specific benchmarks
./run_benchmarks.sh --high-throughput  # Run only high throughput benchmark
./run_benchmarks.sh --controlled       # Run only controlled throughput benchmark (1 hour)
./run_benchmarks.sh --help             # Show all options
```

The script supports the following options:
- `--all`: Run all benchmarks (default if no options are provided)
- `--high-throughput`: Run high throughput benchmark
- `--controlled`: Run controlled throughput benchmark (1 hour)
- `--help`: Show help message

### Standard Benchmarks

When running with `--all` or no options, the script will:
1. Check if Redis is running and start it if needed
2. Run all benchmarks and save the results to `benchmark_results/all_benchmarks.txt`
3. Run the high throughput benchmark with more iterations and save the results to `benchmark_results/high_throughput.txt`

### Controlled Throughput Benchmark

The controlled throughput benchmark (`--controlled`) is designed to:
1. Run for 1 hour with a target throughput of 500,000 operations per minute
2. Use the `AllowWithWait` method to ensure clients wait when the throughput limit is reached
3. Monitor Redis CPU and memory utilization during the benchmark
4. Save detailed metrics to a CSV file for analysis

When running the controlled throughput benchmark, the script will:
1. Start Redis monitoring in the background
2. Run the benchmark for 1 hour
3. Save the benchmark results to `benchmark_results/controlled_throughput.txt`
4. Save Redis metrics to `benchmark_results/redis_metrics_YYYYMMDD_HHMMSS.csv`

A template for documenting the results is provided in `benchmark_results/controlled_throughput_template.md`. This template includes:
- Sections for recording benchmark statistics
- Instructions for analyzing Redis CPU and memory utilization
- A Python script for generating charts from the Redis metrics CSV file

### Manual Benchmarking

Alternatively, you can run the benchmarks manually:

```bash
# Run all tests and benchmarks
go test -v ./throttler

# Run only benchmarks
go test -bench=. ./throttler

# Run high throughput benchmark
go test -bench=BenchmarkHighThroughput ./throttler

# Run controlled throughput benchmark (1 hour)
go test -bench=BenchmarkControlledThroughput -benchtime=1x ./throttler
```

The benchmark results are documented in [benchmark_results/README.md](benchmark_results/README.md).

## How It Works

The throttler uses Redis sorted sets to implement a sliding window rate limiter:

1. Each request is added to a sorted set with the current timestamp as score
2. Expired entries (older than the window) are removed
3. The number of remaining entries is counted
4. If the count is within the limit, the request is allowed

This approach provides accurate rate limiting with minimal performance overhead.

### Allow Method Example

Here's a simple example of how the `Allow` method works:

```go
// Create a throttler
throttler, _ := throttler.New(throttler.Config{
    RedisAddr: "localhost:6379",
})

// Define rate limit parameters
ctx := context.Background()
key := "user-123"           // Unique identifier for the rate limit
limit := int64(5)           // Allow 5 operations
window := time.Minute       // In a 1-minute window

// First 5 requests within the window
for i := 0; i < 5; i++ {
    allowed, _ := throttler.Allow(ctx, key, limit, window)
    fmt.Printf("Request %d: %v\n", i+1, allowed) // All will be true
}

// 6th request within the window
allowed, _ := throttler.Allow(ctx, key, limit, window)
fmt.Printf("Request 6: %v\n", allowed) // Will be false (throttled)

// After the window expires, requests will be allowed again
time.Sleep(window)
allowed, _ = throttler.Allow(ctx, key, limit, window)
fmt.Printf("Request after window: %v\n", allowed) // Will be true
```

### Time and Space Complexity

The `Allow` method has the following complexity:

- **Time Complexity**: O(log(n) + m) where n is the number of operations in the current window and m is the number of expired operations being removed. This is due to the Redis sorted set operations (ZREMRANGEBYSCORE, ZADD, ZCARD). In the worst case, if most operations are expired, this could approach O(n), but in practice, it's often closer to O(log(n)) when most operations are within the current window.
- **Space Complexity**: O(n) where n is the number of operations in the current window. Each operation within the window is stored in the Redis sorted set.

The implementation uses Redis pipelining to reduce network round-trips, which significantly improves performance in high-throughput scenarios.

### Waiting for Rate Limit

The throttler supports waiting for rate limits to become available:

1. The `AllowWithWait` method first checks if the operation is allowed immediately
2. If not allowed, it calculates how long to wait until the next slot becomes available
3. It waits for that duration or until the context is canceled
4. After waiting, it tries again, repeating until the operation is allowed or the context is canceled

This is useful for applications that need to ensure operations are eventually processed rather than immediately rejected when rate limits are reached.

### Auto-Tuning Mechanism

The throttler includes a fully distributed auto-tuning mechanism that dynamically adjusts the overhead factor to achieve the exact configured throughput. This eliminates the need for manual tuning and ensures that the throttler performs optimally in any environment.

The auto-tuning mechanism is distributed, meaning that all instances of the throttler share the same operations counter in Redis. This allows the throttler to accurately track the total throughput across all instances and adjust the overhead factor accordingly.

Here's how to use the auto-tuning feature:

```go
// Create a new throttler with auto-tuning enabled
throttler, err := throttler.New(throttler.Config{
    RedisAddr:       "localhost:6379",
    AutoTuneEnabled: true,
    TargetOpsPerMin: 500000, // Target: 500k ops/minute
    AdjustInterval:  10 * time.Second, // Adjust every 10 seconds
})

// Or enable auto-tuning after creating the throttler
throttler.EnableAutoTuning(500000) // Target: 500k ops/minute
throttler.SetAdjustInterval(10 * time.Second) // Optional

// Check if auto-tuning is enabled
isEnabled := throttler.IsAutoTuningEnabled()

// Get the target operations per minute
targetOps := throttler.GetTargetOpsPerMin()

// Disable auto-tuning if needed
throttler.DisableAutoTuning()
```

The auto-tuning mechanism works by:
1. Tracking the actual throughput over time using a distributed counter in Redis
2. Comparing it to the target throughput
3. Adjusting the overhead factor to bring the actual throughput closer to the target

The adjustment algorithm uses damping and limits to ensure smooth convergence to the target throughput. The convergence time depends on the initial overhead factor, the adjustment interval, and the environment, but typically takes a few minutes.

Since the auto-tuning mechanism is distributed, it works correctly even when multiple instances of the throttler are running across different servers. All instances share the same operations counter in Redis, ensuring that the throttler has an accurate view of the total throughput.

## Performance Tuning

To achieve 500k+ operations per minute:

1. Enable auto-tuning with a target of 500,000 ops/minute (recommended)
2. Start with a reasonable initial overhead factor (e.g., 20.0) to help auto-tuning converge faster
3. Use a dedicated Redis instance with sufficient resources
4. Consider using Redis Cluster for horizontal scaling
5. Increase the number of worker goroutines in high-throughput scenarios
6. Use connection pooling (built into the Redis client)
7. Consider batching requests if appropriate for your use case

The auto-tuning feature is the recommended approach for achieving the exact configured throughput, as it automatically adjusts to the environment and conditions. Manual tuning using the overhead factor is also available but requires more effort to find the optimal value.

## Docker Compose

The included Docker Compose file sets up:

- Redis server with persistence
- Redis Commander web UI for monitoring (optional)

```bash
# Start Redis and Redis Commander
docker-compose up -d

# Access Redis Commander
open http://localhost:8081

# Stop all services
docker-compose down
```