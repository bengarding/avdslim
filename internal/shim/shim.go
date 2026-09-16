package shim

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kdbhalala/avdslim/internal/config"
)

const shimHeader = "# avdslim emulator shim"

// headerProbeBytes is how much of the emulator binary we read when checking for
// the shim marker. The marker sits in the first line, and the real emulator
// binary is hundreds of megabytes — reading all of it into memory to run a
// substring search was pure waste.
const headerProbeBytes = 512

func GetEmulatorBinaryPaths() (emuPath, realPath string, err error) {
	sdkDir := config.GetAndroidSdkDir()
	if sdkDir == "" {
		return "", "", fmt.Errorf("android SDK directory not found ($ANDROID_HOME or default SDK path)")
	}

	emuDir := filepath.Join(sdkDir, "emulator")
	binaryName := "emulator"
	realName := "emulator.real"
	if runtime.GOOS == "windows" {
		binaryName = "emulator.exe"
		realName = "emulator.real.exe"
	}

	emuPath = filepath.Join(emuDir, binaryName)
	realPath = filepath.Join(emuDir, realName)

	if _, err := os.Stat(emuDir); os.IsNotExist(err) {
		return "", "", fmt.Errorf("emulator directory %s does not exist", emuDir)
	}
	return emuPath, realPath, nil
}

// hasShimHeader reports whether path begins with the avdslim shim marker.
// Errors are returned rather than swallowed: previously a permission error made
// this look like "not a shim", which let InstallShim rename the real binary away
// on top of an existing shim.
func hasShimHeader(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	// io.ReadFull rather than a single Read: Read is permitted to return fewer
	// bytes than requested without hitting EOF, which could hide the marker.
	buf := make([]byte, headerProbeBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return false, err
	}
	return strings.Contains(string(buf[:n]), shimHeader), nil
}

func IsShimInstalled() (bool, error) {
	emuPath, realPath, err := GetEmulatorBinaryPaths()
	if err != nil {
		return false, err
	}

	if _, err := os.Stat(realPath); err != nil {
		return false, nil
	}
	isShim, err := hasShimHeader(emuPath)
	if err != nil {
		return false, err
	}
	return isShim, nil
}

func InstallShim(defaultRam int) error {
	// The shim works by replacing the emulator binary with a shell script. On
	// Windows the binary is emulator.exe, and a batch script written to a .exe
	// filename is not executable — the previous implementation renamed the real
	// binary away and then wrote batch content into emulator.exe, leaving the SDK
	// emulator unlaunchable. Refuse rather than break the install.
	if runtime.GOOS == "windows" {
		return fmt.Errorf("install-shim is not supported on Windows: the emulator entry point is emulator.exe " +
			"and a script cannot stand in for it. Use `avdslim start` to launch with low-memory flags instead")
	}

	emuPath, realPath, err := GetEmulatorBinaryPaths()
	if err != nil {
		return err
	}

	if defaultRam <= 0 {
		defaultRam = 1024
	}

	emuIsShim := false
	if _, statErr := os.Stat(emuPath); statErr == nil {
		emuIsShim, err = hasShimHeader(emuPath)
		if err != nil {
			return fmt.Errorf("could not inspect %s: %w", emuPath, err)
		}
	} else if os.IsNotExist(statErr) {
		// emulator is gone. If the backup is present the SDK is mid-breakage.
		if _, realErr := os.Stat(realPath); realErr == nil {
			return fmt.Errorf("%s is missing but %s exists — the shim is half-installed. "+
				"Run `avdslim uninstall-shim` to restore the original binary", emuPath, realPath)
		}
		return fmt.Errorf("emulator binary not found at %s", emuPath)
	} else {
		return statErr
	}

	// Already shimmed: just refresh the script (e.g. a new --ram value).
	if emuIsShim {
		if _, realErr := os.Stat(realPath); realErr != nil {
			return fmt.Errorf("%s is an avdslim shim but the original binary %s is missing. "+
				"Reinstall the emulator package via sdkmanager, then re-run install-shim", emuPath, realPath)
		}
		return writeShimScript(emuPath, realPath, defaultRam)
	}

	// emuPath is a real binary. If realPath also exists, the SDK emulator was
	// reinstalled underneath us and realPath is a stale copy — do not overwrite
	// it with the newer binary, and do not silently discard the new one.
	if _, realErr := os.Stat(realPath); realErr == nil {
		return fmt.Errorf("both %s and %s exist as real binaries, which happens when the emulator "+
			"package is reinstalled while the shim is active. Delete the stale %s, then re-run install-shim",
			emuPath, realPath, realPath)
	}

	if err := os.Rename(emuPath, realPath); err != nil {
		return fmt.Errorf("failed to backup original emulator: %w", err)
	}

	if err := writeShimScript(emuPath, realPath, defaultRam); err != nil {
		// Roll back so the SDK is never left without a working emulator.
		if rbErr := os.Rename(realPath, emuPath); rbErr != nil {
			return fmt.Errorf("failed to install shim (%v) AND failed to roll back (%v). "+
				"Restore manually: mv %s %s", err, rbErr, realPath, emuPath)
		}
		return fmt.Errorf("failed to install shim: %w", err)
	}

	return nil
}

func UninstallShim() error {
	emuPath, realPath, err := GetEmulatorBinaryPaths()
	if err != nil {
		return err
	}

	if _, err := os.Stat(realPath); os.IsNotExist(err) {
		return fmt.Errorf("shim is not installed (original %s not found)", realPath)
	}

	// Verify what sits at emuPath before deleting it. If the emulator package was
	// reinstalled while the shim was active, emuPath is a *fresh real binary* and
	// realPath is stale — the previous code deleted the fresh binary and renamed
	// the stale one over it, silently downgrading the emulator.
	emuExists := true
	isShim := false
	if _, statErr := os.Stat(emuPath); statErr != nil {
		if !os.IsNotExist(statErr) {
			return statErr
		}
		emuExists = false
	} else {
		isShim, err = hasShimHeader(emuPath)
		if err != nil {
			return fmt.Errorf("could not inspect %s: %w", emuPath, err)
		}
	}

	if emuExists && !isShim {
		return fmt.Errorf("%s is not an avdslim shim — it looks like a real emulator binary, "+
			"probably reinstalled by sdkmanager while the shim was active. Refusing to replace it with "+
			"the older backup. Delete the stale backup instead: rm %s", emuPath, realPath)
	}

	if emuExists {
		if err := os.Remove(emuPath); err != nil {
			return fmt.Errorf("failed to remove shim at %s: %w", emuPath, err)
		}
	}

	if err := os.Rename(realPath, emuPath); err != nil {
		return fmt.Errorf("failed to restore original emulator: %w", err)
	}

	return nil
}

func writeShimScript(emuPath, realPath string, defaultRam int) error {
	script := fmt.Sprintf(`#!/bin/bash
%s — transparently injects low-memory flags
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REAL_EMU="$DIR/emulator.real"

if [ ! -f "$REAL_EMU" ]; then
    echo "❌ [avdslim] Error: Real emulator binary not found at $REAL_EMU" >&2
    echo "   The shim is installed but its backup of the original binary is gone." >&2
    echo "   Reinstall the emulator package via sdkmanager, then run:" >&2
    echo "     avdslim uninstall-shim" >&2
    exit 1
fi

HAS_MEM=0
HAS_LOWRAM=0
NO_LOWRAM=0
NO_SLIM=0
HEADLESS=0
HAS_SNAP=0
AVD_NAME=""

PREV=""
for arg in "$@"; do
    if [ "$arg" = "-memory" ]; then HAS_MEM=1; fi
    if [ "$arg" = "-lowram" ]; then HAS_LOWRAM=1; fi
    if [ "$arg" = "--no-lowram" ] || [ "$arg" = "-no-lowram" ]; then NO_LOWRAM=1; fi
    if [ "$arg" = "--no-slim" ] || [ "$arg" = "-no-slim" ]; then NO_SLIM=1; fi
    if [ "$arg" = "--headless" ] || [ "$arg" = "-no-window" ]; then HEADLESS=1; fi
    if [ "$arg" = "-snapshot" ] || [ "$arg" = "-no-snapshot" ] || [ "$arg" = "--cold" ]; then HAS_SNAP=1; fi
    if [ "$PREV" = "-avd" ]; then AVD_NAME="$arg"; fi
    PREV="$arg"
done

EXTRA=()
if [ "$NO_SLIM" -ne 1 ]; then
    if [ "$HAS_MEM" -eq 0 ]; then
        EXTRA+=("-memory" "%d")
    fi
    if [ "$HAS_LOWRAM" -eq 0 ] && [ "$NO_LOWRAM" -eq 0 ]; then
        EXTRA+=("-lowram")
    fi
    if [ "$HEADLESS" -eq 1 ]; then
        EXTRA+=("-no-window")
    fi
    if [ "$HAS_SNAP" -eq 0 ] && [ -n "$AVD_NAME" ]; then
        # Honour ANDROID_AVD_HOME like the Go code's GetAvdBaseDir does. Hardcoding
        # $HOME/.android/avd meant the shim could not find a golden snapshot that
        # 'avdslim bake' had just written elsewhere.
        AVD_HOME="${ANDROID_AVD_HOME:-$HOME/.android/avd}"
        SNAP_DIR="$AVD_HOME/${AVD_NAME}.avd/snapshots/avdslim_clean"
        if [ -d "$SNAP_DIR" ]; then
            EXTRA+=("-snapshot" "avdslim_clean" "-no-snapshot-save")
            # Announce this. Booting a golden snapshot with -no-snapshot-save means
            # the session's state is discarded on exit, which is extremely
            # confusing when it happens silently behind Android Studio's Play button.
            echo "ℹ️  [avdslim] Booting golden snapshot 'avdslim_clean'; changes this session will NOT persist." >&2
            echo "   Pass --no-slim, or run 'avdslim unbake $AVD_NAME', to boot normally." >&2
        fi
    fi
    EXTRA+=("-no-audio" "-camera-back" "none" "-camera-front" "none")
    echo "ℹ️  [avdslim] Injecting: ${EXTRA[*]}" >&2
fi

exec "$REAL_EMU" "${EXTRA[@]}" "$@"
`, shimHeader, defaultRam)

	// Write to a temp file and rename into place, so a partial write can never
	// leave a truncated script standing in for the emulator binary.
	tmp, err := os.CreateTemp(filepath.Dir(emuPath), ".avdslim-shim-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, emuPath)
}
