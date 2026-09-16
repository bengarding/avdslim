# Security Policy

## Reporting a vulnerability

Please report security issues privately via
[GitHub Security Advisories](https://github.com/kdbhalala/avdslim/security/advisories/new)
rather than opening a public issue.

## Verifying what you installed

`avdslim` runs on developer machines and CI runners with access to your Android
SDK, so verify the binary before you trust it.

**Homebrew** — the formula pins a SHA-256 per artifact; `brew` verifies it for you.

**Install script** — `install.sh` downloads `checksums.txt` from the same release
and verifies the tarball's SHA-256 before extracting. It aborts on mismatch, and
refuses to install at all if no SHA-256 tool is available. Fetch it at a tag
rather than piping `main` into a shell:

```bash
curl -fsSLO https://raw.githubusercontent.com/kdbhalala/avdslim/v1.0.5/install.sh
bash install.sh
```

**Build provenance** — releases are attested via GitHub's build provenance, which
ties an artifact to this repository, commit and workflow. A checksum only proves
the download wasn't corrupted; the attestation is what says who built it:

```bash
gh attestation verify avdslim_<version>_<os>_<arch>.tar.gz --repo kdbhalala/avdslim
```

**From source** — builds use `-trimpath` and the module has zero dependencies
(`go.mod` has no `require` block), so there is no third-party code in the binary.

## What the tool does to your system

Worth knowing before running it, especially in CI:

* **`install-shim` modifies your Android SDK in place.** It renames
  `$ANDROID_HOME/emulator/emulator` to `emulator.real` and writes a shell script
  in its place. On a shared or self-hosted CI runner this outlives the job. Not
  supported on Windows.
* **`watch` modifies every emulator that boots** while it runs, including
  emulators under test. It changes the environment your tests run in.
* **`on` / `bake` / `snapshot` run `pm disable-user`** against a fixed package
  list and change guest settings. `on` records prior values so `off` can revert
  them; if the state file is lost, that record is gone.
* **`tune-avd` rewrites `config.ini`** (keeping a one-time `.bak`) and deletes
  `hardware-qemu.ini`. It does **not** delete snapshots.
* **`unbake` deletes the `avdslim_clean` snapshot** for the named AVD, and only
  that snapshot.
* It requires **no elevated host privileges**. `su 0` calls are inside the guest
  emulator only.
* It makes **no network requests** and collects **no telemetry**. The only
  network access in this repository is `install.sh` fetching a release, and
  `scripts/monitor-pull.sh`, a maintainer script that queries this repo's own
  GitHub traffic stats.

## Supported versions

Only the latest release receives fixes.
