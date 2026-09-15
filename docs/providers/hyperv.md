# Hyper-V

Provider id: `hyperv`
Kind: SSH lease
Targets: Windows (native), Linux (prepared Ubuntu cloud image)
Family: `local-vm`

## Overview

The Hyper-V provider creates Generation 2 virtual machines on a local Windows
host from prepared VHDX templates. Each lease uses its own differencing disk;
the parent template is neither copied nor modified. Select `--target windows`
or `--target linux`; omitting the target retains the native Windows default.

Both targets support SSH, Crabbox sync and claim-scoped cleanup. Native Linux
does not add checkpoint/fork, pause/resume, persistent cache, desktop/browser,
code-server or Tailscale support. A Windows outer VM can be configured by its
owner for other workloads, but `windows.mode=wsl2` is not a Hyper-V target.
Use `crabbox providers describe hyperv --json` for compiled capabilities, not
as proof that a particular image will boot.

## Requirements

- A Windows host with Hyper-V enabled and the Hyper-V PowerShell module.
- PowerShell 5.1 or later and permission to manage the new lease's VM.
- A selected existing virtual switch with DHCP; default: `Default Switch`.
- A prepared Generation 2 / UEFI VHDX for the selected guest OS.
- Adequate space for the child disk, bootstrap and file-transfer staging.

### Preparing a template

For Windows, the template needs a local administrator selected with
`--hyperv-user`, a known password supplied through
`CRABBOX_HYPERV_GUEST_PASSWORD` or trusted user config, and DHCP networking.
Domain/UPN names are not accepted. Guest internet access to GitHub is needed
when OpenSSH or Git must be installed.

OpenSSH and Git do **not** need to be pre-installed. The provider reuses
existing installations or installs pinned, SHA-256-verified Win32-OpenSSH
and MinGit packages. It does not depend on Windows Update or Features on
Demand. There is no default guest password. ISO images are unsupported.

### Password-less templates (Windows dev-environment images)

Windows development VHDXs that auto-log-on an administrator with an empty
password cannot be reached through PowerShell Direct as-is. With
`--hyperv-init-password`, the provider mounts only the lease's differencing
disk, loads its offline registry hive and writes a first-logon `RunOnce`
command to set the explicitly supplied password. The template stays untouched.

- This requires an administrator auto-logon template; otherwise prepare a
  known password in the template.
- Neither the password nor username may contain `"` or `%`, because the
  first-logon command passes through `cmd.exe`.
- The password is temporarily visible inside the owned guest in its RunOnce
  value and `net.exe` command line. Release deletes the lease disk.
- Linux rejects `--hyperv-init-password` and does not require this password.

### Linux image contract

Use a generalized Ubuntu cloud VHDX with cloud-init NoCloud support, DHCP and
working Hyper-V integration services, including `hv_kvp_daemon`. Merely
installing a desktop image or having a Linux VHDX is not qualification. ISO,
QCOW2 and RAW images are rejected. Do not bake lease credentials into the image.

KVP must work before SSH discovery: `Get-VMNetworkAdapter` must report the
guest's address. Ubuntu images commonly use matching `linux-cloud-tools-*`
packages; package/service names vary by kernel and image. Installing KVP
after waiting for an address cannot fix the initial discovery dependency.

Crabbox creates a private 64 MiB FAT `cidata` seed for each clone, containing
unique instance metadata and its SSH public key. Cloud-init creates the chosen
user, disables password SSH/root login, prepares the work root and runs the
existing minimal bootstrap. The guest needs the bootstrap tools or permitted
package access. Readiness requires `crabbox-ready`, not simply an open port.
Qualified images must support the Microsoft UEFI CA Secure Boot template;
there is no automatic Secure Boot disable fallback.

Seed creation mounts/formats **only the new seed VHDX** and requires same-user
elevation for those disk operations. Preserve the invoking user, cwd and
`XDG_STATE_HOME` across elevation. Hyper-V administration rights alone may
not authorize mounting a disk. Crabbox does not elevate itself, create a
switch, enable host features or change the parent image.

On Ubuntu, include `linux-cloud-tools-common`, the versioned
`linux-cloud-tools-<kernel-version>` package, and the matching kernel-flavour
package (for example `linux-cloud-tools-<kernel-version>-generic`). The flavour
package supplies the `/usr/lib/linux-tools/$(uname -r)/hv_kvp_daemon` path used
by the `/usr/sbin/hv_kvp_daemon` wrapper; the versioned base package alone is
not enough. The image need not contain the eventual login account:
`--hyperv-user` supplies the per-clone cloud-init username and its authorized key.
Root login stays disabled unless `root` is explicitly selected as that account;
SSH password authentication remains disabled in either case.

## Configuration

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--hyperv-image` | (none) | Prepared Generation 2 VHDX template |
| `--hyperv-user` | `crabbox` | Windows local administrator or Linux SSH user |
| `--hyperv-work-root` | `C:\crabbox` / `/work/crabbox` | Guest work root; explicit config, environment and CLI values are preserved |
| `--hyperv-cpu` | `4` | Positive virtual CPU count |
| `--hyperv-memory` | `8192` | Positive memory size in MiB |
| `--hyperv-switch` | `Default Switch` | Existing Hyper-V switch |
| `--hyperv-init-password` | `false` | Windows administrator auto-logon templates only |

Linux usernames are 1-32 lowercase letters, digits, underscores or hyphens,
starting with a lowercase letter or underscore. The Linux work root must be
an absolute POSIX directory other than `/`. Windows usernames accept letters,
digits, `.`, `_` and `-`. Explicit invalid CPU/memory flags are errors rather
than requests to silently substitute defaults.

### Config file

```yaml
provider: hyperv
target: linux
hyperv:
  image: C:\Images\ubuntu-cloud.vhdx
  user: alice
  workRoot: /home/alice/my-app
  cpus: 4
  memory: 8192
  switch: Default Switch
```

For Windows, use `target: windows` and a Windows work root. Keep the password
in the environment or trusted user config (`hyperv.guestPassword`), never in
repository config or command arguments.

### Environment variables

| Variable | Description |
| --- | --- |
| `CRABBOX_HYPERV_IMAGE` | VHDX template path |
| `CRABBOX_HYPERV_USER` | Guest SSH user |
| `CRABBOX_HYPERV_WORK_ROOT` | Guest work root |
| `CRABBOX_HYPERV_CPUS` | CPU count |
| `CRABBOX_HYPERV_MEMORY` | Memory in MiB |
| `CRABBOX_HYPERV_SWITCH` | Existing virtual switch |
| `CRABBOX_HYPERV_GUEST_PASSWORD` | Windows bootstrap administrator password |
| `CRABBOX_HYPERV_INIT_PASSWORD` | Windows first-logon password initialization |

## Bootstrap contract

Windows preserves its disconnected startup: create the child disk and VM,
optionally initialize the password, wait for PowerShell Direct, quarantine SSH
and stage the lease key, connect the network, install missing OpenSSH/Git,
activate validated key-only SSH and wait for native Windows readiness.
Boot probes and guest invocations retain their bounded retries and diagnostics.
The SSH configuration preserves an existing SFTP subsystem or supplies the
standard subsystem when missing; a successful SSH login alone is not SFTP proof.

Linux persists ownership before any creation, reserves an exclusive private
storage directory, builds the NoCloud seed and child disk, configures fixed
memory/CPU and disables automatic checkpoints. It records the new VM's
immutable identity before starting it, then waits for KVP/DHCP and authenticated
bootstrap readiness. Only after readiness is the seed detached and removed.
Reuse restores the stored target, username and work root rather than current
Windows defaults.

Acquisition has a shared 15-minute budget, with a separate two-minute cleanup
allowance. Ordinary Hyper-V queries/mutations are bounded; Windows guest
invocation budgets are capped by remaining acquisition time. Caller
cancellation takes precedence. No-IP errors identify the Linux KVP/image
contract, while host-command errors remain explicit.

## Lifecycle

1. **Acquire**: Persist a recoverable claim before provisioning; return a lease
   ID and SSH endpoint only after the selected guest's readiness check.
2. **Resolve/status**: Use the exact claim and stored target identity. Linux
   refuses missing/changed VM identities and does not guess an address.
3. **List**: Show provider inventory; a `crabbox-` prefix is not ownership proof.
4. **Release**: Verify the claim and resource identity, remove only owned
   resources, then remove credentials and the unchanged claim. Release has a
   two-minute budget; partial failures retain recovery state.
5. **Cleanup**: Only expired exact claims authorize removal. Linux uses its
   private ownership marker and recognized storage layout, not a prefix sweep.

`warmup` defaults to `--keep=true`. A failed kept Linux bootstrap retains the
claim, key and needed seed/VM for diagnosis and exact-ID `stop`. Host seed
mounts are unwound even when retaining the guest. Non-kept failures attempt
bounded owned cleanup. After a process kill, inspect the isolated claim and
retry `stop`; do not assume finally/defer executed.

Creation uncertainty, a replaced VM, an unrecognized storage marker or a
partial deletion must not be resolved by adopting/deleting a similarly named
VM. Such failures are explicit and may require operator review. Unknown
legacy target identity is not defaulted to Windows or Linux.

## Notes

- Claims and credentials honor `XDG_STATE_HOME`. VM storage does not: Windows
  retains `%USERPROFILE%\Hyper-V\Virtual Hard Disks\` and
  `%USERPROFILE%\Hyper-V\Virtual Machines\`; Linux uses an exclusive
  per-lease subdirectory under the VHD directory for its marker, child, seed
  and VM configuration.
- Parent templates, external disks, cache disks and unrelated VMs are never
  cleanup targets. Unrecognized residual files cause a recoverable error.
- Native Windows `cp` supports individual regular files using modern
  scp/SFTP. Linux `cp` from Windows still requires the supported rsync
  transport. See [`cp`](../commands/cp.md) for prerequisites and staging.
- Guest-to-host loopback access can use an external OpenSSH reverse forward.
  Crabbox's local-forward `tunnel` command is not a replacement for `ssh -R`.
- Compiled capability output and fake-host tests do not qualify an image.
  Verify boot, KVP, SSH, copy and exact cleanup on explicitly authorized clones.
- Host Hyper-V calls run in Windows PowerShell with no inherited
  `PSModulePath`, so a PowerShell 7 caller cannot make Desktop PowerShell
  resolve an in-box module such as `Microsoft.PowerShell.Security` to its Core
  copy. Machine and user module paths still apply.

## Examples

```powershell
crabbox warmup --provider hyperv --target linux --hyperv-image C:\Images\ubuntu-cloud.vhdx --hyperv-user alice --hyperv-work-root /home/alice/my-app --hyperv-cpu 2 --hyperv-memory 4096 --slug linux-test
crabbox warmup --provider hyperv --target windows --hyperv-image C:\Images\windows.vhdx --hyperv-user Administrator --slug windows-test
crabbox claims list --json
crabbox status --provider hyperv --id linux-test --json
crabbox ssh --provider hyperv --id linux-test
crabbox cp --provider hyperv --id windows-test .\input.zip SANDBOX:C:\crabbox\input.zip
crabbox cp --provider hyperv --id linux-test SANDBOX:/home/alice/my-app/evidence.zip .\evidence.zip
crabbox stop --provider hyperv --id linux-test
crabbox stop --provider hyperv --id windows-test
```
