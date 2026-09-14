package hyperv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	core "github.com/openclaw/crabbox/internal/cli"
)

const cloudInitSeedSizeBytes = 64 * 1024 * 1024

func linuxStoragePath(name string) string {
	return filepath.Join(hypervVHDDir(), name)
}

func cloudInitSeedPath(name string) string {
	return filepath.Join(linuxStoragePath(name), "seed.vhdx")
}

func cloudInitMountMarker(seedPath string) string {
	return filepath.Join(filepath.Dir(seedPath), "seed-host-mounted")
}

func linuxDiskPath(name string) string {
	return filepath.Join(linuxStoragePath(name), "disk.vhdx")
}

func cloudInitMetaData(name string) string {
	return fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", name, name)
}

func writeNoCloudSource(dir, pattern, content string) (name string, err error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name = file.Name()
	defer func() {
		err = errors.Join(err, file.Close())
		if err != nil {
			err = errors.Join(err, os.Remove(name))
		}
	}()
	if err = core.SecurePrivatePath(name, false); err != nil {
		return name, err
	}
	_, err = file.WriteString(content)
	return name, err
}

func noCloudSeedScript(seedPath string) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$seed = '%s'
$mountMarker = '%s'
$mounted = $false
try {
  New-VHD -Path $seed -Dynamic -SizeBytes %d -ErrorAction Stop | Out-Null
  [IO.File]::WriteAllText($mountMarker, 'mount pending')
  $vhd = Mount-VHD -Path $seed -Passthru -ErrorAction Stop
  $mounted = $true
  $disk = $vhd | Get-Disk -ErrorAction Stop
  if ($disk.IsOffline) { Set-Disk -Number $disk.Number -IsOffline $false -ErrorAction Stop }
  if ($disk.IsReadOnly) { Set-Disk -Number $disk.Number -IsReadOnly $false -ErrorAction Stop }
  Initialize-Disk -Number $disk.Number -PartitionStyle MBR -ErrorAction Stop | Out-Null
  $partition = New-Partition -DiskNumber $disk.Number -UseMaximumSize -AssignDriveLetter -ErrorAction Stop
  $volume = $partition | Format-Volume -FileSystem FAT -NewFileSystemLabel 'cidata' -Confirm:$false -Force -ErrorAction Stop
  if (-not $volume.Path) { throw 'NoCloud seed volume has no accessible path' }
  $utf8 = New-Object System.Text.UTF8Encoding($false)
  [IO.File]::WriteAllText((Join-Path $volume.Path 'user-data'), [IO.File]::ReadAllText($env:_CRABBOX_USER_DATA_PATH), $utf8)
  [IO.File]::WriteAllText((Join-Path $volume.Path 'meta-data'), [IO.File]::ReadAllText($env:_CRABBOX_META_DATA_PATH), $utf8)
} finally {
  if ($mounted) {
    Dismount-VHD -Path $seed -ErrorAction Stop
    [IO.File]::Delete($mountMarker)
  }
}`, escapePSString(seedPath), escapePSString(cloudInitMountMarker(seedPath)), cloudInitSeedSizeBytes)
}

func (b *backend) createNoCloudSeed(ctx context.Context, seedPath, userData, metaData string) (err error) {
	userPath, err := writeNoCloudSource(filepath.Dir(seedPath), "user-data-", userData)
	if err != nil {
		return fmt.Errorf("prepare NoCloud user data: %w", err)
	}
	defer func() { err = errors.Join(err, os.Remove(userPath)) }()
	metaPath, err := writeNoCloudSource(filepath.Dir(seedPath), "meta-data-", metaData)
	if err != nil {
		return fmt.Errorf("prepare NoCloud metadata: %w", err)
	}
	defer func() { err = errors.Join(err, os.Remove(metaPath)) }()
	seedCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, runErr := b.powershellWithEnv(seedCtx, noCloudSeedScript(seedPath), append(os.Environ(),
		"_CRABBOX_USER_DATA_PATH="+userPath, "_CRABBOX_META_DATA_PATH="+metaPath))
	if runErr != nil {
		return commandError("create NoCloud seed (requires same-user elevation for the new seed disk)", result, runErr)
	}
	return core.SecurePrivatePath(seedPath, false)
}

func (b *backend) dismountNoCloudSeed(ctx context.Context, name string) error {
	if err := validateLinuxStorageName(name); err != nil {
		return err
	}
	seed := cloudInitSeedPath(name)
	// A seed attached to a running kept VM is not a host mount. Only our
	// persisted mount intent authorizes host-side dismount recovery.
	marker, err := os.Lstat(cloudInitMountMarker(seed))
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if !marker.Mode().IsRegular() {
		return errors.New("NoCloud mount marker is not a regular file")
	}
	info, err := os.Lstat(seed)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("NoCloud seed is not a regular file; refusing dismount")
	}
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'
$disk = Get-VHD -Path '%s' -ErrorAction Stop
if ($disk.Attached) { Dismount-VHD -Path '%s' -ErrorAction Stop }`, escapePSString(seed), escapePSString(seed))
	result, err := b.powershell(ctx, script)
	if err != nil {
		return commandError("dismount owned NoCloud seed", result, err)
	}
	return nil
}

func (b *backend) detachAndRemoveNoCloudSeed(ctx context.Context, name, id string) error {
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'
$vm = Get-VM -Name '%s' -ErrorAction Stop
if ([string]$vm.Id -ne '%s') { throw 'Hyper-V VM identity changed; refusing seed detach' }
Get-VMHardDiskDrive -VM $vm -ErrorAction Stop | Where-Object { $_.Path -eq '%s' } | Remove-VMHardDiskDrive -ErrorAction Stop
`, escapePSString(name), escapePSString(id), escapePSString(cloudInitSeedPath(name)))
	result, err := b.powershell(ctx, script)
	if err != nil {
		return commandError("detach owned NoCloud seed", result, err)
	}
	return b.removeVHDFile(cloudInitSeedPath(name))
}
