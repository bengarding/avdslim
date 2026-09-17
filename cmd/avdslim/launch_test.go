package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kdbhalala/avdslim/internal/adb"
)

// fakeAdb writes an executable stub that prints the given text for any argument
// list, and returns a Client pointed at it.
func fakeAdb(t *testing.T, output string) *adb.Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "adb")
	script := "#!/bin/sh\ncat <<'OUT'\n" + output + "\nOUT\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return adb.NewClientWithPath(path)
}

func TestWaitForEmulatorGone_ReturnsTrueWhenDeviceAbsent(t *testing.T) {
	client := fakeAdb(t, "List of devices attached\n")

	if !waitForEmulatorGone(client, "emulator-5554", 2*time.Second) {
		t.Error("waitForEmulatorGone = false when the device is not listed")
	}
}

// The old loops polled FindHostPidForSerial and broke as soon as it returned 0,
// which it also does when it merely cannot identify the process — printing
// "stopped cleanly" for an emulator that was still running.
func TestWaitForEmulatorGone_ReturnsFalseWhileDeviceStillListed(t *testing.T) {
	client := fakeAdb(t, "List of devices attached\nemulator-5554\tdevice")

	if waitForEmulatorGone(client, "emulator-5554", 2*time.Second) {
		t.Error("waitForEmulatorGone = true while the device is still listed")
	}
}

func TestFindSerialForAvd_EmptyWhenNothingRunning(t *testing.T) {
	client := fakeAdb(t, "List of devices attached\n")

	if got := findSerialForAvd(client, "Pixel_10_Pro"); got != "" {
		t.Errorf("findSerialForAvd = %q, want empty", got)
	}
}

func TestSlimFlagsFrom_ForwardsOnlySlimFlags(t *testing.T) {
	// handleLaunch must not leak its own flags into handleOn, but must forward
	// the slim ones — passing nil here previously made `start --aggressive`
	// silently apply the Standard preset.
	got := slimFlagsFrom([]string{
		"--ram=2048", "--aggressive", "--cold", "--keep=com.foo",
		"--headless", "--disable-animations", "--no-lowram",
	})

	want := map[string]bool{"--aggressive": true, "--keep=com.foo": true, "--disable-animations": true}
	if len(got) != len(want) {
		t.Fatalf("slimFlagsFrom = %v, want exactly %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("slimFlagsFrom forwarded unexpected flag %q", g)
		}
	}
}

func TestPositionalArgs_DropsFlags(t *testing.T) {
	got := positionalArgs([]string{"--aggressive", "emulator-5554", "-f", "--keep=x"})
	if len(got) != 1 || got[0] != "emulator-5554" {
		t.Errorf("positionalArgs = %v, want [emulator-5554]", got)
	}
}

// A memory increase must not be reported as "Reclaimed: 0MB". On a real run RSS
// went 359MB -> 1752MB and the clamped arithmetic still printed 0.
func TestDescribeDelta_ReportsIncreasesHonestly(t *testing.T) {
	cases := []struct {
		before, after int
		want          string
	}{
		{8516, 8538, "INCREASED by 22MB"},
		{359, 1752, "INCREASED by 1393MB"},
		{2500, 1500, "reclaimed 1000MB"},
		{1000, 1000, "no change"},
	}
	for _, tc := range cases {
		if got := describeDelta(tc.before, tc.after); got != tc.want {
			t.Errorf("describeDelta(%d, %d) = %q, want %q", tc.before, tc.after, got, tc.want)
		}
	}
}
