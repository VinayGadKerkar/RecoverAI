package outage

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	redisclient "recoverai/internal/redis"
)

const (
	// A bank is considered in outage if it has >= threshold failures in the window.
	outageThreshold     = 10
	outageWindowSeconds = 300 // 5 minutes
	outageTTL           = 30 * time.Minute
)

// Detector tracks bank failure rates in Redis and detects outage conditions.
type Detector struct {
	redis *redisclient.Client
}

// NewDetector creates a new outage detector.
func NewDetector(r *redisclient.Client) *Detector {
	return &Detector{redis: r}
}

// RecordFailure increments the failure counter for the given bank.
// Called by the Risk Engine every time a payment from that bank fails.
func (d *Detector) RecordFailure(ctx context.Context, bank string) error {
	if bank == "" {
		return nil
	}
	key := d.failureKey(bank)
	count, err := d.redis.Incr(ctx, key)
	if err != nil {
		return fmt.Errorf("record failure for bank %s: %w", bank, err)
	}
	if count == 1 {
		// First failure in window — set expiry
		if err := d.redis.Expire(ctx, key, outageWindowSeconds*time.Second); err != nil {
			slog.Warn("outage detector: failed to set TTL", "bank", bank, "error", err)
		}
	}

	// If threshold crossed, set an explicit outage flag with a longer TTL
	if count >= outageThreshold {
		flagKey := d.outageKey(bank)
		if err := d.redis.Set(ctx, flagKey, strconv.FormatInt(count, 10), outageTTL); err != nil {
			slog.Warn("outage detector: failed to set outage flag", "bank", bank, "error", err)
		}
		slog.Warn("bank outage detected", "bank", bank, "failures_in_window", count)
	}
	return nil
}

// IsInOutage returns true if the given bank is currently flagged as having an outage.
func (d *Detector) IsInOutage(ctx context.Context, bank string) (bool, error) {
	if bank == "" {
		return false, nil
	}
	exists, err := d.redis.Exists(ctx, d.outageKey(bank))
	if err != nil {
		return false, fmt.Errorf("check outage for bank %s: %w", bank, err)
	}
	return exists, nil
}

// FailureCount returns the current rolling failure count for a bank.
func (d *Detector) FailureCount(ctx context.Context, bank string) (int64, error) {
	v, err := d.redis.Get(ctx, d.failureKey(bank))
	if err != nil {
		return 0, nil // treat missing key as zero
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, nil
	}
	return n, nil
}

// ClearOutage manually clears an outage flag (admin action).
func (d *Detector) ClearOutage(ctx context.Context, bank string) error {
	return d.redis.Del(ctx, d.outageKey(bank), d.failureKey(bank))
}

func (d *Detector) failureKey(bank string) string {
	return fmt.Sprintf("outage:failures:%s", bank)
}

func (d *Detector) outageKey(bank string) string {
	return fmt.Sprintf("outage:flag:%s", bank)
}
