package auth

import (
	"context"
	"strconv"
	"strings"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// SoonestResetSelector picks the available credential whose weekly quota window
// resets first, so quota that would otherwise expire unused is spent before
// quota that will still be there next week. Credentials with no observed reset
// (never used, or not a Claude/Codex credential) come last, because their window
// has not started yet. Ties fall back to fill-first ordering.
//
// Reset times come from the passive quota snapshot (Auth.Quota.Signals), so a
// credential is ranked by the headers of its most recent upstream response.
type SoonestResetSelector struct{}

// Pick selects the available auth with the earliest weekly reset.
func (s *SoonestResetSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	_ = opts
	now := time.Now()
	available, err := getSelectorAvailableAuths(ctx, auths, provider, model, now)
	if err != nil {
		return nil, err
	}
	available = preferCodexWebsocketAuths(ctx, provider, available)

	best := available[0]
	bestReset, bestKnown := weeklyResetAt(best, now)
	for _, candidate := range available[1:] {
		reset, known := weeklyResetAt(candidate, now)
		if known && (!bestKnown || reset.Before(bestReset)) {
			best, bestReset, bestKnown = candidate, reset, true
		}
	}
	return best, nil
}

// weeklyResetAt reads the weekly window reset from the credential's last quota
// snapshot. Resets already in the past are treated as unknown: the window has
// rolled over and the next one starts on first use.
func weeklyResetAt(auth *Auth, now time.Time) (time.Time, bool) {
	if auth == nil || len(auth.Quota.Signals) == 0 {
		return time.Time{}, false
	}
	signals := auth.Quota.Signals
	var reset time.Time
	switch strings.ToLower(strings.TrimSpace(auth.Provider)) {
	case "claude":
		reset = unixSignal(signals["Anthropic-Ratelimit-Unified-7d-Reset"])
	case "codex":
		// Codex reports the 5h window as primary and the weekly window as secondary.
		reset = unixSignal(signals["X-Codex-Secondary-Reset-At"])
		if reset.IsZero() && !auth.Quota.ObservedAt.IsZero() {
			if seconds, err := strconv.ParseInt(signals["X-Codex-Secondary-Reset-After-Seconds"], 10, 64); err == nil && seconds >= 0 {
				reset = auth.Quota.ObservedAt.Add(time.Duration(seconds) * time.Second)
			}
		}
	}
	if reset.IsZero() || !reset.After(now) {
		return time.Time{}, false
	}
	return reset, true
}

// unixSignal parses a reset header carrying Unix seconds, which upstreams may
// send with a fractional part.
func unixSignal(raw string) time.Time {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(seconds), 0)
}
