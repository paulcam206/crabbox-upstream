package hyperv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	core "github.com/openclaw/crabbox/internal/cli"
)

const linuxImageRequirements = "a generalized Ubuntu cloud VHDX with NoCloud, DHCP and working Hyper-V integration services including hv_kvp_daemon (Ubuntu needs linux-cloud-tools-common plus the versioned and kernel-flavour linux-cloud-tools packages matching uname -r)"
const linuxStorageLayout = "linux-v1"

type linuxHostOperations struct {
	seed   func(context.Context, string, string, string) error
	create func(context.Context, Config, string) (hypervVM, error)
	start  func(context.Context, string, string) error
	ip     func(context.Context, string, time.Duration) (string, error)
	detach func(context.Context, string, string) error
	remove func(context.Context, string, string) error
	query  func(context.Context, string) (hypervVM, error)
}

func (b *backend) linuxOperations() linuxHostOperations {
	if b.linuxHost != nil {
		return *b.linuxHost
	}
	return linuxHostOperations{b.createNoCloudSeed, b.createLinuxVM, b.startLinuxVM, b.waitForIP, b.detachAndRemoveNoCloudSeed, b.removeLinuxVM, b.queryLinuxVM}
}

func validLinuxSSHUser(user string) bool {
	if len(user) == 0 || len(user) > 32 {
		return false
	}
	for i, c := range user {
		if c >= 'a' && c <= 'z' || c == '_' || i > 0 && (c >= '0' && c <= '9' || c == '-') {
			continue
		}
		return false
	}
	return true
}

func linuxUserData(cfg Config, publicKey string) string {
	return strings.Replace(core.CloudInitUserData(cfg, publicKey), "#cloud-config\n",
		fmt.Sprintf("#cloud-config\nssh_pwauth: false\ndisable_root: %t\n", cfg.SSHUser != "root"), 1)
}

func validateLinuxConfig(cfg Config) error {
	if !strings.HasSuffix(strings.ToLower(cfg.HyperV.Image), ".vhdx") {
		return exit(2, "provider=hyperv target=linux requires --hyperv-image pointing to %s; ISO, QCOW2 and RAW are unsupported", linuxImageRequirements)
	}
	if !validLinuxSSHUser(cfg.HyperV.User) {
		return exit(2, "Linux --hyperv-user must be 1-32 lowercase letters, digits, underscores or hyphens, starting with a letter or underscore")
	}
	root := cfg.HyperV.WorkRoot
	if !path.IsAbs(root) || path.Clean(root) == "/" || strings.ContainsAny(root, "\\\x00\r\n") {
		return exit(2, "Linux --hyperv-work-root must be an absolute POSIX directory other than /")
	}
	if cfg.HyperV.InitPassword {
		return exit(2, "--hyperv-init-password is supported only for target=windows")
	}
	info, err := os.Lstat(cfg.HyperV.Image)
	if err != nil {
		return fmt.Errorf("inspect prepared Linux VHDX: %w", err)
	}
	if !info.Mode().IsRegular() {
		return exit(2, "prepared Linux VHDX must be a regular file, not a directory or link")
	}
	return nil
}

func (b *backend) acquireLinux(ctx context.Context, req AcquireRequest) (LeaseTarget, error) {
	if hypervHostOS != "windows" {
		return LeaseTarget{}, exit(2, "provider=hyperv requires a Windows host with Hyper-V enabled")
	}
	cfg := b.configForRun()
	if err := validateLinuxConfig(cfg); err != nil {
		return LeaseTarget{}, err
	}
	if strings.TrimSpace(req.Repo.Root) == "" {
		return LeaseTarget{}, exit(2, "Hyper-V acquisition requires a repository root for durable ownership")
	}
	if err := ctx.Err(); err != nil {
		return LeaseTarget{}, err
	}
	instances, err := b.listInstances(ctx)
	if err != nil {
		return LeaseTarget{}, err
	}
	claims, err := providerClaims()
	if err != nil {
		return LeaseTarget{}, err
	}
	var servers []Server
	for _, inst := range instances {
		servers = append(servers, b.serverFromInstance(inst, claims[inst.Name], cfg))
	}
	leaseID := newLeaseID()
	slug, err := allocateDirectLeaseSlug(leaseID, req.RequestedSlug, servers)
	if err != nil {
		return LeaseTarget{}, err
	}
	keyPath, publicKey, err := core.EnsureTestboxKeyForConfigContext(ctx, cfg, leaseID)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), b.cleanupBudget)
		defer cancel()
		return LeaseTarget{}, errors.Join(err, core.RemoveStoredTestboxConnectionArtifactsContext(cleanupCtx, leaseID))
	}
	cfg.SSHKey = keyPath
	name := leaseProviderName(leaseID, slug)
	labels := directLeaseLabels(cfg, leaseID, slug, providerName, "", req.Keep, b.now().UTC())
	labels["instance"], labels["image"] = name, cfg.HyperV.Image
	labels["ssh_user"], labels["ssh_port"], labels["work_root"] = cfg.SSHUser, sshPort, cfg.WorkRoot
	labels["state"], labels["hyperv_storage_layout"] = "provisioning", linuxStorageLayout
	claim := core.LeaseClaim{LeaseID: leaseID, Slug: slug, Provider: providerName, ProviderScope: instanceScope(name), Labels: labels}
	lease := LeaseTarget{LeaseID: leaseID, Server: b.serverFromInstance(hypervVM{Name: name, State: 3}, claim, cfg)}
	lease.Server.Status, lease.Server.Labels["state"] = "provisioning", "provisioning"
	if err := persistLease(leaseID, slug, name, cfg, req, lease); err != nil {
		removeStoredTestboxKey(leaseID)
		return LeaseTarget{}, fmt.Errorf("persist Linux ownership before creation: %w", err)
	}
	fmt.Fprintf(b.rt.Stderr, "provisioning provider=hyperv lease=%s instance=%s target=linux cpus=%d memory=%dMB keep=%v\n", leaseID, name, cfg.HyperV.CPUs, cfg.HyperV.Memory, req.Keep)
	fail := func(cause error) (LeaseTarget, error) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), b.cleanupBudget)
		defer cancel()
		current, ok, readErr := resolveLeaseClaimForProvider(leaseID, providerName)
		if readErr != nil || !ok {
			return LeaseTarget{}, errors.Join(cause, readErr, fmt.Errorf("lease %s ownership unavailable; cleanup not attempted", leaseID))
		}
		if req.Keep {
			if err := requireExactHyperVClaim(leaseID, name); err != nil {
				return LeaseTarget{}, errors.Join(cause, err)
			}
			if err := verifyLinuxStorage(name, leaseID); err != nil {
				return LeaseTarget{}, errors.Join(cause, err)
			}
			cleanupErr := b.dismountNoCloudSeed(cleanupCtx, name)
			fmt.Fprintf(b.rt.Stderr, "retained lease=%s target=linux; recover with crabbox status --provider hyperv --id %s or stop this exact lease\n", leaseID, leaseID)
			return LeaseTarget{}, errors.Join(cause, cleanupErr)
		}
		return LeaseTarget{}, errors.Join(cause, b.releaseLinuxClaim(cleanupCtx, current))
	}
	if err := reserveLinuxStorage(name, leaseID); err != nil {
		// No host operation is safe when the generated storage name was already occupied.
		return LeaseTarget{}, fmt.Errorf("reserve Linux storage for lease %s (claim retained): %w", leaseID, err)
	}
	host := b.linuxOperations()
	if err := host.seed(ctx, cloudInitSeedPath(name), linuxUserData(cfg, publicKey), cloudInitMetaData(name)); err != nil {
		return fail(err)
	}
	vm, err := host.create(ctx, cfg, name)
	if err != nil {
		return fail(err)
	}
	if vm.Name != name || !validHyperVGUID(vm.ID) {
		return fail(errors.New("created Linux VM did not return its exact immutable identity"))
	}
	lease.Server = b.serverFromInstance(vm, claim, cfg)
	lease.Server.Status, lease.Server.Labels["state"] = "provisioning", "provisioning"
	if err := persistLease(leaseID, slug, name, cfg, req, lease); err != nil {
		return fail(fmt.Errorf("persist created VM identity: %w", err))
	}
	claim.CloudImmutableID = vm.ID
	if err := host.start(ctx, name, vm.ID); err != nil {
		return fail(err)
	}
	vm.State = 2
	ip, err := host.ip(ctx, name, 5*time.Minute)
	if err != nil {
		return fail(fmt.Errorf("Linux IP discovery requires %s: %w", linuxImageRequirements, err))
	}
	lease, err = b.prepareLease(ctx, cfg, vm, ip, claim, true)
	if err != nil {
		return fail(err)
	}
	if err := host.detach(ctx, name, vm.ID); err != nil {
		return fail(err)
	}
	if err := persistLease(leaseID, slug, name, cfg, req, lease); err != nil {
		return fail(err)
	}
	return lease, nil
}

func validHyperVGUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func reserveLinuxStorage(name, leaseID string) error {
	if err := validateLinuxStorageName(name); err != nil {
		return err
	}
	if err := os.MkdirAll(hypervVHDDir(), 0o755); err != nil {
		return err
	}
	root := linuxStoragePath(name)
	if err := os.Mkdir(root, 0o700); err != nil {
		return err
	}
	if err := core.SecurePrivatePath(root, true); err != nil {
		return errors.Join(err, os.Remove(root))
	}
	ownerPath := filepath.Join(root, "owner")
	file, err := os.OpenFile(ownerPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.Join(err, os.Remove(root))
	}
	return writeLinuxOwner(file, leaseID)
}

func writeLinuxOwner(file *os.File, leaseID string) error {
	_, writeErr := file.WriteString(leaseID)
	return errors.Join(writeErr, file.Sync(), file.Close())
}

func verifyLinuxStorage(name, leaseID string) error {
	if err := validateLinuxStorageName(name); err != nil {
		return err
	}
	root := linuxStoragePath(name)
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Linux storage root is not an owned regular directory")
	}
	owner := filepath.Join(root, "owner")
	info, err = os.Lstat(owner)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 128 {
		return errors.New("Linux storage ownership marker is not a bounded regular file")
	}
	data, err := os.ReadFile(owner)
	if err != nil {
		return err
	}
	if string(data) != leaseID {
		return errors.New("Linux storage ownership conflict")
	}
	return nil
}

func validateLinuxStorageName(name string) error {
	if !validHyperVVMName(name) {
		return errors.New("invalid Linux VM storage name")
	}
	if _, err := os.UserHomeDir(); err != nil {
		return fmt.Errorf("Linux Hyper-V storage requires the current user's home: %w", err)
	}
	return nil
}

func linuxVMCreateScript(cfg Config, name string) string {
	return fmt.Sprintf(`$ErrorActionPreference='Stop'
if (Get-VM -Name '%[1]s' -ErrorAction SilentlyContinue) { throw 'Hyper-V VM name already exists' }
$switch = Get-VMSwitch -Name '%[2]s' -ErrorAction Stop
New-VHD -Path '%[3]s' -ParentPath '%[4]s' -Differencing -ErrorAction Stop | Out-Null
$vm = New-VM -Name '%[1]s' -MemoryStartupBytes %[5]d -Generation 2 -VHDPath '%[3]s' -Path '%[6]s' -ErrorAction Stop
$vm | Set-VM -ProcessorCount %[7]d -AutomaticCheckpointsEnabled $false -ErrorAction Stop
Set-VMMemory -VM $vm -DynamicMemoryEnabled $false -ErrorAction Stop
Set-VMFirmware -VM $vm -EnableSecureBoot On -SecureBootTemplate MicrosoftUEFICertificateAuthority -ErrorAction Stop
Add-VMHardDiskDrive -VM $vm -ControllerType SCSI -ControllerNumber 0 -ControllerLocation 1 -Path '%[8]s' -ErrorAction Stop
Connect-VMNetworkAdapter -VMName '%[1]s' -SwitchName $switch.Name -ErrorAction Stop
$vm | Select-Object Name, State, @{Name='Id';Expression={[string]$_.Id}} | ConvertTo-Json -Compress
`, escapePSString(name), escapePSString(cfg.HyperV.Switch), escapePSString(linuxDiskPath(name)),
		escapePSString(cfg.HyperV.Image), int64(cfg.HyperV.Memory)*1024*1024,
		escapePSString(linuxStoragePath(name)), cfg.HyperV.CPUs, escapePSString(cloudInitSeedPath(name)))
}

func (b *backend) createLinuxVM(ctx context.Context, cfg Config, name string) (hypervVM, error) {
	result, err := b.powershell(ctx, linuxVMCreateScript(cfg, name))
	if err != nil {
		return hypervVM{}, commandError("create owned Linux VM", result, err)
	}
	var vm hypervVM
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &vm); err != nil {
		return vm, fmt.Errorf("read created Linux VM identity: %w", err)
	}
	return vm, nil
}

func (b *backend) startLinuxVM(ctx context.Context, name, id string) error {
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'
$vm = Get-VM -Name '%s' -ErrorAction Stop
if ([string]$vm.Id -ne '%s') { throw 'Hyper-V VM identity changed; refusing start' }
$vm | Start-VM -ErrorAction Stop`, escapePSString(name), escapePSString(id))
	result, err := b.powershell(ctx, script)
	if err != nil {
		return commandError("start owned Linux VM", result, err)
	}
	return nil
}

func (b *backend) queryLinuxVM(ctx context.Context, name string) (hypervVM, error) {
	if err := validateLinuxStorageName(name); err != nil {
		return hypervVM{}, err
	}
	script := fmt.Sprintf(`Get-VM -ErrorAction Stop | Where-Object { $_.Name -eq '%s' } | Select-Object Name, State, @{Name='Id';Expression={[string]$_.Id}} | ConvertTo-Json -Compress`, escapePSString(name))
	result, err := b.powershell(ctx, script)
	if err != nil {
		return hypervVM{}, commandError("query Linux VM identity", result, err)
	}
	if strings.TrimSpace(result.Stdout) == "" || strings.TrimSpace(result.Stdout) == "null" {
		return hypervVM{Name: name, State: hypervMissingState}, nil
	}
	var vm hypervVM
	if err := json.Unmarshal([]byte(result.Stdout), &vm); err != nil {
		return vm, fmt.Errorf("parse Linux VM identity: %w", err)
	}
	if vm.Name != name || !validHyperVGUID(vm.ID) {
		return vm, errors.New("Linux VM query did not return the requested immutable identity")
	}
	return vm, nil
}

func (b *backend) removeLinuxVM(ctx context.Context, name, id string) error {
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'
$vm = Get-VM -ErrorAction Stop | Where-Object { $_.Name -eq '%s' }
if ($vm) {
  if ('%s' -eq '' -or [string]$vm.Id -ne '%s') { throw 'Hyper-V VM identity missing or changed; refusing removal' }
  if ([int]$vm.State -ne 3) { $vm | Stop-VM -Force -Confirm:$false -ErrorAction Stop }
  $vm | Remove-VM -Force -Confirm:$false -ErrorAction Stop
}`, escapePSString(name), escapePSString(id), escapePSString(id))
	result, err := b.powershell(ctx, script)
	if err != nil {
		return commandError("remove exact owned Linux VM", result, err)
	}
	return nil
}

func (b *backend) releaseLinuxClaim(ctx context.Context, claim core.LeaseClaim) error {
	name := instanceNameFromClaim(claim)
	if err := validateLinuxStorageName(name); err != nil {
		return err
	}
	if err := requireExactHyperVClaim(claim.LeaseID, name); err != nil {
		return err
	}
	if claim.Labels["hyperv_storage_layout"] != linuxStorageLayout {
		return exit(4, "Linux lease %s has no recognized storage ownership; refusing automatic cleanup", claim.LeaseID)
	}
	err := core.RemoveLeaseClaimIfUnchangedAfter(claim.LeaseID, claim, func() error {
		_, statErr := os.Lstat(linuxStoragePath(name))
		if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		if statErr == nil {
			if err := verifyLinuxStorage(name, claim.LeaseID); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := b.linuxOperations().remove(ctx, name, claim.CloudImmutableID); err != nil {
			return err
		}
		if err := b.dismountNoCloudSeed(ctx, name); err != nil {
			return err
		}
		for _, filename := range []string{"seed.vhdx", "disk.vhdx", "seed-host-mounted"} {
			if err := b.removeVHDFile(filepath.Join(linuxStoragePath(name), filename)); err != nil {
				return err
			}
		}
		if err := removeHyperVConfigFiles(filepath.Join(linuxStoragePath(name), name)); err != nil {
			return err
		}
		entries, err := os.ReadDir(linuxStoragePath(name))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "user-data-") || strings.HasPrefix(entry.Name(), "meta-data-") {
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					return errors.New("NoCloud source remnant is not a regular file; ownership retained")
				}
				if err := b.removeVHDFile(filepath.Join(linuxStoragePath(name), entry.Name())); err != nil {
					return err
				}
				continue
			}
			if entry.Name() != "owner" {
				return fmt.Errorf("Linux lease storage still contains %q; ownership retained", entry.Name())
			}
		}
		if err := b.removeVHDFile(filepath.Join(linuxStoragePath(name), "owner")); err != nil {
			return err
		}
		if err := os.Remove(linuxStoragePath(name)); err != nil && !os.IsNotExist(err) {
			file, restoreErr := os.OpenFile(filepath.Join(linuxStoragePath(name), "owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if restoreErr == nil {
				restoreErr = writeLinuxOwner(file, claim.LeaseID)
			}
			return errors.Join(fmt.Errorf("remove owned Linux storage directory: %w", err), restoreErr)
		}
		return core.RemoveStoredTestboxConnectionArtifactsContext(ctx, claim.LeaseID)
	})
	return err
}
