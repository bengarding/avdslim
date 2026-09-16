package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAvdName_AcceptsRealNames(t *testing.T) {
	// Shapes Android Studio and avdmanager actually produce.
	valid := []string{
		"Pixel_10_Pro",
		"Pixel_7_API_34",
		"Nexus5X",
		"my-avd",
		"avd.1",
		"a",
		"Pixel_Tablet_API_35_ps16k",
	}
	for _, name := range valid {
		if err := ValidateAvdName(name); err != nil {
			t.Errorf("ValidateAvdName(%q) = %v, want nil", name, err)
		}
	}
}

func TestValidateAvdName_RejectsTraversal(t *testing.T) {
	// Each of these previously flowed into filepath.Join + os.RemoveAll.
	invalid := []string{
		"",
		".",
		"..",
		"../evil",
		"../../../../tmp/x",
		"foo/bar",
		`foo\bar`,
		"/etc",
		"/absolute/path",
		"a..b",
		"snapshots/../../..",
		strings.Repeat("a", maxAvdNameLen+1),
	}
	for _, name := range invalid {
		if err := ValidateAvdName(name); err == nil {
			t.Errorf("ValidateAvdName(%q) = nil, want error", name)
		}
	}
}

func TestValidateAvdName_RejectsShellAndGlobMetacharacters(t *testing.T) {
	// AVD names reach the emulator console and get printed into shell hints,
	// so keep the charset tight rather than relying on downstream quoting.
	for _, name := range []string{
		"a b", "a;rm -rf /", "a$(id)", "a`id`", "a|b", "a&b", "a*", "a?", "a\nb",
		"a'b", `a"b`, "a\x00b",
	} {
		if err := ValidateAvdName(name); err == nil {
			t.Errorf("ValidateAvdName(%q) = nil, want error", name)
		}
	}
}

func TestAvdDir_StaysUnderBaseDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ANDROID_AVD_HOME", base)

	dir, err := AvdDir("Pixel_10_Pro")
	if err != nil {
		t.Fatalf("AvdDir returned error: %v", err)
	}
	want := filepath.Join(base, "Pixel_10_Pro.avd")
	if dir != want {
		t.Errorf("AvdDir = %q, want %q", dir, want)
	}
	if filepath.Dir(dir) != filepath.Clean(base) {
		t.Errorf("AvdDir escaped base: %q not directly under %q", dir, base)
	}
}

func TestAvdDir_HonoursAndroidAvdHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ANDROID_AVD_HOME", base)

	dir, err := AvdDir("Test_AVD")
	if err != nil {
		t.Fatalf("AvdDir returned error: %v", err)
	}
	if !strings.HasPrefix(dir, base) {
		t.Errorf("AvdDir = %q, want it under ANDROID_AVD_HOME %q", dir, base)
	}
}

func TestAvdDir_RejectsTraversal(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ANDROID_AVD_HOME", base)

	for _, name := range []string{"../escape", "../../etc", "a/b"} {
		if _, err := AvdDir(name); err == nil {
			t.Errorf("AvdDir(%q) = nil error, want rejection", name)
		}
	}
}

func TestSnapshotDir_ScopesToNamedSnapshot(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ANDROID_AVD_HOME", base)

	dir, err := SnapshotDir("Pixel_10_Pro", GoldenSnapshotName)
	if err != nil {
		t.Fatalf("SnapshotDir returned error: %v", err)
	}
	want := filepath.Join(base, "Pixel_10_Pro.avd", "snapshots", GoldenSnapshotName)
	if dir != want {
		t.Errorf("SnapshotDir = %q, want %q", dir, want)
	}
	// Must never resolve to the snapshots/ parent, which would delete them all.
	if dir == filepath.Join(base, "Pixel_10_Pro.avd", "snapshots") {
		t.Error("SnapshotDir resolved to the whole snapshots/ directory")
	}
}

func TestSnapshotDir_RejectsTraversalInSnapshotName(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ANDROID_AVD_HOME", base)

	// `avdslim snapshot --tag=..` must not resolve to snapshots/ itself.
	for _, snap := range []string{"..", "../..", "a/b", "", "."} {
		if _, err := SnapshotDir("Pixel_10_Pro", snap); err == nil {
			t.Errorf("SnapshotDir(_, %q) = nil error, want rejection", snap)
		}
	}
}
