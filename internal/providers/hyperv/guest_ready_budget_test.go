package hyperv

import (
	"testing"
	"time"

	core "github.com/openclaw/crabbox/internal/cli"
)

func TestGuestReadyBudgetFromEnv(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want time.Duration
	}{
		{"unset uses a budget longer than Windows first boot", "", defaultGuestReadyBudget},
		{"explicit longer budget", "25m", 25 * time.Minute},
		{"explicit seconds-based budget", "90s", 90 * time.Second},
		{"below one minute is refused", "10s", defaultGuestReadyBudget},
		{"above the cap is refused", "3h", defaultGuestReadyBudget},
		{"unparseable is refused", "soon", defaultGuestReadyBudget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := guestReadyBudgetFromEnv(func(key string) string {
				if key != "CRABBOX_HYPERV_GUEST_READY_BUDGET" {
					t.Fatalf("unexpected variable %q", key)
				}
				return tc.raw
			})
			if got != tc.want {
				t.Fatalf("budget=%s want %s", got, tc.want)
			}
		})
	}
	if defaultGuestReadyBudget <= 7*time.Minute {
		t.Fatalf("default %s must exceed the observed 6m44s cold Windows warmup", defaultGuestReadyBudget)
	}
}

func TestAcquireBudgetCoversReadinessOverride(t *testing.T) {
	t.Setenv("CRABBOX_HYPERV_GUEST_READY_BUDGET", "40m")
	b := newBackend(core.ProviderSpec{}, core.Config{}, core.Runtime{}).(*backend)
	if b.acquireBudget <= b.guestReadyBudget {
		t.Fatalf("acquire budget %s must exceed readiness budget %s", b.acquireBudget, b.guestReadyBudget)
	}
}
