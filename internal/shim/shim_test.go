package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeSdk builds a throwaway SDK tree with a stand-in emulator binary and points
// ANDROID_HOME at it, so these tests need no real Android SDK.
func fakeSdk(t *testing.T, emulatorContents string) (sdkDir, emuPath, realPath string) {
	t.Helper()

	sdkDir = t.TempDir()
	emuDir := filepath.Join(sdkDir, "emulator")
	if err := os.MkdirAll(emuDir, 0o755); err != nil {
		t.Fatal(err)
	}
	emuPath = filepath.Join(emuDir, "emulator")
	realPath = filepath.Join(emuDir, "emulator.real")

	if emulatorContents != "" {
		if err := os.WriteFile(emuPath, []byte(emulatorContents), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("ANDROID_HOME", sdkDir)
	t.Setenv("ANDROID_SDK_ROOT", "")
	return sdkDir, emuPath, realPath
}

const fakeRealBinary = "\x7fELF fake emulator binary contents"

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shim install is unsupported on Windows by design")
	}
}

func TestInstallShim_BacksUpRealBinaryAndWritesScript(t *testing.T) {
	skipOnWindows(t)
	_, emuPath, realPath := fakeSdk(t, fakeRealBinary)

	if err := InstallShim(1024); err != nil {
		t.Fatalf("InstallShim: %v", err)
	}

	// Original preserved verbatim.
	got, err := os.ReadFile(realPath)
	if err != nil {
		t.Fatalf("reading backup: %v", err)
	}
	if string(got) != fakeRealBinary {
		t.Error("backup does not match the original binary byte-for-byte")
	}

	// emuPath is now the shim, and is executable.
	shimBody, err := os.ReadFile(emuPath)
	if err != nil {
		t.Fatalf("reading shim: %v", err)
	}
	if !strings.Contains(string(shimBody), shimHeader) {
		t.Error("shim script is missing its header marker")
	}
	if !strings.Contains(string(shimBody), "-memory") {
		t.Error("shim script does not inject -memory")
	}
	info, err := os.Stat(emuPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("shim mode = %v, want executable", info.Mode().Perm())
	}

	installed, err := IsShimInstalled()
	if err != nil {
		t.Fatalf("IsShimInstalled: %v", err)
	}
	if !installed {
		t.Error("IsShimInstalled = false after a successful install")
	}
}

func TestUninstallShim_RestoresOriginalExactly(t *testing.T) {
	skipOnWindows(t)
	_, emuPath, realPath := fakeSdk(t, fakeRealBinary)

	if err := InstallShim(1024); err != nil {
		t.Fatalf("InstallShim: %v", err)
	}
	if err := UninstallShim(); err != nil {
		t.Fatalf("UninstallShim: %v", err)
	}

	got, err := os.ReadFile(emuPath)
	if err != nil {
		t.Fatalf("reading restored binary: %v", err)
	}
	if string(got) != fakeRealBinary {
		t.Error("restored emulator does not match the original")
	}
	if _, err := os.Stat(realPath); !os.IsNotExist(err) {
		t.Error("backup file still present after uninstall")
	}
}

// The regression that mattered most: sdkmanager reinstalls the emulator while
// the shim is active, so emuPath is a NEW real binary and realPath is a stale
// copy. The old code deleted the new binary and renamed the stale one over it.
func TestUninstallShim_RefusesToDowngradeAfterSdkUpdate(t *testing.T) {
	skipOnWindows(t)
	_, emuPath, realPath := fakeSdk(t, fakeRealBinary)

	if err := InstallShim(1024); err != nil {
		t.Fatalf("InstallShim: %v", err)
	}

	// Simulate sdkmanager dropping a newer real binary at emuPath.
	const newerBinary = "\x7fELF NEWER emulator binary"
	if err := os.WriteFile(emuPath, []byte(newerBinary), 0o755); err != nil {
		t.Fatal(err)
	}

	err := UninstallShim()
	if err == nil {
		t.Fatal("UninstallShim succeeded; it should refuse to overwrite a real binary with the stale backup")
	}
	if !strings.Contains(err.Error(), "not an avdslim shim") {
		t.Errorf("error = %q, want it to explain that emuPath is not a shim", err)
	}

	// Crucially, the newer binary must survive.
	got, err := os.ReadFile(emuPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != newerBinary {
		t.Error("the newer emulator binary was destroyed")
	}
	if _, err := os.Stat(realPath); err != nil {
		t.Error("stale backup was removed even though uninstall refused")
	}
}

func TestInstallShim_RefusesWhenBothLookLikeRealBinaries(t *testing.T) {
	skipOnWindows(t)
	_, emuPath, realPath := fakeSdk(t, fakeRealBinary)

	// A stale backup left behind by an SDK reinstall.
	if err := os.WriteFile(realPath, []byte("\x7fELF stale"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := InstallShim(1024)
	if err == nil {
		t.Fatal("InstallShim succeeded; it should refuse when both paths hold real binaries")
	}
	if !strings.Contains(err.Error(), "reinstalled") {
		t.Errorf("error = %q, want it to mention the reinstall situation", err)
	}
	// The real binary must not have been clobbered.
	got, _ := os.ReadFile(emuPath)
	if string(got) != fakeRealBinary {
		t.Error("InstallShim modified the emulator despite refusing")
	}
}

func TestInstallShim_IsIdempotentAndUpdatesRam(t *testing.T) {
	skipOnWindows(t)
	_, emuPath, realPath := fakeSdk(t, fakeRealBinary)

	if err := InstallShim(1024); err != nil {
		t.Fatalf("first InstallShim: %v", err)
	}
	if err := InstallShim(2048); err != nil {
		t.Fatalf("second InstallShim: %v", err)
	}

	// The backup must still be the original, not a copy of the shim script.
	got, err := os.ReadFile(realPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != fakeRealBinary {
		t.Error("re-running InstallShim corrupted the backup")
	}

	shimBody, _ := os.ReadFile(emuPath)
	if !strings.Contains(string(shimBody), "2048") {
		t.Error("re-running InstallShim did not update the RAM value")
	}
}

func TestInstallShim_ReportsHalfInstalledState(t *testing.T) {
	skipOnWindows(t)
	// emulator missing, backup present: the shim script was deleted somehow.
	_, _, realPath := fakeSdk(t, "")
	if err := os.WriteFile(realPath, []byte(fakeRealBinary), 0o755); err != nil {
		t.Fatal(err)
	}

	err := InstallShim(1024)
	if err == nil {
		t.Fatal("InstallShim succeeded on a half-installed SDK")
	}
	if !strings.Contains(err.Error(), "half-installed") {
		t.Errorf("error = %q, want it to name the half-installed state", err)
	}
}

func TestIsShimInstalled_FalseForStockSdk(t *testing.T) {
	fakeSdk(t, fakeRealBinary)

	installed, err := IsShimInstalled()
	if err != nil {
		t.Fatalf("IsShimInstalled: %v", err)
	}
	if installed {
		t.Error("IsShimInstalled = true for a stock SDK")
	}
}

func TestHasShimHeader_DoesNotReadWholeBinary(t *testing.T) {
	// The marker must be found near the start; a marker buried past the probe
	// window should not count, which is what keeps us from reading ~700MB.
	dir := t.TempDir()
	buried := filepath.Join(dir, "buried")
	body := strings.Repeat("x", headerProbeBytes*2) + shimHeader
	if err := os.WriteFile(buried, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	found, err := hasShimHeader(buried)
	if err != nil {
		t.Fatalf("hasShimHeader: %v", err)
	}
	if found {
		t.Error("hasShimHeader matched a marker beyond the probe window")
	}
}

func TestHasShimHeader_ReportsMissingFile(t *testing.T) {
	// Errors must surface rather than being reported as "not a shim", which
	// previously let InstallShim rename a real binary on top of an existing shim.
	if _, err := hasShimHeader(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("hasShimHeader returned nil error for a missing file")
	}
}
