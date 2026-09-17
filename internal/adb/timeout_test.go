package adb

import (
	"strings"
	"testing"
	"time"
)

// A wedged adb must not block the caller forever. Uses /bin/sleep in place of
// adb so the test is hermetic and needs no emulator.
func TestExecTimeout_KillsHungCommand(t *testing.T) {
	c := &Client{adbPath: "/bin/sleep"}

	start := time.Now()
	_, err := c.ExecTimeout(150*time.Millisecond, "30")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ExecTimeout returned nil error for a command that outlived its deadline")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, want it to mention the timeout", err)
	}
	// Generous ceiling: the point is that it returned at all rather than hanging.
	if elapsed > 10*time.Second {
		t.Errorf("ExecTimeout took %s; the process was not killed promptly", elapsed)
	}
}

func TestExecTimeout_ReturnsOutputWhenCommandFinishes(t *testing.T) {
	c := &Client{adbPath: "/bin/echo"}

	out, err := c.ExecTimeout(5*time.Second, "hello")
	if err != nil {
		t.Fatalf("ExecTimeout returned error: %v", err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Errorf("output = %q, want %q", strings.TrimSpace(out), "hello")
	}
}

func TestTimeoutFor_GivesBlockingSubcommandsLonger(t *testing.T) {
	if got := timeoutFor([]string{"devices"}); got != DefaultExecTimeout {
		t.Errorf("timeoutFor(devices) = %s, want %s", got, DefaultExecTimeout)
	}
	// wait-for-device is supposed to block on device state; a 30s cap would make
	// `avdslim start` fail on any emulator with a slow cold boot.
	if got := timeoutFor([]string{"wait-for-device"}); got != BlockingExecTimeout {
		t.Errorf("timeoutFor(wait-for-device) = %s, want %s", got, BlockingExecTimeout)
	}
	if got := timeoutFor([]string{"-s", "emulator-5554", "wait-for-device"}); got != BlockingExecTimeout {
		t.Errorf("timeoutFor with -s prefix = %s, want %s", got, BlockingExecTimeout)
	}
}

func TestExec_AppliesADefaultTimeout(t *testing.T) {
	// Guards against a future refactor dropping the bound entirely.
	if DefaultExecTimeout <= 0 {
		t.Fatal("DefaultExecTimeout must be positive")
	}
	if BlockingExecTimeout < DefaultExecTimeout {
		t.Error("BlockingExecTimeout should be at least DefaultExecTimeout")
	}
}
