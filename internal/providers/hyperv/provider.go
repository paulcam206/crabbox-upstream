package hyperv

import (
	"flag"

	core "github.com/openclaw/crabbox/internal/cli"
	"github.com/openclaw/crabbox/internal/providers/shared"
)

func init() {
	core.RegisterProvider(Provider{})
}

type Provider struct{}

func (Provider) Name() string { return providerName }

func (Provider) Aliases() []string { return nil }

func (Provider) Spec() core.ProviderSpec {
	return core.ProviderSpec{
		Name:             providerName,
		Family:           "local-vm",
		Kind:             core.ProviderKindSSHLease,
		Targets:          []core.TargetSpec{{OS: core.TargetWindows, WindowsMode: core.WindowsModeNormal}, {OS: core.TargetLinux}},
		Features:         core.FeatureSet{core.FeatureSSH, core.FeatureCrabboxSync, core.FeatureCleanup},
		Coordinator:      core.CoordinatorNever,
		ClassDisposition: core.ProviderClassDispositionUnmapped,
	}
}

func (Provider) RegisterFlags(fs *flag.FlagSet, defaults core.Config) any {
	return registerFlags(fs, defaults)
}

func (Provider) ApplyFlags(cfg *core.Config, fs *flag.FlagSet, values any) error {
	return applyFlags(cfg, fs, values)
}

func (p Provider) Configure(cfg core.Config, rt core.Runtime) (core.Backend, error) {
	if cfg.TargetOS != "" && cfg.TargetOS != core.TargetWindows && cfg.TargetOS != core.TargetLinux {
		return nil, core.Exit(2, "provider=%s supports target=windows or target=linux", providerName)
	}
	if cfg.TargetOS == core.TargetWindows && cfg.WindowsMode != "" && cfg.WindowsMode != core.WindowsModeNormal {
		return nil, core.Exit(2, "provider=%s supports windows.mode=normal only", providerName)
	}
	if cfg.TargetOS == core.TargetLinux && cfg.WindowsMode != "" && cfg.WindowsMode != core.WindowsModeNormal {
		return nil, core.Exit(2, "windows.mode is not supported for a native Linux Hyper-V guest")
	}
	if cfg.TargetOS == core.TargetLinux && cfg.HyperV.InitPassword {
		return nil, core.Exit(2, "--hyperv-init-password is supported only for target=windows")
	}
	if cfg.TargetOS == core.TargetLinux && (cfg.Desktop || cfg.Browser || cfg.Code || len(cfg.Cache.Volumes) != 0) {
		return nil, core.Exit(2, "provider=hyperv target=linux supports SSH execution and sync, not desktop, browser, code-server or cache volumes")
	}
	if cfg.HyperV.CPUs < 0 || cfg.HyperV.Memory < 0 || int64(cfg.HyperV.Memory) > (1<<63-1)/(1024*1024) {
		return nil, core.Exit(2, "Hyper-V CPU and memory settings must be positive and representable")
	}
	if cfg.Tailscale.Enabled || string(cfg.Network) == "tailscale" {
		return nil, core.Exit(2, "--tailscale is not supported for provider=%s; use a remote SSH provider when tailnet reachability is required", providerName)
	}
	return newBackend(p.Spec(), cfg, rt), nil
}

func (p Provider) ConfigureDoctor(cfg core.Config, rt core.Runtime) (core.DoctorBackend, error) {
	return shared.ConfigureDoctor(providerName, func() (core.Backend, error) { return p.Configure(cfg, rt) })
}
