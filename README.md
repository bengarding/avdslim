# ⚡ AVD-SLIM

> **Android Emulator RAM & CPU Optimizer**  
> *Inspired by [MobAI-App/simslim](https://github.com/MobAI-App/simslim) for iOS simulators.*

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/kdbhalala/avdslim)](https://goreportcard.com/report/github.com/kdbhalala/avdslim)
[![Release](https://img.shields.io/github/v/release/kdbhalala/avdslim)](https://github.com/kdbhalala/avdslim/releases)
[![GitHub Marketplace](https://img.shields.io/badge/Marketplace-AVD--SLIM-blue?logo=github-actions&logoColor=white)](https://github.com/marketplace/actions/avd-slim-android-emulator-ram-ci-optimizer)

`avdslim` is a lightweight, zero-dependency CLI tool that cuts Android Virtual Device (AVD) host memory consumption — on one measured Apple Silicon setup, from **~8.5 GB to ~2.5 GB** of Activity Monitor footprint (**~6.5 GB to ~1.5 GB** of dirty RAM) — and reduces idle CPU overhead, on Apple Silicon & Linux.

```
┌────────────────────────────────────────────────────────────────────────┐
│  Activity Monitor (phys_footprint), one Pixel 10 Pro AVD on M-series:  │
│                                                                        │
│  Before:  qemu-system-aarch64  ██████████████████████████  8,518 MB    │
│  After:   qemu-system-aarch64  ███████                     2,498 MB    │
│                                                                        │
│  ⚡ Reclaimed: ~6.0 GB host RAM (71% reduction)                        │
└────────────────────────────────────────────────────────────────────────┘
```

<sub>Both figures are `phys_footprint`, so they compare like for like. Dirty RAM
(`footprint -p`) drops from ~6,500 MB to ~1,560 MB. See
[Memory Footprint Breakdown](#-memory-footprint-breakdown) — these are one
machine's measurements, not a benchmark.</sub>

---

## 🔧 Changes in this fork

A security and correctness pass over upstream `v1.0.5`. High level:

**Supply chain**
* `install.sh` verifies the release SHA-256 against `checksums.txt` before extracting; previously it never verified anything.
* The GitHub Action no longer pipes `install.sh` from `main` into `bash` — it runs its own copy at whatever ref you pinned.
* All CI actions pinned to commit SHAs; releases now carry a build provenance attestation.
* `go.mod` pointed at a **nonexistent** GitHub account, which broke `go install` and left the import path claimable by anyone. Fixed.

**Data loss**
* `tune-avd` and `restart` used to delete *every* snapshot for an AVD, unprompted — including your emulator's Quick Boot state. They now warn instead.
* AVD names from argv are validated before reaching `os.RemoveAll`.
* AVD directories are resolved through the `.ini` registry. Assuming `<name>.avd` silently broke `bake`, `unbake`, `restart`, `start` and golden snapshots for any AVD whose directory name differs from its name.

**Behaviour**
* **Animations are now opt-in** (`--disable-animations`). Zeroing `animator_duration_scale` changes guest behaviour and can mask or invent races in UI tests.
* `off` records each setting's prior value and replays it, instead of writing hardcoded "stock" defaults over whatever you had.
* The emulator shim refuses to install on Windows (it bricked the SDK) and refuses to downgrade your emulator after an SDK update.
* Every external command is bounded by a timeout; a wedged `adb` used to hang `watch` forever.

**Honesty**
* `bench` reported savings against hardcoded constants that were never measured. Removed.
* `on` reported memory *increases* as "Reclaimed: 0MB", and implied it could shrink the host process — it cannot; that is fixed at launch. See the callout under [The Solution](#-the-solution).
* `off` and `make install` claimed success when they had done nothing.
* Dropped unverifiable "100% guaranteed" claims and the "<1.5s instant boot" figure (measured: ~13s end-to-end).

**Tests & CI**
* First tests in the repo (69 subtests) plus a CI workflow — previously nothing ran on a pull request.
* Every command exercised against a real SDK and emulator, which is how most of the above was found.

See `git log` for per-change detail and rationale.

---

## 🛡️ What It Touches, and What It Doesn't

The #1 fear with debloating tools is silent breakage. Here is what `avdslim`
actually does, stated as scope rather than as a guarantee — this is a
`pm disable-user` wrapper, and only testing your own app can confirm your app
still behaves.

**Not in any disable list** (see `avdslim profiles` for the full lists):

| Subsystem / Service | Notes |
| :--- | :--- |
| `com.google.android.gms` (Play Services core) | Never disabled, so Firebase Auth, FCM and the Maps SDK keep working |
| Android System WebView | Not in any list; Chromium engine and in-app browsers untouched |
| Flutter / React Native / native runtimes | Not in any list; hot reload, DevTools, JNI/NDK unaffected |
| Localhost & network sockets | Nothing touches networking; TCP/UDP, Metro (`:8081`) and `adb reverse` unaffected |

**Caveats worth knowing before you rely on it:**

* `--aggressive` disables `com.android.vending` (Play Store),
  `com.android.chrome`, and the setup wizard packages. On some images this
  **can** affect FCM registration and it definitely breaks Play Billing and
  Play Integrity testing. Don't use `--aggressive` if you test those.
* The standard list disables `com.google.android.as`
  (Android System Intelligence) on images that have it, which on some
  Android versions affects notification ranking and autofill.
* Animation scales are **left alone by default**. Pass `--disable-animations`
  to zero them — see the note in [Instant Undo / Restore](#7-instant-undo--restore-restore-off)
  about why that can matter for UI tests.
* `avdslim restore` reverts what avdslim recorded changing, using the state
  file written by `avdslim on`. If that file is gone, there is no record and
  restore reports that it changed nothing rather than guessing.

---

## 💡 Golden SDK Recommendation: Which System Image to Choose?

When creating Virtual Devices in **Android Studio Device Manager**, your choice of system image makes an enormous difference in RAM consumption:

| System Image Type | Status | Why? |
| :--- | :---: | :--- |
| **Google APIs** *(Standard 4 KB)* | ✅ **ALWAYS USE (Best)** | Firebase Auth, FCM Push & Maps work, with no Play Store background updaters. Allows `adb root` so `avdslim` can compact kernel memory. **Runs ultra-smooth at 1024 MB RAM**. |
| **Google Play** | ❌ **AVOID** | Runs heavy Play Store self-updaters and background Play Protect scanning loops. Production build locks out `adb root` (cannot flush kernel caches). Consumes ~40% more RAM. |
| **16 KB Page Size** *(`ps16k`)* | ❌ **AVOID** | Hardcodes a **4,096 MB minimum RAM ceiling in QEMU** (ignoring low-memory flags). Uses 4x larger page buffers. Only use if specifically debugging 16K native C/C++ alignment. |

---

## 🎯 The Problem

When developing Android apps on macOS or Linux, developers often discover `qemu-system-aarch64` consuming **5 GB to 8+ GB of RAM** in Activity Monitor.

### Why Does the Emulator Consume 8 GB?
1. **The Lavapipe Trap**: Android Studio frequently defaults `hw.gpu.mode = auto`, which falls back to Mesa CPU software rasterization (`lavapipe`). This allocates **~4 GB of software rendering buffers** directly in host RAM on top of the guest OS RAM.
2. **16 KB Page Size Images**: On modern ARM64 images (`google_apis_ps16k`), QEMU hardcodes a minimum RAM threshold (`minRam = 4096MB`), silently overriding lower RAM settings.
3. **Android Bloatware**: Over 35 non-essential daemons (Google Assistant, System Intelligence, Maps, Photos, YouTube, telemetry) wake CPU cores and pollute memory.
4. **Stale Snapshots**: Android Studio re-loads cached snapshots (`hardware-qemu.ini`) that preserve heavy 4 GB states across reboots.

---

## 💡 The Solution

Just like `simslim` silences iOS simulators via `launchctl`, `avdslim`:
1. **Passes `-lowram` to QEMU**: Removes the internal 4 GB lower bound and boots the Android kernel in low-RAM mode (`hw.ramSize = 1024M` or `1536M`).
2. **Enforces Cross-Platform GPU Acceleration**: Forces `-gpu host` to render natively via host GPU drivers, completely bypassing CPU software rasterizers:
   - **macOS (Apple Silicon / Intel)**: Native Apple Metal hardware acceleration.
   - **Linux / Ubuntu (Desktop)**: Native DRI / OpenGL / Vulkan via Mesa / NVIDIA drivers (`/dev/dri`).
   - **Linux / Ubuntu (Headless CI / Docker)**: Auto-detects headless environments (no `$DISPLAY`) and uses Google SwiftShader (`-gpu swiftshader_indirect`) to avoid display server crashes while bounding memory.
   - **Windows 10 / 11**: Direct3D 11 via ANGLE or native Desktop OpenGL / Vulkan.
3. **Disables 24+ Bloat Daemons**: Silences non-essential Google background services via `pm disable-user --user 0`.
4. **Optionally Eliminates Animation Lag**: Sets window, transition, and animator scales to 0x — **only when you pass `--disable-animations`**. Off by default, because zeroing `animator_duration_scale` changes guest behaviour rather than just memory use.
5. **Limits Background Churn**: Caps `background_process_limit = 4` (protecting OAuth and biometrics) and disables auto-sync.
6. **Drops Caches**: Flushes Linux page caches and compacts memory heaps.

> ### ⚠️ Which of those actually shrinks Activity Monitor
>
> **Only 1 and 2 — and only at launch.** QEMU sizes the guest's RAM and its GPU
> buffers once, when the emulator starts. Steps 3–6 are what `avdslim on` does,
> and they all happen *inside* the guest: disabling packages, changing settings,
> dropping caches. They reduce CPU wakeups and guest memory pressure, but they
> **cannot change the size of the host `qemu-system` process**.
>
> So if you start an emulator from Android Studio's Play button and then run
> `avdslim on`, Activity Monitor will *not* drop. That is expected, not a bug.
> Measured on one machine, same AVD:
>
> | How it was launched | Activity Monitor footprint |
> | :--- | :--- |
> | Android Studio Play button (no flags) | **~6.9 GB** |
> | `avdslim start` (`-memory 1024 -lowram -gpu host`) | **~2.6 GB** |
>
> To actually cut host RAM you have to relaunch:
>
> ```bash
> avdslim tune-avd <avd> --ram=1024   # persist RAM + GPU mode in config.ini
> avdslim restart                     # relaunch with those flags
> ```
>
> Or `avdslim install-shim` once, so Android Studio's Play button launches with
> the flags automatically.

---

## 📊 Memory Footprint Breakdown

| Stage | Activity Monitor (`phys_footprint`) | Active Dirty RAM (`footprint`) | Reclaimed |
| :--- | :--- | :--- | :--- |
| **Default Stock Emulator** (Pixel 10 Pro) | **8,518 MB (8.5 GB)** | ~6,500 MB | Baseline |
| **With `avdslim launch` (`-lowram`, Metal GPU)** | **2,498 MB (2.5 GB)** | **1,560 MB (1.5 GB)** | **~6.0 GB saved (71%)** |
| **Standard 1080p Profile** (Pixel 5) | **2,325 MB (2.3 GB)** | **1,306 MB (1.3 GB)** | **~6.2 GB saved (73%)** |

> **Note on Activity Monitor vs Dirty RAM**:  
> macOS Activity Monitor reports `phys_footprint` from Apple's Mach kernel ledger. This includes ~530 MB of compressed pages from the initial boot spike and Metal GPU display pipeline buffers. The actual dirty physical memory held in RAM is **~1.5 GB** (verified with `footprint -p <pid>`).

> **These are one machine's measurements, not a benchmark.** They come from a
> Pixel 10 Pro / Pixel 5 AVD on Apple Silicon. Your numbers depend on the system
> image, resolution, GPU mode and host. To measure your own, run
> `avdslim measure` before and after `avdslim on`. Note that `avdslim bench`
> reports the *current* measured state only — it deliberately does not print a
> "savings vs stock" figure, because it has no way to know what your stock
> baseline was.

---

## 🚀 Installation

> **⚠️ Only building from source gets the changes above.**
> The newest published release is `v1.0.5`, which predates all of them. Every
> install method that downloads a release artifact — Homebrew, `install.sh`,
> `go install @latest`, the pre-built tarballs — delivers the **unfixed** binary.
> Those options are struck through below until a release is cut from this branch.

### Build from source ✅

```bash
git clone https://github.com/kdbhalala/avdslim.git
cd avdslim
make install          # → ~/.local/bin/avdslim
```

Zero dependencies (`go.mod` has no `require` block), so this needs only a Go
toolchain. Override the destination with `make install PREFIX=/opt/homebrew/bin`.

---

### ~~Option 1: Homebrew~~

~~The formula pins a SHA-256 for each artifact, so Homebrew verifies the download for you.~~

> ~~`brew tap kdbhalala/avdslim …` && `brew install avdslim`~~ — the formula pins
> `v1.0.5` checksums, so this installs the pre-fix binary.

### ~~Option 2: Install script~~

~~`install.sh` downloads `checksums.txt` from the same release and verifies the tarball's SHA-256 before extracting anything, aborting on mismatch.~~

> The *script* in this branch is the hardened one, but there is no release for it
> to fetch — pointing it at `v1.0.5` downloads the pre-fix binary. It also cannot
> verify a provenance attestation, because `v1.0.5` was published before
> attestations were added.

### ~~Option 3: Go Install~~

~~`go install github.com/kdbhalala/avdslim/cmd/avdslim@latest`~~

> This *would* now work — the module path was broken upstream and is fixed here —
> but `@latest` resolves to `v1.0.5`, which predates the fix, so the command still
> fails. Working once a release is tagged from this branch.

### ~~Option 4: Pre-built Binaries~~

~~Download pre-compiled binaries from GitHub Releases.~~

> All published tarballs are `v1.0.5` and report their version as `1.0.5`
> regardless of tag (that bug is fixed here, but only in binaries built from this
> branch).

---

## 🛠️ Usage & Workflows

### 1. Android Studio 1-Click Integration (`install-shim`)
Prefer clicking the green **"Play"** button in Android Studio? Wrap the SDK emulator binary once:
```bash
avdslim install-shim
```
* **Zero workflow changes**: Android Studio launches automatically stay slimmed (1024 MB, `-lowram`, Metal GPU).
* **Reversible**: `avdslim uninstall-shim` restores the original SDK binary.

**How it works, and the caveats:** the shim renames the SDK's `emulator` binary
to `emulator.real` and puts a shell script in its place. That means:

* **If `sdkmanager` updates the emulator package**, the new binary lands at
  `emulator` and your `emulator.real` backup becomes stale. `uninstall-shim`
  detects this and refuses rather than replacing the newer binary with the older
  backup; delete the stale backup and re-run `install-shim`.
* **If a golden snapshot exists**, shimmed launches boot it with
  `-no-snapshot-save`, which means **state from that session is discarded on
  exit**. The shim prints a notice when it does this. Pass `--no-slim` or run
  `avdslim unbake <avd>` to boot normally.
* **Not supported on Windows.** The entry point there is `emulator.exe`, and a
  script cannot stand in for a `.exe` that callers invoke by exact path.
  `install-shim` refuses on Windows; use `avdslim start` instead.
* It modifies a **shared** SDK. On a self-hosted CI runner the change outlives
  the job and affects every later build on that machine.

---

### 2. Live Efficiency Benchmark (`bench`)
Print a live before/after scoreboard comparing stock flagship consumption against your running slimmed AVD:
```bash
avdslim bench
```

---

### 3. Golden Snapshot: Skip the Cold Boot (`bake`)
Cold booting Android emulators typically takes 35–60 seconds. `avdslim bake` cold boots your emulator once, applies all bloat pruning and memory optimizations, and saves an immutable `avdslim_clean` snapshot:
```bash
avdslim bake
# Or specify AVD name or index:
avdslim bake Pixel_10_Pro
# Headless baking (for CI or background):
avdslim bake 1 --headless
```
* **Skips the cold boot**: subsequent launches (`avdslim start`, or Android Studio via the shim) resume the saved RAM image instead of booting Android from scratch. QEMU's snapshot load itself is a second or two; `avdslim start` end-to-end is longer, because it also waits for `sys.boot_completed` and re-applies the slim pass. Measured on one machine: ~46s to bake cold, ~13s for a subsequent `avdslim start`.
* **Ephemeral safety (`-no-snapshot-save`)**: Dev sessions never pollute the snapshot. Every reboot starts 100% clean and slimmed.

---

### 4. Snapshot Live Configured State (`snapshot`)
Want your test apps, debug build, local database, or test account logins preserved in the instant restore snapshot?
1. Launch your emulator: `avdslim start`
2. Install your apps, log into test accounts, and configure your test environment.
3. Lock this exact state into your Golden Snapshot:
```bash
avdslim snapshot
# Or alias:
avdslim bake --live
```
Now every future launch resumes your pre-installed apps and credentials from the saved image instead of cold-booting.

---

### 5. Remove / Reset Golden Snapshot (`unbake`)
If an Android SDK image updates or you want to return to stock cold boots:
```bash
avdslim unbake
# Or specify AVD name or index:
avdslim unbake Pixel_10_Pro
```

---

### 6. Zero-Friction Watch Mode (`watch`)
Don't want to change your workflow? Run `avdslim watch` in the background. Whenever you launch an emulator from Android Studio or VS Code, `avdslim` detects it and automatically silences bloat as soon as it boots:
```bash
avdslim watch
```
*(Options: pass `--aggressive`, `--keep=<package>` or `--disable-animations`)*.

> ⚠️ `watch` modifies **every** emulator that boots while it runs, including one
> you started deliberately stock to reproduce a bug, and including emulators
> under test in CI. That is the point of the mode, but it means the environment
> your tests run in is no longer the environment you configured.

---

### 7. Instant Undo / Restore (`restore`, `off`)
Reverts what avdslim recorded changing: re-enables the packages it disabled and
puts each setting back to the value it read **before** changing it.
```bash
avdslim restore
# Or use alias:
avdslim off
```

* `avdslim on` writes a state file to `/data/local/tmp/avdslim_state.json`
  recording the packages it disabled and the prior value of every setting it
  touched. `restore` replays exactly that. **If the state file is gone**
  (for example after a cold boot that wiped `/data/local/tmp`), there is no
  record of what changed, and `restore` reports that it changed nothing rather
  than enabling packages it may never have disabled.
* `user_setup_complete` and `device_provisioned` are recorded but deliberately
  **not** reverted — setting them back to `0` re-triggers the setup wizard.
* **On animations:** avdslim leaves animation scales alone unless you pass
  `--disable-animations`. If you do opt in, note that
  `animator_duration_scale = 0` changes behaviour, not just performance — some
  View/Compose animations invoke their end callbacks synchronously at duration
  zero, which can hide or manufacture races in UI tests. `restore` puts back
  whatever value you had, including a non-default one like `0.5`.

---

### 8. Slim an Active Emulator (`on`)
Immediately silences background bloat and trims memory on a running emulator:
```bash
# Standard preset (safe for all apps):
avdslim on

# Aggressive preset (also disables Play Store self-updater):
avdslim on --aggressive

# Keep a specific app (e.g. Google Maps):
avdslim on --keep=com.google.android.apps.maps
```

---

### 9. Deep Memory Breakdown (`measure`)
Inspect host macOS memory (`phys_footprint`, resident RSS) alongside the guest Android `dumpsys meminfo`:
```bash
avdslim measure
# Or specify serial:
avdslim measure emulator-5554
```

---

### 10. Tune Host AVD Configuration (`tune-avd`)
Configures an AVD's `config.ini` for optimal memory consumption and purges stale snapshots:
```bash
avdslim tune-avd Pixel_10_Pro --ram=1024 --heap=256
```
* Sets `hw.ramSize = 1024`
* Sets `hw.gpu.mode = host` (Apple Silicon Metal hardware acceleration)
* Disables camera and audio emulation threads
* Purges stale `hardware-qemu.ini` and snapshots

---

### 11. Restart Emulator with Clean Cache (`restart`)
Gracefully shuts down the emulator, purges stale runtime snapshots, and relaunches with low-memory host flags:
```bash
avdslim restart emulator-5554 --ram=1024
```

---

### 12. Start / Launch Emulator (`start`, `run`, `launch`)
Starts an AVD with low-memory host flags and auto-slims upon boot. If a Golden Snapshot exists, it resumes from that image instead of cold-booting:
```bash
# Interactive numbered menu (press 1, 2, or hit Enter for default)
avdslim start

# Select directly by index number
avdslim start 1

# Multi-window / split-screen testing (disables -lowram kernel flag)
avdslim start 1 --no-lowram

# Headless mode (for CI runners or automated testing)
avdslim start 1 --headless

# Force a cold boot without loading snapshot
avdslim start 1 --cold

# Custom RAM allocation
avdslim run Pixel_10_Pro --ram=1024

# Skip auto-slimming if you need stock services untouched
avdslim start 1 --no-slim
```

---

### 13. Graceful Stop with Snapshot Prompt (`stop`, `kill`)
Gracefully shuts down the emulator. Optionally updates the Golden Snapshot with your session's state:
```bash
# Interactive (prompts if you want to save current state):
avdslim stop

# Automatically trim bloat, snapshot state, and exit:
avdslim stop --snap

# Immediate force exit without snapshotting:
avdslim stop -f
```

---

### 14. Environment Doctor (`doctor`)
Audits your Android toolchain, active AVDs, Golden Snapshots, 16K page size overhead, and warns about software GPU fallback:
```bash
avdslim doctor
```

---

### 15. View Bloat Profiles (`profiles`)
Inspects the list of disabled packages categorized by function (Assistant, Telephony, Consumer Bloat, etc.), what is not in any disable list, and the caveats that apply:
```bash
avdslim profiles
```

---

## ☁️ GitHub Actions CI Integration
Official GitHub Marketplace Action: **[AVD-SLIM — Android Emulator RAM & CI Optimizer](https://github.com/marketplace/actions/avd-slim-android-emulator-ram-ci-optimizer)**

Slash CI runner memory and run parallel emulator shards on free GitHub Actions runners:

```yaml
- name: AVD-SLIM — Android Emulator RAM & CI Optimizer
  uses: kdbhalala/avdslim@<commit-sha>   # pin a SHA, not a tag
  with:
    ram: '1024'
    install-shim: 'false'   # 'true' mutates the runner's SDK in place
```

> **⚠️ Do not use `@v1`.** That tag predates this branch, so it still pipes
> `install.sh` from `main` into `bash` — meaning any push to `main` becomes
> arbitrary code execution in your CI job, with your `GITHUB_TOKEN` and secrets in
> scope. Pin a commit SHA containing these changes.
>
> `install-shim: 'true'` modifies the SDK in place. Fine on an ephemeral runner;
> on a **self-hosted** runner the change outlives the job and affects every later
> build on that machine. `watch: true` modifies every emulator that boots,
> including ones under test.

---

## 📜 License

MIT License. See [LICENSE](LICENSE) for details.
