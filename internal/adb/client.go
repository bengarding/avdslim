package adb

import (
	"bufio"
	"bytes"
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

// getSetting reads a guest setting, returning "null" when it is unset.
func (c *Client) getSetting(serial, namespace, key string) string {
	out, err := c.Exec("-s", serial, "shell", "settings", "get", namespace, key)
	if err != nil {
		return "null"
	}
	v := strings.TrimSpace(out)
	if v == "" {
		return "null"
	}
	return v
}

// applySettings records the pre-change value of each restorable setting, then
// writes the new value. Returns the recorded values for persistence.
func (c *Client) applySettings(serial string, settings []guestSetting, prior map[string]string) {
	for _, s := range settings {
		if s.Restore {
			if _, seen := prior[settingKey(s)]; !seen {
				prior[settingKey(s)] = c.getSetting(serial, s.Namespace, s.Key)
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
	cmd := exec.Command(c.adbPath, "-s", serial, "shell", "cat > "+StateFilePath)
	cmd.Stdin = bytes.NewReader(payload)
	return cmd.Run()
}

type Client struct {
	adbPath string
}

func NewClient() *Client {
	return &Client{
		adbPath: findAdb(),
	}
}

func (c *Client) Exec(args ...string) (string, error) {
	cmd := exec.Command(c.adbPath, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
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

			stateExists, _ := c.Exec("-s", serial, "shell", "ls", StateFilePath)
			isSlimmed := strings.Contains(stateExists, "avdslim_state.json")

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
	c.Exec("-s", serial, "shell", "am", "trim-memory", "--all", "COMPLETE")
	c.Exec("-s", serial, "shell", "su", "0", "sync")
	c.Exec("-s", serial, "shell", "su", "0", "echo 3 > /proc/sys/vm/drop_caches")

	return len(disabledList), nil
}

func (c *Client) Restore(serial string) (int, error) {
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
		return 0, nil
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
		if prior == "null" {
			c.Exec("-s", serial, "shell", "settings", "delete", namespace, settingName)
			fmt.Printf("   ✓ Unset %s (was not set before)\n", key)
			continue
		}
		c.Exec("-s", serial, "shell", "settings", "put", namespace, settingName, prior)
		fmt.Printf("   ✓ Restored %s = %s\n", key, prior)
	}

	c.Exec("-s", serial, "shell", "rm", "-f", StateFilePath)

	return restoredCount, nil
}

func findAdb() string {
	return config.FindAdbExecutable()
}
