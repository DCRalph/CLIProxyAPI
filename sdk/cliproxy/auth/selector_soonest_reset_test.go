package auth

import (
	"context"
	"strconv"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestSoonestResetSelectorPick(t *testing.T) {
	t.Parallel()

	now := time.Now()
	unix := func(d time.Duration) string { return strconv.FormatInt(now.Add(d).Unix(), 10) }
	claude := func(id, reset string) *Auth {
		return &Auth{ID: id, Provider: "claude", Quota: QuotaState{
			ObservedAt: now,
			Signals:    map[string]string{"Anthropic-Ratelimit-Unified-7d-Reset": reset},
		}}
	}

	tests := []struct {
		name  string
		auths []*Auth
		want  string
	}{
		{
			name:  "earliest weekly reset wins",
			auths: []*Auth{claude("a", unix(96*time.Hour)), claude("b", unix(20*time.Hour)), claude("c", unix(48*time.Hour))},
			want:  "b",
		},
		{
			name:  "unobserved credentials come last",
			auths: []*Auth{{ID: "a", Provider: "claude"}, claude("b", unix(96*time.Hour))},
			want:  "b",
		},
		{
			name:  "past reset counts as unobserved",
			auths: []*Auth{claude("a", unix(-time.Hour)), claude("b", unix(96*time.Hour))},
			want:  "b",
		},
		{
			name:  "no reset data falls back to fill-first",
			auths: []*Auth{{ID: "b", Provider: "claude"}, {ID: "a", Provider: "claude"}},
			want:  "a",
		},
		{
			name: "codex uses the secondary window, absolute or relative",
			auths: []*Auth{
				{ID: "a", Provider: "codex", Quota: QuotaState{ObservedAt: now, Signals: map[string]string{
					"X-Codex-Primary-Reset-At":   unix(time.Hour),
					"X-Codex-Secondary-Reset-At": unix(72 * time.Hour),
				}}},
				{ID: "b", Provider: "codex", Quota: QuotaState{ObservedAt: now, Signals: map[string]string{
					"X-Codex-Secondary-Reset-After-Seconds": strconv.Itoa(int((24 * time.Hour).Seconds())),
				}}},
			},
			want: "b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := (&SoonestResetSelector{}).Pick(context.Background(), tt.auths[0].Provider, "", cliproxyexecutor.Options{}, tt.auths)
			if err != nil {
				t.Fatalf("Pick() error = %v", err)
			}
			if got.ID != tt.want {
				t.Fatalf("Pick() auth.ID = %q, want %q", got.ID, tt.want)
			}
		})
	}
}
