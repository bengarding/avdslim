package host

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// probeTimeout bounds the host inspection helpers. lsof in particular can hang
// for a long time when a network mount is unresponsive, and these are only used
// to report memory figures — never worth blocking the CLI on.
const probeTimeout = 10 * time.Second

// RunProbe executes a short-lived host command with a timeout.
func RunProbe(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func FindHostPidForSerial(serial string) int {
	portStr := strings.TrimPrefix(serial, "emulator-")

	// 1. Try lsof on console port (e.g. 5554) - fastest and most precise
	if portStr != "" {
		if out, err := RunProbe("lsof", "-i", ":"+portStr, "-sTCP:LISTEN", "-t"); err == nil {
			fields := strings.Fields(string(out))
			if len(fields) > 0 {
				if pid, err := strconv.Atoi(fields[0]); err == nil && pid > 0 {
					return pid
				}
			}
		}
	}

	// 2. Fallback to process inspection
	out, err := RunProbe("ps", "-eo", "pid,command")
	if err != nil {
		return 0
	}

	myPid := os.Getpid()
	lines := strings.Split(string(out), "\n")
	var candidates []int

	for _, l := range lines {
		if strings.Contains(l, "avdslim") || strings.Contains(l, "crashpad") || strings.Contains(l, "netsimd") {
			continue
		}

		if strings.Contains(l, "qemu-system") {
			fields := strings.Fields(l)
			if len(fields) > 0 {
				if pid, err := strconv.Atoi(fields[0]); err == nil && pid != myPid {
					if strings.Contains(l, "-port "+portStr) || strings.Contains(l, portStr) {
						return pid
					}
					candidates = append(candidates, pid)
				}
			}
		}
	}

	if len(candidates) > 0 {
		return candidates[0]
	}
	return 0
}

func GetHostRssMb(pid int) int {
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "VmRSS:") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						if kb, err := strconv.Atoi(fields[1]); err == nil {
							return kb / 1024
						}
					}
				}
			}
		}
	}

	out, err := RunProbe("ps", "-p", strconv.Itoa(pid), "-o", "rss=")
	if err == nil {
		if kb, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
			return kb / 1024
		}
	}
	return 0
}

func GetHostFootprintMb(pid int) int {
	if out, err := RunProbe("footprint", "-p", strconv.Itoa(pid)); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "phys_footprint:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if mb, err := strconv.Atoi(fields[1]); err == nil && mb > 0 {
						return mb
					}
				}
			}
		}
	}
	return GetHostRssMb(pid)
}

func PrintGuestMeminfo(raw string) {
	fmt.Println("📱 GUEST (Android OS) Memory Breakdown:")
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		if strings.Contains(l, "Total RAM:") || strings.Contains(l, "Free RAM:") ||
			strings.Contains(l, "Used RAM:") || strings.Contains(l, "Lost RAM:") {
			fmt.Printf("   %s\n", strings.TrimSpace(l))
		}
	}
	fmt.Println()

	fmt.Println("🔝 Top Memory-Consuming Processes in Guest:")
	found := false
	count := 0
	for _, l := range lines {
		if strings.Contains(l, "Total PSS by process:") {
			found = true
			continue
		}
		if found {
			t := strings.TrimSpace(l)
			if t == "" || strings.HasPrefix(t, "Total PSS by category:") {
				break
			}
			if strings.Contains(t, "K:") && count < 10 {
				fmt.Printf("   %s\n", t)
				count++
			}
		}
	}
	fmt.Println()
}
