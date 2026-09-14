package hyperv

import (
	"errors"
	"flag"
	"io"
	"testing"

	core "github.com/openclaw/crabbox/internal/cli"
)

func TestHyperVResourceFlagsRejectInvalidRequests(t *testing.T) {
	for _, argument := range []string{"--hyperv-cpu=0", "--hyperv-cpu=-1", "--hyperv-memory=0", "--hyperv-memory=-1", "--hyperv-memory=8796093022208"} {
		t.Run(argument, func(t *testing.T) {
			cfg := core.BaseConfig()
			cfg.Provider, cfg.TargetOS = providerName, targetLinux
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			values := registerFlags(fs, cfg)
			if err := fs.Parse([]string{argument}); err != nil {
				t.Fatal(err)
			}
			err := applyFlags(&cfg, fs, values)
			var exitErr core.ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != 2 {
				t.Fatalf("invalid resources were not rejected: %v", err)
			}
		})
	}
}

func TestHyperVResourceFlagsPreserveRequestedBounds(t *testing.T) {
	cfg := core.BaseConfig()
	cfg.Provider, cfg.TargetOS = providerName, targetLinux
	core.MarkTargetExplicit(&cfg)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	values := registerFlags(fs, cfg)
	if err := fs.Parse([]string{"--hyperv-cpu=6", "--hyperv-memory=4096", "--hyperv-work-root=/home/alice/jobs"}); err != nil {
		t.Fatal(err)
	}
	if err := applyFlags(&cfg, fs, values); err != nil {
		t.Fatal(err)
	}
	if cfg.HyperV.CPUs != 6 || cfg.HyperV.Memory != 4096 || cfg.TargetOS != targetLinux ||
		cfg.HyperV.WorkRoot != "/home/alice/jobs" || !core.HyperVWorkRootWasExplicit(cfg) || cfg.WindowsMode != core.WindowsModeNormal {
		t.Fatal("valid resource or explicit target/root configuration was changed")
	}
	if _, err := (Provider{}).Configure(cfg, core.Runtime{}); err != nil {
		t.Fatal(err)
	}
}
