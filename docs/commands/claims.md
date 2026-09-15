# claims

`crabbox claims list` prints the lease claims stored on the current machine. It
is a read-only, credential-free inventory: it does not load provider
configuration, initialize a provider backend, contact a provider, refresh a
claim, or remove stale state.

```sh
crabbox claims list
crabbox claims list --json
```

Human output is headed `unverified local state` because a structurally valid
claim may be stale. Provider-scoped [`crabbox list`](list.md) remains the command
for provider-backed machine inventory.

## JSON

`--json` emits this versioned envelope, including empty arrays when no claims or
problems exist:

```json
{
  "version": 1,
  "source": "local-claims",
  "claims": [],
  "problems": []
}
```

Each claim contains only these public fields:

```json
{
  "leaseId": "cbx_abcdef123456",
  "slug": "blue-lobster",
  "provider": "aws",
  "repoRoot": "/path/to/repo",
  "pond": "",
  "targetOS": "linux",
  "windowsMode": "",
  "claimedAt": "2026-08-16T10:00:00Z",
  "lastUsedAt": "2026-08-16T10:05:00Z",
  "idleTimeoutSeconds": 1800
}
```

Cloud and resource identifiers, provider scope, hosts and endpoints, SSH
details, labels, login and registration URLs or IDs, credentials, cache
metadata, revisions, and fixed-create internals are never included. Claims are
sorted by lease ID, provider, and slug.

Malformed files do not hide valid claims. For this read-only inventory, each
local claim file has an inclusive 1 MiB implementation limit. Files that exceed
the limit while being read use `claim_too_large` and are never read into memory
in full; other concurrent changes remain `read_error`. This inventory limit does
not change the runtime claim loader. Each problem has stable `file`, `code`, and
`message` fields. Supported codes are
`invalid_filename`, `invalid_json`, `claim_too_large`, `empty_lease_id`,
`lease_id_mismatch`, `read_error`, and `invalid_claim`. Unsafe or overlong
filenames are represented by a short SHA-256 fingerprint instead of their
contents. Non-regular claim paths use `non_regular_file`. At most 100 problem
entries are emitted; the final
`problems_truncated` entry reports when additional files were omitted. Problems
are sorted by file reference and code.

Target identity comes from the stored target fields or canonical target labels,
including Hyper-V provisioning and retained-failure claims. Labels themselves
remain private. Unknown targets are not inferred from the current configuration
or VM name. Conflicting or invalid target/mode metadata adds an `invalid_target`
problem and excludes that record; valid records remain visible. This projection
does not rewrite legacy claims or contact the provider.

For non-Windows targets the public `windowsMode` is empty, including older
records carrying an inert internal `normal` default. `targetOS` remains the
plain OS name (for example `linux`), never `linux/normal`. Windows claims
retain their actual `normal` or `wsl2` mode. Raw records are not rewritten.

On Windows, scan, open-handle and post-read identity snapshots use the same
Win32 `FileIdInfo` route: a 64-bit volume serial plus the full 128-bit file ID.
This is required on ReFS, where mixing directory-enumeration and legacy handle
IDs can reject an unchanged file. Identity, mode, size and modification time
must all agree; unsupported or unknown identity remains a `read_error` rather
than falling back to path names or metadata-only matching.

## Exit codes

- `0`: the snapshot is missing, empty, or contains only valid claims.
- `2`: one or more malformed claim records were found. Valid claims and bounded
  problem entries are still written before exit.
- Other nonzero codes: the local state directory itself could not be read or the
  output could not be written.

## Flags

```text
--json    print the stable JSON envelope
```
