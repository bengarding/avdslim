package config

import (
	"os"
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

func TestIs16KPageSize_ChecksBothTagKeys(t *testing.T) {
	// doctor read only tag.ids while tune-avd read only tag.id, so the two
	// commands could disagree about the same AVD. Both keys must be consulted.
	for name, cfg := range map[string]map[string]string{
		"tag.id singular": {"tag.id": "google_apis_ps16k"},
		"tag.ids plural":  {"tag.ids": "google_apis_page_size_16kb"},
		"tag.id 16kb":     {"tag.id": "android-page_size_16kb"},
		"sysdir ps16k":    {"image.sysdir.1": "system-images/android-36/google_apis_ps16k/arm64-v8a/"},
		"sysdir 16kb":     {"image.sysdir.1": "system-images/android-36/foo16kb/arm64-v8a/"},
	} {
		t.Run(name, func(t *testing.T) {
			if !Is16KPageSize(cfg) {
				t.Errorf("Is16KPageSize(%v) = false, want true", cfg)
			}
		})
	}

	for name, cfg := range map[string]map[string]string{
		"plain google apis": {"tag.id": "google_apis", "image.sysdir.1": "system-images/android-35/google_apis/arm64-v8a/"},
		"empty":             {},
	} {
		t.Run(name, func(t *testing.T) {
			if Is16KPageSize(cfg) {
				t.Errorf("Is16KPageSize(%v) = true, want false", cfg)
			}
		})
	}
}

func TestIsPlayStoreImage(t *testing.T) {
	for name, cfg := range map[string]map[string]string{
		"enabled true": {"PlayStore.enabled": "true"},
		"enabled yes":  {"PlayStore.enabled": "yes"},
		"enabled TRUE": {"PlayStore.enabled": "TRUE"},
		"tag.id":       {"tag.id": "google_apis_playstore"},
		"tag.ids":      {"tag.ids": "google_apis_playstore"},
	} {
		t.Run(name, func(t *testing.T) {
			if !IsPlayStoreImage(cfg) {
				t.Errorf("IsPlayStoreImage(%v) = false, want true", cfg)
			}
		})
	}

	for name, cfg := range map[string]map[string]string{
		"enabled false": {"PlayStore.enabled": "false"},
		"google apis":   {"tag.id": "google_apis"},
		"empty":         {},
	} {
		t.Run(name, func(t *testing.T) {
			if IsPlayStoreImage(cfg) {
				t.Errorf("IsPlayStoreImage(%v) = true, want false", cfg)
			}
		})
	}
}

// An AVD's name is not its directory name. Real case from a live SDK:
// "Pixel_10_Pro_API_37" created against android-37.0 is stored in
// "Pixel_10_Pro_API_37.0.avd". Assuming <name>.avd meant golden snapshots were
// never found, unbake reported nothing to remove, and restart silently failed to
// purge hardware-qemu.ini.
func TestAvdDir_FollowsIniPathWhenDirNameDiffers(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ANDROID_AVD_HOME", base)

	realDir := filepath.Join(base, "Pixel_10_Pro_API_37.0.avd")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ini := "avd.ini.encoding=UTF-8\npath=" + realDir + "\npath.rel=avd/Pixel_10_Pro_API_37.0.avd\ntarget=android-37.0\n"
	if err := os.WriteFile(filepath.Join(base, "Pixel_10_Pro_API_37.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := AvdDir("Pixel_10_Pro_API_37")
	if err != nil {
		t.Fatalf("AvdDir: %v", err)
	}
	if got != realDir {
		t.Errorf("AvdDir = %q, want %q (from the .ini path= key)", got, realDir)
	}
}

func TestAvdDir_FallsBackToConventionalLayout(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ANDROID_AVD_HOME", base)

	// No .ini present: the <name>.avd assumption is the only thing available.
	got, err := AvdDir("Pixel_9_Pro_API_36")
	if err != nil {
		t.Fatalf("AvdDir: %v", err)
	}
	if want := filepath.Join(base, "Pixel_9_Pro_API_36.avd"); got != want {
		t.Errorf("AvdDir = %q, want %q", got, want)
	}
}

func TestAvdDir_StillValidatesNameBeforeReadingIni(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ANDROID_AVD_HOME", base)

	for _, name := range []string{"../escape", "a/b", "..", ""} {
		if _, err := AvdDir(name); err == nil {
			t.Errorf("AvdDir(%q) = nil error, want rejection", name)
		}
	}
}

func TestListAvdNames_UsesIniNamesNotDirNames(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ANDROID_AVD_HOME", base)

	for _, d := range []string{"Pixel_10_Pro_API_37.0.avd", "Pixel_9_Pro_API_36.avd"} {
		if err := os.MkdirAll(filepath.Join(base, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, dir := range map[string]string{
		"Pixel_10_Pro_API_37": "Pixel_10_Pro_API_37.0.avd",
		"Pixel_9_Pro_API_36":  "Pixel_9_Pro_API_36.avd",
	} {
		ini := "path=" + filepath.Join(base, dir) + "\n"
		if err := os.WriteFile(filepath.Join(base, name+".ini"), []byte(ini), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := ListAvdNames()
	want := []string{"Pixel_10_Pro_API_37", "Pixel_9_Pro_API_36"}
	if len(got) != len(want) {
		t.Fatalf("ListAvdNames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ListAvdNames[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// The directory-derived name must not leak through.
	for _, n := range got {
		if strings.HasSuffix(n, ".0") {
			t.Errorf("ListAvdNames returned a directory-derived name: %q", n)
		}
	}
}
