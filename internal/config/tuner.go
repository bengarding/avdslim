package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// GetInstalledAvds returns one entry per AVD, keyed by config.ini keys, plus
// "name" (the name the emulator recognises) and "avdPath" (its data directory).
//
// Names come from the .ini filenames, not from directory names. Deriving the name
// from "<dir>.avd" reported e.g. "Pixel_10_Pro_API_37.0" for an AVD the emulator
// calls "Pixel_10_Pro_API_37", so `avdslim list` printed a name that every other
// command — and the emulator itself — then failed to resolve.
func GetInstalledAvds() []map[string]string {
	var list []map[string]string

	for _, name := range ListAvdNames() {
		dir, err := AvdDir(name)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, "config.ini"))
		if err != nil {
			continue
		}
		m := map[string]string{
			"name":    name,
			"avdPath": dir,
		}
		for _, line := range strings.Split(string(data), "\n") {
			idx := strings.Index(line, "=")
			if idx > 0 {
				m[strings.TrimSpace(line[:idx])] = strings.TrimSpace(line[idx+1:])
			}
		}
		list = append(list, m)
	}
	return list
}

func TuneAvd(targetAvd string, ramMb, heapMb int, gpuMode string) error {
	avdBase := GetAvdBaseDir()

	// Resolve through the AVD registry rather than walking for config.ini files.
	// The walk derived each AVD's name from its directory, which is not the AVD's
	// name, so `tune-avd Pixel_10_Pro_API_37` reported "not found" for an AVD
	// stored in Pixel_10_Pro_API_37.0.avd. The walk also recursed, so any nested
	// config.ini showed up as a phantom AVD.
	installed := GetInstalledAvds()
	if len(installed) == 0 {
		return fmt.Errorf("no AVD configurations found in %s", avdBase)
	}

	var avdName, fileToTune string
	if targetAvd != "" {
		for _, avd := range installed {
			if strings.EqualFold(avd["name"], targetAvd) {
				avdName = avd["name"]
				fileToTune = filepath.Join(avd["avdPath"], "config.ini")
				break
			}
		}
		if fileToTune == "" {
			return fmt.Errorf("AVD %q not found in %s", targetAvd, avdBase)
		}
	} else if len(installed) == 1 {
		avdName = installed[0]["name"]
		fileToTune = filepath.Join(installed[0]["avdPath"], "config.ini")
	} else {
		fmt.Println("Found multiple AVDs. Please specify one:")
		for _, avd := range installed {
			fmt.Printf("  • %s\n", avd["name"])
		}
		return fmt.Errorf("please specify AVD name")
	}
	fmt.Printf("⚙️  Tuning config.ini for %q (RAM: %dMB, Heap: %dMB)...\n", avdName, ramMb, heapMb)

	data, err := os.ReadFile(fileToTune)
	if err != nil {
		return err
	}

	backupFile := fileToTune + ".bak"
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		_ = os.WriteFile(backupFile, data, 0644)
		fmt.Printf("   ✓ Created backup: %s\n", backupFile)
	}

	lines := strings.Split(string(data), "\n")
	kv := make(map[string]string)
	for _, l := range lines {
		idx := strings.Index(l, "=")
		if idx > 0 {
			k := strings.TrimSpace(l[:idx])
			v := strings.TrimSpace(l[idx+1:])
			kv[k] = v
		}
	}

	if gpuMode == "" {
		gpuMode = GetRecommendedGpuMode()
	}

	kv["hw.ramSize"] = strconv.Itoa(ramMb)
	kv["vm.heapSize"] = strconv.Itoa(heapMb)
	kv["hw.camera.back"] = "none"
	kv["hw.camera.front"] = "none"
	kv["hw.audioInput"] = "no"
	kv["hw.audioOutput"] = "no"
	kv["hw.gpu.mode"] = gpuMode
	kv["hw.gpu.enabled"] = "yes"
	kv["hw.dPad"] = "no"
	kv["fastboot.forceColdBoot"] = "yes"
	kv["fastboot.forceFastBoot"] = "no"

	if err := os.WriteFile(fileToTune, []byte(renderConfigIni(lines, kv)), 0644); err != nil {
		return err
	}

	// Purge the stale runtime ini so the emulator does not restore the previous
	// hardware config. This is the file that actually carries the old RAM/GPU
	// values forward.
	//
	// Snapshots are deliberately NOT deleted here. Doing so destroyed every
	// snapshot for the AVD, including ones the user created themselves, with no
	// prompt and no way back. They are merely stale, not harmful: a snapshot
	// taken under the old RAM size will be rejected or cold-booted by QEMU.
	avdDir := filepath.Dir(fileToTune)
	_ = os.Remove(filepath.Join(avdDir, "hardware-qemu.ini"))
	_ = os.Remove(filepath.Join(avdDir, "hardware-qemu.ini.lock"))

	fmt.Printf("✅ Successfully tuned AVD %q!\n", avdName)
	fmt.Printf("   • Host RAM allocated: %d MB (prevents host memory pressure)\n", ramMb)
	fmt.Printf("   • VM Heap: %d MB\n", heapMb)
	fmt.Println("   • Hardware Audio & Camera: disabled (saves host threads/buffers)")
	fmt.Printf("   • GPU Mode: %s (%s)\n", gpuMode, GetGpuBackendDescription(gpuMode))
	fmt.Println("   • Runtime cache (hardware-qemu.ini): purged")

	// Tell the user their snapshots are now stale rather than silently deleting
	// them, and hand them the explicit command to remove them if they want.
	if snaps := listSnapshots(filepath.Join(avdDir, "snapshots")); len(snaps) > 0 {
		fmt.Printf("   ⚠️  %d existing snapshot(s) were captured under the previous config\n", len(snaps))
		fmt.Printf("      and may cold-boot instead of restoring: %s\n", strings.Join(snaps, ", "))
		fmt.Printf("      They have been left in place. To remove avdslim's golden snapshot:\n")
		fmt.Printf("        avdslim unbake %s\n", avdName)
	}

	is16K := Is16KPageSize(kv)
	isPlayStore := IsPlayStoreImage(kv)
	if is16K {
		fmt.Println("   🚨 Note: This AVD uses a 16 KB page-size image. QEMU enforces a 4096 MB RAM floor.")
		fmt.Println("      💡 Recommendation: For daily dev at 1024 MB RAM, use standard 4 KB 'Google APIs'.")
	} else if isPlayStore {
		fmt.Println("   ⚠️  Note: This AVD uses 'Google Play' (locks guest root & runs background updaters).")
		fmt.Println("      💡 Recommendation: For lowest RAM usage, use 'Google APIs' instead.")
	}

	fmt.Println()
	fmt.Printf("ℹ️  Note: If this emulator is currently running, restart it to apply changes:\n   avdslim restart %s\n\n", avdName)
	return nil
}

// renderConfigIni rewrites config.ini in place: existing keys keep their
// original position and get their new value, everything else (comments, blank
// lines, unrecognised syntax) is preserved verbatim, and genuinely new keys are
// appended in sorted order.
//
// The previous implementation rebuilt the file from a map, which dropped every
// line that did not contain "=" — comments and blank lines were silently
// deleted — and, because Go randomises map iteration, emitted the keys in a
// different order on every run, making the .bak diff useless for seeing what
// actually changed.
func renderConfigIni(originalLines []string, kv map[string]string) string {
	written := make(map[string]bool, len(kv))

	var sb strings.Builder
	for i, line := range originalLines {
		// Don't re-emit a trailing empty element produced by splitting on "\n".
		if i == len(originalLines)-1 && line == "" {
			continue
		}

		idx := strings.Index(line, "=")
		if idx > 0 {
			key := strings.TrimSpace(line[:idx])
			if val, ok := kv[key]; ok && !written[key] {
				sb.WriteString(key + "=" + val + "\n")
				written[key] = true
				continue
			}
		}
		sb.WriteString(line + "\n")
	}

	newKeys := make([]string, 0, len(kv))
	for k := range kv {
		if !written[k] {
			newKeys = append(newKeys, k)
		}
	}
	sort.Strings(newKeys)
	for _, k := range newKeys {
		sb.WriteString(k + "=" + kv[k] + "\n")
	}

	return sb.String()
}

// listSnapshots returns the names of snapshot subdirectories, or nil if there
// are none. Used to warn about stale snapshots instead of deleting them.
func listSnapshots(snapshotsDir string) []string {
	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		// The emulator keeps bookkeeping files alongside snapshot dirs.
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func HasGoldenSnapshot(avdName string) bool {
	snapDir, err := SnapshotDir(avdName, GoldenSnapshotName)
	if err != nil {
		return false
	}
	info, err := os.Stat(snapDir)
	return err == nil && info.IsDir()
}
