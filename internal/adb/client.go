package adb

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/kdbhalala/avdslim/internal/bloat"
	"github.com/kdbhalala/avdslim/internal/config"
)

const StateFilePath = "/data/local/tmp/avdslim_state.json"

type RunningEmulator struct {
	Serial         string
	Model          string
	AndroidVersion string
	ApiLevel       string
	HostPid        int
	HostRssMb      int
	IsSlimmed      bool
}

type SlimState struct {
	Timestamp        string   `json:"timestamp"`
	DisabledPackages []string `json:"disabled_packages"`
	Preset           string   `json:"preset"`

	// PriorSettings records each guest setting avdslim changed, keyed
	// "<namespace>/<key>", holding the value read from the device *before* the
	// change. "null" means the setting was unset. `off` replays these so it puts
	// back what was actually there instead of guessing at stock defaults.
	PriorSettings map[string]string `json:"prior_settings,omitempty"`
}

// SlimOptions controls what Slim changes on the guest.
type SlimOptions struct {
	Aggressive   bool
	KeepPackages []string

	// DisableAnimations zeroes window/transition/animator scales. Opt-in: it
	// changes how the guest behaves, not just how much RAM it uses. With the
	// animator duration at 0, some View/Compose animations invoke their end
	// callbacks synchronously, which can hide or manufacture races in UI tests.
	DisableAnimations bool
}

// guestSetting is a single `settings put` that avdslim performs.
type guestSetting struct {
	Namespace string // global | secure | system
	Key       string
	Value     string

	// Restore is false for settings that must not be reverted. Putting
	// user_setup_complete back to 0 would re-trigger the setup wizard.
	Restore bool
}

// memorySettings reduce resource use without altering observable app behaviour.
var memorySettings = []guestSetting{
	{Namespace: "global", Key: "background_process_limit", Value: "4", Restore: true},
	{Namespace: "global", Key: "auto_sync", Value: "0", Restore: true},
	{Namespace: "secure", Key: "location_mode", Value: "0", Restore: true},
}

// provisioningSettings skip first-run setup. Deliberately never restored.
var provisioningSettings = []guestSetting{
	{Namespace: "secure", Key: "user_setup_complete", Value: "1", Restore: false},
	{Namespace: "global", Key: "device_provisioned", Value: "1", Restore: false},
}

// animationSettings are applied only when SlimOptions.DisableAnimations is set.
var animationSettings = []guestSetting{
	{Namespace: "global", Key: "window_animation_scale", Value: "0", Restore: true},
	{Namespace: "global", Key: "transition_animation_scale", Value: "0", Restore: true},
	{Namespace: "global", Key: "animator_duration_scale", Value: "0", Restore: true},
}

func settingKey(s guestSetting) string { return s.Namespace + "/" + s.Key }

// unsetSettingValue is what `settings get` prints for a key that has no value,
// and what we record to mean "delete this key on restore". It matches Android's
// own output so a recorded value round-trips unambiguously.
const unsetSettingValue = "null"

// getSetting reads a guest setting.
//
// ok is false when the read itself failed, which is deliberately distinct from
// the setting being unset (value "null"). Conflating the two is dangerous:
// Restore deletes any key recorded as "null", so treating a failed read as
// "unset" would make `off` delete a setting that had a perfectly good value.
func (c *Client) getSetting(serial, namespace, key string) (string, bool) {
	out, err := c.Exec("-s", serial, "shell", "settings", "get", namespace, key)
	if err != nil {
		return "", false
	}
	v := strings.TrimSpace(out)
	if v == "" {
		return unsetSettingValue, true
	}
	return v, true
}

// applySettings records the pre-change value of each restorable setting, then
// writes the new value. Recorded values accumulate into prior for persistence.
func (c *Client) applySettings(serial string, settings []guestSetting, prior map[string]string) {
	for _, s := range settings {
		if s.Restore {
			if _, seen := prior[settingKey(s)]; !seen {
				if v, ok := c.getSetting(serial, s.Namespace, s.Key); ok {
					prior[settingKey(s)] = v
				} else {
					// Record nothing rather than guessing. An unrecorded setting
					// is left untouched by `off`, which is the safe failure mode;
					// recording "null" would have it deleted.
					fmt.Printf("   ⚠️  Could not read %s before changing it; `avdslim off` will leave it as-is.\n", settingKey(s))
				}
			}
		}
		c.Exec("-s", serial, "shell", "settings", "put", s.Namespace, s.Key, s.Value)
	}
}

// writeStateFile writes the state JSON via stdin rather than building an
// `echo '...' > file` shell string, so no value in the JSON can affect quoting.
func (c *Client) writeStateFile(serial string, state SlimState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultExecTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.adbPath, "-s", serial, "shell", "cat > "+StateFilePath)
	cmd.Stdin = bytes.NewReader(payload)
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("writing state file timed out after %s", DefaultExecTimeout)
		}
		return err
	}
	return nil
}

type Client struct {
	adbPath string
}

func NewClient() *Client {
	return &Client{
		adbPath: findAdb(),
	}
}

// NewClientWithPath returns a Client that shells out to a specific adb binary.
// Used by tests to substitute a stub so they need no emulator.
func NewClientWithPath(adbPath string) *Client {
	return &Client{adbPath: adbPath}
}

const (
	// DefaultExecTimeout bounds an ordinary adb invocation. Without a bound, a
	// wedged adb server or an unauthorized device leaves the caller blocked
	// forever — `avdslim watch` would sit there silently doing nothing.
	DefaultExecTimeout = 30 * time.Second

	// BlockingExecTimeout covers subcommands that are *meant* to wait on device
	// state, such as wait-for-device.
	BlockingExecTimeout = 5 * time.Minute
)

// blockingSubcommands legitimately block until the device changes state, so
// they get the longer bound rather than the default.
var blockingSubcommands = map[string]bool{
	"wait-for-device":       true,
	"wait-for-any-device":   true,
	"wait-for-boot":         true,
	"wait-for-usb-device":   true,
	"wait-for-local-device": true,
}

// Exec runs adb with a timeout appropriate for the subcommand.
func (c *Client) Exec(args ...string) (string, error) {
	return c.ExecTimeout(timeoutFor(args), args...)
}

// ExecTimeout runs adb, killing it if it exceeds d.
func (c *Client) ExecTimeout(d time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.adbPath, args...)
	out, err := cmd.CombinedOutput()

	// CommandContext reports the kill as a generic "signal: killed", which tells
	// the user nothing. Surface the timeout explicitly instead.
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("adb %s timed out after %s", strings.Join(args, " "), d)
	}
	return string(out), err
}

func timeoutFor(args []string) time.Duration {
	for _, a := range args {
		if blockingSubcommands[a] {
			return BlockingExecTimeout
		}
	}
	return DefaultExecTimeout
}

// AttachedEmulator is a serial plus its adb connection state, as reported by
// `adb devices` alone.
type AttachedEmulator struct {
	Serial string
	State  string // device | offline | unauthorized | ...
}

// ListEmulatorSerials runs only `adb devices` and returns every emulator entry
// with its state.
//
// GetRunningEmulators costs five adb invocations per device (devices, three
// getprops and a cat of the state file), which makes it far too expensive for a
// polling loop — and worse, the getprop/cat calls run `adb shell` against a
// device that may be mid-boot or mid-shutdown, exactly when shell calls stall.
// Polling loops use this instead: one invocation, no shell, predictable timing.
func (c *Client) ListEmulatorSerials() ([]AttachedEmulator, error) {
	out, err := c.Exec("devices")
	if err != nil {
		return nil, err
	}

	var list []AttachedEmulator
	scanner := bufio.NewScanner(strings.NewReader(out))
	isFirst := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if isFirst {
			isFirst = false
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 && strings.HasPrefix(parts[0], "emulator-") {
			list = append(list, AttachedEmulator{Serial: parts[0], State: parts[1]})
		}
	}
	return list, nil
}

func (c *Client) GetRunningEmulators() ([]RunningEmulator, error) {
	out, err := c.Exec("devices")
	if err != nil {
		return nil, err
	}

	var list []RunningEmulator
	scanner := bufio.NewScanner(strings.NewReader(out))
	isFirst := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if isFirst {
			isFirst = false
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[1] == "device" && strings.HasPrefix(parts[0], "emulator-") {
			serial := parts[0]
			model, _ := c.Exec("-s", serial, "shell", "getprop", "ro.product.model")
			ver, _ := c.Exec("-s", serial, "shell", "getprop", "ro.build.version.release")
			sdk, _ := c.Exec("-s", serial, "shell", "getprop", "ro.build.version.sdk")

			isSlimmed := c.HasSlimState(serial)

			list = append(list, RunningEmulator{
				Serial:         serial,
				Model:          strings.TrimSpace(model),
				AndroidVersion: strings.TrimSpace(ver),
				ApiLevel:       strings.TrimSpace(sdk),
				IsSlimmed:      isSlimmed,
			})
		}
	}
	return list, nil
}

// HasSlimState reports whether avdslim's state file is present on the device.
//
// This used to be `ls <path>` plus strings.Contains(out, "avdslim_state.json"),
// which was always true: when the file is missing, ls prints
// "ls: /data/local/tmp/avdslim_state.json: No such file or directory" — an error
// message that contains the filename. Every emulator therefore reported as
// already slimmed, which made `avdslim watch` skip every device it saw and
// silently never slim anything, and made `list`/`doctor` always print SLIMMED.
//
// Checking for a marker from inside the file's contents cannot be satisfied by
// an error message about the file.
func (c *Client) HasSlimState(serial string) bool {
	out, err := c.Exec("-s", serial, "shell", "cat", StateFilePath)
	return err == nil && strings.Contains(out, "disabled_packages")
}

// GetAvdName returns the AVD name for a running emulator.
//
// `adb -s <serial> emu avd name` replies with the name followed by an "OK" line,
// or "KO: <reason>" on failure. Callers previously each re-implemented this
// parsing and then matched with strings.Contains, which collides between AVDs
// whose names share a prefix (asking for "Pixel_5" matched a running
// "Pixel_5_API_34").
func (c *Client) GetAvdName(serial string) (string, error) {
	out, err := c.Exec("-s", serial, "emu", "avd", "name")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "OK" {
			continue
		}
		if strings.HasPrefix(line, "KO") {
			return "", fmt.Errorf("emulator console refused avd name request: %s", line)
		}
		return line, nil
	}
	return "", fmt.Errorf("could not determine AVD name for %s", serial)
}

// IsAvd reports whether the emulator at serial is running the named AVD, using
// an exact (case-insensitive) comparison rather than a substring match.
func (c *Client) IsAvd(serial, avdName string) bool {
	got, err := c.GetAvdName(serial)
	if err != nil {
		return false
	}
	return strings.EqualFold(got, avdName)
}

func (c *Client) ResolveDevice(args []string) (string, error) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		return args[0], nil
	}
	running, err := c.GetRunningEmulators()
	if err != nil || len(running) == 0 {
		return "", fmt.Errorf("no running Android emulator found via adb")
	}
	if len(running) == 1 {
		return running[0].Serial, nil
	}
	fmt.Println("⚠️  Multiple emulators running. Please specify one:")
	for _, r := range running {
		fmt.Printf("  • %s (%s)\n", r.Serial, r.Model)
	}
	return "", fmt.Errorf("device serial required")
}

func (c *Client) Slim(serial string, opts SlimOptions) (int, error) {
	targetPackages := make([]string, 0, 50)
	for _, pkgs := range bloat.StandardBloatCategories {
		targetPackages = append(targetPackages, pkgs...)
	}
	if opts.Aggressive {
		targetPackages = append(targetPackages, bloat.AggressiveBloatPackages...)
	}

	keepMap := make(map[string]bool)
	for _, k := range opts.KeepPackages {
		keepMap[k] = true
	}

	installedRaw, _ := c.Exec("-s", serial, "shell", "pm", "list", "packages")
	installedMap := make(map[string]bool)
	for _, l := range strings.Split(installedRaw, "\n") {
		clean := strings.TrimSpace(strings.ReplaceAll(l, "package:", ""))
		if clean != "" {
			installedMap[clean] = true
		}
	}

	disabledList := make([]string, 0, len(targetPackages))
	for _, pkg := range targetPackages {
		if !installedMap[pkg] || keepMap[pkg] {
			continue
		}
		res, _ := c.Exec("-s", serial, "shell", "pm", "disable-user", "--user", "0", pkg)
		if strings.Contains(res, "disabled-user") || strings.Contains(res, "new state") {
			disabledList = append(disabledList, pkg)
			fmt.Printf("   ✓ Disabled: %s\n", pkg)
		}
	}

	// Apply settings, recording each prior value first so `off` can put back
	// exactly what was there.
	prior := make(map[string]string)
	c.applySettings(serial, memorySettings, prior)
	c.applySettings(serial, provisioningSettings, prior)
	if opts.DisableAnimations {
		c.applySettings(serial, animationSettings, prior)
		fmt.Println("   ✓ Animations disabled (window/transition/animator scale = 0)")
	}

	// Persist state JSON. Written after the settings pass so PriorSettings is
	// populated; a state file without it would leave `off` unable to restore.
	preset := "Standard"
	if opts.Aggressive {
		preset = "Aggressive"
	}
	state := SlimState{
		Timestamp:        time.Now().Format(time.RFC3339),
		DisabledPackages: disabledList,
		Preset:           preset,
		PriorSettings:    prior,
	}
	if err := c.writeStateFile(serial, state); err != nil {
		fmt.Printf("   ⚠️  Could not persist slim state (%v); `avdslim off` will not be able to restore settings.\n", err)
	}

	// Trim memory
	c.Exec("-s", serial, "shell", "am", "kill-all")
	// `am trim-memory` takes a single process, not "--all": the old call was
	// rejected by activity manager on every run and trimmed nothing. kill-all
	// above plus the pagecache drop below are what actually reclaim memory.
	c.Exec("-s", serial, "shell", "su", "0", "sync")
	c.Exec("-s", serial, "shell", "su", "0", "echo 3 > /proc/sys/vm/drop_caches")

	return len(disabledList), nil
}

// Restore reverts what avdslim recorded changing. hadState reports whether a
// state file was found; without one there is nothing to revert, and the caller
// must not claim success.
func (c *Client) Restore(serial string) (restored int, hadState bool, err error) {
	var state SlimState
	haveState := false

	stateRaw, err := c.Exec("-s", serial, "shell", "cat", StateFilePath)
	if err == nil && strings.Contains(stateRaw, "disabled_packages") {
		if json.Unmarshal([]byte(strings.TrimSpace(stateRaw)), &state) == nil {
			haveState = true
		}
	}

	// Without a state file there is no record of what avdslim changed, so there
	// is nothing to undo. Previously this fell back to enabling every package in
	// the bloat list — including ones the user had disabled deliberately, and
	// ones avdslim never touched — and then wrote hardcoded "stock" values over
	// their settings. Report the situation instead of guessing.
	if !haveState {
		fmt.Println("   ⚠️  No avdslim state file found on this device.")
		fmt.Println("      Nothing is known to have been changed by avdslim, so nothing was reverted.")
		fmt.Printf("      (State lives at %s and is written by `avdslim on`.)\n", StateFilePath)
		return 0, false, nil
	}

	restoredCount := 0
	for _, pkg := range state.DisabledPackages {
		res, _ := c.Exec("-s", serial, "shell", "pm", "enable", pkg)
		if strings.Contains(res, "enabled") || strings.Contains(res, "new state") {
			restoredCount++
			fmt.Printf("   ✓ Enabled: %s\n", pkg)
		}
	}

	// Replay recorded values rather than assuming stock defaults. The old code
	// forced animation scales to 1 and location_mode to 3 unconditionally, which
	// clobbered any non-default values the user had set (a 0.5 animation scale,
	// or location deliberately off).
	for key, prior := range state.PriorSettings {
		namespace, settingName, ok := strings.Cut(key, "/")
		if !ok {
			continue
		}
		if prior == unsetSettingValue {
			c.Exec("-s", serial, "shell", "settings", "delete", namespace, settingName)
			fmt.Printf("   ✓ Unset %s (was not set before)\n", key)
			continue
		}
		c.Exec("-s", serial, "shell", "settings", "put", namespace, settingName, prior)
		fmt.Printf("   ✓ Restored %s = %s\n", key, prior)
	}

	c.Exec("-s", serial, "shell", "rm", "-f", StateFilePath)

	return restoredCount, true, nil
}

func findAdb() string {
	return config.FindAdbExecutable()
}
