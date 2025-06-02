package throttler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Throttler represents a distributed rate limiter using Redis
type Throttler struct {
	redisClient *redis.Client
	keyPrefix   string
	// overheadFactor is used to adjust the limit to account for overhead
	// A value of 1.0 means no adjustment, 2.0 means double the limit, etc.
	overheadFactor float64

	// Auto-tuning fields
	autoTuneEnabled bool
	targetOpsPerMin int64

	// Throughput tracking
	lastAdjustTime time.Time
	adjustInterval time.Duration
	mu             sync.Mutex
}

// ClearKey removes all rate limit data for a specific key
func (t *Throttler) ClearKey(ctx context.Context, key string) error {
	redisKey := fmt.Sprintf("%s:%s", t.keyPrefix, key)
	return t.redisClient.Del(ctx, redisKey).Err()
}

// getOpsCounterKey returns the Redis key for the operations counter
func (t *Throttler) getOpsCounterKey() string {
	return fmt.Sprintf("%s:ops_counter", t.keyPrefix)
}

// getOpsCounter gets the operations counter from Redis
func (t *Throttler) getOpsCounter(ctx context.Context) (int64, error) {
	counterKey := t.getOpsCounterKey()
	val, err := t.redisClient.Get(ctx, counterKey).Int64()
	if err == redis.Nil {
		// Key doesn't exist, return 0
		return 0, nil
	}
	return val, err
}

// incrementOpsCounter increments the operations counter in Redis
func (t *Throttler) incrementOpsCounter(ctx context.Context) error {
	counterKey := t.getOpsCounterKey()
	return t.redisClient.Incr(ctx, counterKey).Err()
}

// resetOpsCounter resets the operations counter in Redis
func (t *Throttler) resetOpsCounter(ctx context.Context) error {
	counterKey := t.getOpsCounterKey()
	return t.redisClient.Set(ctx, counterKey, 0, 0).Err()
}

// SetOverheadFactor sets the overhead factor used to adjust the limit
// A value of 1.0 means no adjustment, 2.0 means double the limit, etc.
// This can be used to tune the throttler to achieve the exact configured throughput
func (t *Throttler) SetOverheadFactor(factor float64) {
	if factor <= 0 {
		factor = 1.0 // Default: no adjustment
	}
	t.overheadFactor = factor
}

// GetOverheadFactor returns the current overhead factor
func (t *Throttler) GetOverheadFactor() float64 {
	return t.overheadFactor
}

// EnableAutoTuning enables automatic tuning of the overhead factor
// to achieve the exact target throughput
func (t *Throttler) EnableAutoTuning(targetOpsPerMin int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.autoTuneEnabled = true
	t.targetOpsPerMin = targetOpsPerMin
	// Reset the operations counter in Redis
	ctx := context.Background()
	if err := t.resetOpsCounter(ctx); err != nil {
		// Just log the error and continue
		fmt.Printf("Failed to reset operations counter: %v\n", err)
	}
	t.lastAdjustTime = time.Now()
}

// DisableAutoTuning disables automatic tuning of the overhead factor
func (t *Throttler) DisableAutoTuning() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.autoTuneEnabled = false
}

// IsAutoTuningEnabled returns whether automatic tuning is enabled
func (t *Throttler) IsAutoTuningEnabled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.autoTuneEnabled
}

// GetTargetOpsPerMin returns the target operations per minute
func (t *Throttler) GetTargetOpsPerMin() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.targetOpsPerMin
}

// SetAdjustInterval sets the interval at which to adjust the overhead factor
func (t *Throttler) SetAdjustInterval(interval time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if interval > 0 {
		t.adjustInterval = interval
	}
}

// adjustOverheadFactor adjusts the overhead factor based on the observed throughput
// to achieve the exact target throughput
func (t *Throttler) adjustOverheadFactor() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if auto-tuning is enabled
	if !t.autoTuneEnabled || t.targetOpsPerMin <= 0 {
		return
	}

	// Check if it's time to adjust
	now := time.Now()
	elapsed := now.Sub(t.lastAdjustTime)
	if elapsed < t.adjustInterval {
		return
	}

	// Calculate current throughput
	elapsedMinutes := elapsed.Minutes()
	if elapsedMinutes <= 0 {
		return
	}

	// Get the operations counter from Redis
	ctx := context.Background()
	opsCount, err := t.getOpsCounter(ctx)
	if err != nil {
		// Just log the error and return
		fmt.Printf("Failed to get operations counter: %v\n", err)
		return
	}

	currentOpsPerMin := float64(opsCount) / elapsedMinutes

	// Reset counter and update last adjust time
	if err := t.resetOpsCounter(ctx); err != nil {
		// Just log the error and continue
		fmt.Printf("Failed to reset operations counter: %v\n", err)
	}
	t.lastAdjustTime = now

	// If current throughput is close enough to target (within 1%), don't adjust
	targetOpsPerMin := float64(t.targetOpsPerMin)
	if math.Abs(currentOpsPerMin-targetOpsPerMin)/targetOpsPerMin < 0.01 {
		return
	}

	// Calculate new overhead factor
	// If current throughput is too low, increase the factor
	// If current throughput is too high, decrease the factor
	if currentOpsPerMin > 0 {
		// Calculate adjustment ratio
		ratio := targetOpsPerMin / currentOpsPerMin

		// Apply adjustment with damping to prevent oscillation
		// Use a damping factor of 0.5 (adjust halfway)
		dampingFactor := 0.5
		adjustment := (ratio - 1.0) * dampingFactor

		// Limit adjustment to prevent extreme changes
		if adjustment > 1.0 {
			adjustment = 1.0
		} else if adjustment < -0.5 {
			adjustment = -0.5
		}

		// Apply adjustment
		newFactor := t.overheadFactor * (1.0 + adjustment)

		// Ensure factor is reasonable
		if newFactor < 1.0 {
			newFactor = 1.0
		} else if newFactor > 100.0 {
			newFactor = 100.0
		}

		// Update factor
		t.overheadFactor = newFactor
	}
}

// Config holds the configuration for the throttler
type Config struct {
	// RedisAddr is the address of the Redis server
	RedisAddr string
	// RedisPassword is the password for the Redis server
	RedisPassword string
	// RedisDB is the Redis database to use
	RedisDB int
	// KeyPrefix is the prefix for Redis keys
	KeyPrefix string
	// OverheadFactor is used to adjust the limit to account for overhead
	// A value of 1.0 means no adjustment, 2.0 means double the limit, etc.
	// Default is 1.0 (no adjustment)
	OverheadFactor float64

	// AutoTuneEnabled enables automatic tuning of the overhead factor
	// to achieve the exact target throughput
	AutoTuneEnabled bool
	// TargetOpsPerMin is the target operations per minute
	// Only used when AutoTuneEnabled is true
	TargetOpsPerMin int64
	// AdjustInterval is the interval at which to adjust the overhead factor
	// Default is 10 seconds
	AdjustInterval time.Duration
}

// New creates a new Throttler with the given configuration
func New(config Config) (*Throttler, error) {
	if config.RedisAddr == "" {
		return nil, errors.New("redis address is required")
	}

	client := redis.NewClient(&redis.Options{
		Addr:     config.RedisAddr,
		Password: config.RedisPassword,
		DB:       config.RedisDB,
	})

	keyPrefix := config.KeyPrefix
	if keyPrefix == "" {
		keyPrefix = "throttler:"
	}

	// Set default overhead factor if not provided
	overheadFactor := config.OverheadFactor
	if overheadFactor <= 0 {
		overheadFactor = 1.0 // Default: no adjustment
	}

	// Set default adjust interval if not provided
	adjustInterval := config.AdjustInterval
	if adjustInterval <= 0 {
		adjustInterval = 10 * time.Second // Default: 10 seconds
	}

	return &Throttler{
		redisClient:     client,
		keyPrefix:       keyPrefix,
		overheadFactor:  overheadFactor,
		autoTuneEnabled: config.AutoTuneEnabled,
		targetOpsPerMin: config.TargetOpsPerMin,
		lastAdjustTime:  time.Now(),
		adjustInterval:  adjustInterval,
	}, nil
}

// Allow checks if an operation is allowed based on the rate limit
// key: unique identifier for the rate limit (e.g., user ID, IP address)
// limit: maximum number of operations allowed in the time window
// window: time window in seconds
// returns: true if the operation is allowed, false otherwise
func (t *Throttler) Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, error) {
	// Get the current overhead factor (thread-safe)
	var overheadFactor float64
	func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		overheadFactor = t.overheadFactor
	}()

	redisKey := fmt.Sprintf("%s:%s", t.keyPrefix, key)

	// Apply overhead factor to adjust the limit
	// This allows the throttler to achieve the exact configured throughput
	// by accounting for overhead
	adjustedLimit := int64(float64(limit) * overheadFactor)

	// Current timestamp in milliseconds
	now := time.Now().UnixMilli()
	windowMs := window.Milliseconds()

	// Remove expired entries (older than the window)
	cutoff := now - windowMs

	// Remove all elements with score less than cutoff
	err := t.redisClient.ZRemRangeByScore(ctx, redisKey, "0", fmt.Sprintf("%d", cutoff)).Err()
	if err != nil {
		return false, fmt.Errorf("failed to remove expired entries: %w", err)
	}

	// Count the number of requests in the current window
	currentCount, err := t.redisClient.ZCard(ctx, redisKey).Result()
	if err != nil {
		return false, fmt.Errorf("failed to get current count: %w", err)
	}

	// If we're already at or over the adjusted limit, don't allow the operation
	if currentCount >= adjustedLimit {
		return false, nil
	}

	// We're under the adjusted limit, so add the current request
	err = t.redisClient.ZAdd(ctx, redisKey, redis.Z{Score: float64(now), Member: fmt.Sprintf("%d", now)}).Err()
	if err != nil {
		return false, fmt.Errorf("failed to add request: %w", err)
	}

	// Set expiration on the key to automatically clean up
	err = t.redisClient.Expire(ctx, redisKey, window+time.Minute).Err()
	if err != nil {
		return false, fmt.Errorf("failed to set expiration: %w", err)
	}

	// Update throughput statistics and adjust overhead factor if auto-tuning is enabled
	if t.autoTuneEnabled {
		func() {
			t.mu.Lock()
			defer t.mu.Unlock()

			// Increment operations counter in Redis
			if err := t.incrementOpsCounter(ctx); err != nil {
				// Just log the error and continue
				fmt.Printf("Failed to increment operations counter: %v\n", err)
			}

			// Check if it's time to adjust the overhead factor
			now := time.Now()
			if now.Sub(t.lastAdjustTime) >= t.adjustInterval {
				// Adjust overhead factor based on observed throughput
				t.adjustOverheadFactor()
			}
		}()
	}

	return true, nil
}

// GetRemainingTime returns the estimated time until the next request can be allowed
// This is an approximation based on the current rate and limit
func (t *Throttler) GetRemainingTime(ctx context.Context, key string, limit int64, window time.Duration) (time.Duration, error) {
	// Get the current overhead factor (thread-safe)
	var overheadFactor float64
	func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		overheadFactor = t.overheadFactor
	}()

	redisKey := fmt.Sprintf("%s:%s", t.keyPrefix, key)

	// Apply overhead factor to adjust the limit
	// This allows the throttler to achieve the exact configured throughput
	// by accounting for overhead
	adjustedLimit := int64(float64(limit) * overheadFactor)

	// Current timestamp in milliseconds
	now := time.Now().UnixMilli()
	windowMs := window.Milliseconds()

	// Remove expired entries (older than the window)
	cutoff := now - windowMs

	// Use Redis pipeline to execute commands
	pipe := t.redisClient.Pipeline()

	// Remove all elements with score less than cutoff
	pipe.ZRemRangeByScore(ctx, redisKey, "0", fmt.Sprintf("%d", cutoff))

	// Count the number of requests in the current window
	pipe.ZCard(ctx, redisKey)

	// Get the oldest timestamp in the window
	pipe.ZRange(ctx, redisKey, 0, 0)

	// Execute the pipeline
	cmds, err := pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to execute Redis pipeline: %w", err)
	}

	// Get the count from the ZCard command (second command in the pipeline)
	count, err := cmds[1].(*redis.IntCmd).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get count: %w", err)
	}

	// If we're under the adjusted limit, no need to wait
	// Note: We check if count < adjustedLimit (not <=) because we need to account for the new request
	// that will be added when Allow is called
	if count < adjustedLimit {
		return 0, nil
	}

	// Get the oldest entry from the ZRange command (third command in the pipeline)
	oldestEntries, err := cmds[2].(*redis.StringSliceCmd).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get oldest entry: %w", err)
	}

	// If there are no entries, no need to wait
	if len(oldestEntries) == 0 {
		return 0, nil
	}

	// Parse the oldest timestamp
	var oldestTimestamp int64
	_, err = fmt.Sscanf(oldestEntries[0], "%d", &oldestTimestamp)
	if err != nil {
		return 0, fmt.Errorf("failed to parse oldest timestamp: %w", err)
	}

	// Calculate when the oldest entry will expire
	expiryTime := oldestTimestamp + windowMs

	// Calculate how long to wait
	waitTime := expiryTime - now

	// If waitTime is negative, no need to wait
	if waitTime <= 0 {
		return 0, nil
	}

	return time.Duration(waitTime) * time.Millisecond, nil
}

// AllowWithWait checks if an operation is allowed based on the rate limit
// If not allowed, it waits until the operation can be allowed or the context is canceled
// Returns true if the operation was eventually allowed, false if the context was canceled
func (t *Throttler) AllowWithWait(ctx context.Context, key string, limit int64, window time.Duration) (bool, error) {
	// First try without waiting
	allowed, err := t.Allow(ctx, key, limit, window)
	if err != nil {
		return false, err
	}

	if allowed {
		return true, nil
	}

	// If not allowed, start waiting with exponential backoff
	backoff := 10 * time.Millisecond
	maxBackoff := 1 * time.Second

	for {
		// Check if context is canceled
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
			// Continue
		}

		// Get estimated time until next slot is available
		waitTime, err := t.GetRemainingTime(ctx, key, limit, window)
		if err != nil {
			// If there's an error, use exponential backoff
			waitTime = backoff
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
		} else if waitTime == 0 {
			// If waitTime is 0, use minimum backoff
			waitTime = backoff
		}

		// Wait for the calculated time or until context is canceled
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(waitTime):
			// Try again after waiting
			allowed, err := t.Allow(ctx, key, limit, window)
			if err != nil {
				return false, err
			}

			if allowed {
				return true, nil
			}

			// If still not allowed, increase backoff and try again
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
		}
	}
}

// Close closes the Redis client
func (t *Throttler) Close() error {
	return t.redisClient.Close()
}
