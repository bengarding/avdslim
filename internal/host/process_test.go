package host

import "strings"

import "testing"

func TestHasPortArg_MatchesExplicitPortFlags(t *testing.T) {
	cases := []struct {
		name    string
		cmdline string
		port    string
		want    bool
	}{
		{"port flag", "123 qemu-system-aarch64 -avd Pixel -port 5554", "5554", true},
		{"ports pair first", "123 qemu-system-aarch64 -ports 5554,5555", "5554", true},
		{"ports pair second", "123 qemu-system-aarch64 -ports 5554,5555", "5555", true},
		// The old implementation accepted the port appearing anywhere on the line.
		{"port only in a path", "123 qemu-system-aarch64 -avd /home/u/5554/x -port 5556", "5554", false},
		{"port only as the pid", "5554 qemu-system-aarch64 -port 5556", "5554", false},
		{"different port", "123 qemu-system-aarch64 -port 5556", "5554", false},
		{"no port flag at all", "123 qemu-system-aarch64 -avd Pixel", "5554", false},
		{"empty port", "123 qemu-system-aarch64 -port 5554", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hasPortArg(strings.Fields(tc.cmdline), tc.port)
			if got != tc.want {
				t.Errorf("hasPortArg(%q, %q) = %v, want %v", tc.cmdline, tc.port, got, tc.want)
			}
		})
	}
}
