package hyperv

import (
	"flag"

	core "github.com/openclaw/crabbox/internal/cli"
)

type flagValues struct {
	Image        *string
	User         *string
	WorkRoot     *string
	CPUs         *int
	Memory       *int
	Switch       *string
	InitPassword *bool
}

func registerFlags(fs *flag.FlagSet, defaults core.Config) any {
	return flagValues{
		Image:        fs.String("hyperv-image", defaults.HyperV.Image, "prepared Generation 2 VHDX template (Windows or generalized Ubuntu cloud image)"),
		User:         fs.String("hyperv-user", defaults.HyperV.User, "guest SSH account (Windows administrator password via CRABBOX_HYPERV_GUEST_PASSWORD; Linux key-only)"),
		WorkRoot:     fs.String("hyperv-work-root", defaults.HyperV.WorkRoot, "Crabbox work root inside the guest"),
		CPUs:         fs.Int("hyperv-cpu", defaults.HyperV.CPUs, "CPU count for Hyper-V leases"),
		Memory:       fs.Int("hyperv-memory", defaults.HyperV.Memory, "memory in MB for Hyper-V leases"),
		Switch:       fs.String("hyperv-switch", defaults.HyperV.Switch, "Hyper-V virtual switch name"),
		InitPassword: fs.Bool("hyperv-init-password", defaults.HyperV.InitPassword, "set the guest password at first boot via the lease disk (for password-less auto-logon templates, e.g. Windows dev-environment VHDXs)"),
	}
}

func applyFlags(cfg *core.Config, fs *flag.FlagSet, values any) error {
	v, ok := values.(flagValues)
	if !ok {
		return nil
	}
	if flagWasSet(fs, "hyperv-image") {
		cfg.HyperV.Image = *v.Image
	}
	if flagWasSet(fs, "hyperv-user") {
		cfg.HyperV.User = *v.User
	}
	if flagWasSet(fs, "hyperv-work-root") {
		cfg.HyperV.WorkRoot = *v.WorkRoot
		core.MarkHyperVWorkRootExplicit(cfg)
	}
	if flagWasSet(fs, "hyperv-cpu") {
		if *v.CPUs <= 0 {
			return exit(2, "--hyperv-cpu must be positive")
		}
		cfg.HyperV.CPUs = *v.CPUs
	}
	if flagWasSet(fs, "hyperv-memory") {
		if *v.Memory <= 0 || int64(*v.Memory) > (1<<63-1)/(1024*1024) {
			return exit(2, "--hyperv-memory must be a positive, representable number of MiB")
		}
		cfg.HyperV.Memory = *v.Memory
	}
	if flagWasSet(fs, "hyperv-switch") {
		cfg.HyperV.Switch = *v.Switch
	}
	if flagWasSet(fs, "hyperv-init-password") {
		cfg.HyperV.InitPassword = *v.InitPassword
	}
	if core.ProviderNameMatches(cfg.Provider, Provider{}) {
		// Target flags are applied after provider flags on several lifecycle
		// commands. Leave target validation to the centralized provider-target
		// check after all flag sources have been applied. When no target source
		// is explicit, adopt the provider's Windows default here.
		if !core.IsTargetExplicit(cfg) && !flagWasSet(fs, "target") {
			cfg.TargetOS = targetWindows
		}
		applyDefaults(cfg)
	}
	return nil
}
