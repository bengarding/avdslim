package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// maxAvdNameLen is a generous upper bound; real AVD names are far shorter.
const maxAvdNameLen = 255

// ValidateAvdName rejects anything that is not a plain AVD directory name.
//
// AVD names reach this program from argv (`avdslim unbake <name>`), from the
// emulator console (`adb emu avd name`), and from CI wrappers that may derive
// them from a branch name or PR title. They are then joined onto the AVD base
// directory and handed to os.RemoveAll, so a name containing a path separator
// or ".." would let a delete escape the AVD tree entirely.
//
// The allowed set matches what avdmanager itself accepts for AVD names.
func ValidateAvdName(name string) error {
	if name == "" {
		return fmt.Errorf("AVD name is empty")
	}
	if len(name) > maxAvdNameLen {
		return fmt.Errorf("AVD name is too long (%d chars, max %d)", len(name), maxAvdNameLen)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid AVD name %q", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("AVD name %q must not contain a path separator", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf(`AVD name %q must not contain ".."`, name)
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("AVD name %q must not be an absolute path", name)
	}
	for _, r := range name {
		ok := r == '.' || r == '_' || r == '-' ||
			(r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z')
		if !ok {
			return fmt.Errorf("AVD name %q contains unsupported character %q (allowed: letters, digits, '.', '_', '-')", name, r)
		}
	}
	return nil
}

// ValidateSnapshotName applies the same rules to a snapshot name, which can be
// supplied by the user via `avdslim snapshot --tag=<name>`.
func ValidateSnapshotName(name string) error {
	if err := ValidateAvdName(name); err != nil {
		return fmt.Errorf("invalid snapshot name: %w", err)
	}
	return nil
}

// AvdDir returns the .avd directory for name, honouring $ANDROID_AVD_HOME.
//
// Callers must use this rather than joining onto a hardcoded ~/.android/avd,
// both so that ANDROID_AVD_HOME is respected and so that the name is validated
// before it can reach a filesystem operation.
func AvdDir(name string) (string, error) {
	if err := ValidateAvdName(name); err != nil {
		return "", err
	}
	base := GetAvdBaseDir()
	dir := filepath.Join(base, name+".avd")

	// Defence in depth: even if validation above is ever loosened, the result
	// must still sit directly beneath the AVD base directory.
	if filepath.Dir(dir) != filepath.Clean(base) {
		return "", fmt.Errorf("refusing to operate on %q: resolves outside %s", name, base)
	}
	return dir, nil
}

// SnapshotsDir returns the directory holding all snapshots for an AVD.
func SnapshotsDir(avdName string) (string, error) {
	avdDir, err := AvdDir(avdName)
	if err != nil {
		return "", err
	}
	return filepath.Join(avdDir, "snapshots"), nil
}

// SnapshotDir returns the directory for one named snapshot of an AVD.
func SnapshotDir(avdName, snapshotName string) (string, error) {
	if err := ValidateSnapshotName(snapshotName); err != nil {
		return "", err
	}
	snapsDir, err := SnapshotsDir(avdName)
	if err != nil {
		return "", err
	}
	return filepath.Join(snapsDir, snapshotName), nil
}

// GoldenSnapshotName is the snapshot avdslim bakes and boots from.
const GoldenSnapshotName = "avdslim_clean"

// Is16KPageSize reports whether an AVD's config describes a 16 KB page-size
// system image, for which QEMU enforces a 4096 MB RAM floor.
//
// Both tag.id and tag.ids are consulted. doctor previously read only tag.ids
// while tune-avd read only tag.id, so the two commands could disagree about the
// same AVD — one warning about the 4 GB floor while the other reported a
// successful tune to 1024 MB.
// Every known 16 KB marker is checked against every field that can carry one.
// The original code only looked for "page_size_16kb" in the tag and "ps16k" /
// "16kb" in image.sysdir.1, so an AVD tagged google_apis_ps16k whose sysdir did
// not also spell it out went undetected — and was then "tuned" to 1024 MB with
// no warning that QEMU would silently hold it at the 4096 MB floor.
func Is16KPageSize(cfg map[string]string) bool {
	markers := []string{"page_size_16kb", "ps16k", "16kb"}
	for _, field := range []string{"tag.id", "tag.ids", "image.sysdir.1"} {
		v := strings.ToLower(cfg[field])
		for _, m := range markers {
			if strings.Contains(v, m) {
				return true
			}
		}
	}
	return false
}

// IsPlayStoreImage reports whether an AVD uses a Google Play production image,
// which runs Play Store updaters and blocks `adb root`.
func IsPlayStoreImage(cfg map[string]string) bool {
	switch strings.ToLower(cfg["PlayStore.enabled"]) {
	case "true", "yes":
		return true
	}
	for _, key := range []string{"tag.id", "tag.ids"} {
		if strings.Contains(cfg[key], "playstore") {
			return true
		}
	}
	return false
}
