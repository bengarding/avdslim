package adb

import "testing"

// Animations must never be touched unless explicitly requested. Zeroing
// animator_duration_scale changes guest behaviour, not just memory use.
func TestAnimationSettings_AreNotInMemoryOrProvisioningSets(t *testing.T) {
	animationKeys := map[string]bool{
		"window_animation_scale":     true,
		"transition_animation_scale": true,
		"animator_duration_scale":    true,
	}

	for _, s := range append(append([]guestSetting{}, memorySettings...), provisioningSettings...) {
		if animationKeys[s.Key] {
			t.Errorf("%q is applied unconditionally; animations must be opt-in only", s.Key)
		}
	}

	if len(animationSettings) != 3 {
		t.Errorf("animationSettings has %d entries, want 3", len(animationSettings))
	}
	for _, s := range animationSettings {
		if !animationKeys[s.Key] {
			t.Errorf("unexpected key %q in animationSettings", s.Key)
		}
		if s.Value != "0" {
			t.Errorf("animationSettings[%q].Value = %q, want \"0\"", s.Key, s.Value)
		}
		if !s.Restore {
			t.Errorf("animationSettings[%q] must be restorable", s.Key)
		}
	}
}

// Reverting these would re-trigger the setup wizard, so they are intentionally
// excluded from restore.
func TestProvisioningSettings_AreNotRestored(t *testing.T) {
	for _, s := range provisioningSettings {
		if s.Restore {
			t.Errorf("provisioningSettings[%q].Restore = true; reverting it re-triggers setup", s.Key)
		}
	}
}

func TestMemorySettings_AreRestorable(t *testing.T) {
	if len(memorySettings) == 0 {
		t.Fatal("memorySettings is empty")
	}
	for _, s := range memorySettings {
		if !s.Restore {
			t.Errorf("memorySettings[%q].Restore = false; `off` could not put it back", s.Key)
		}
		if s.Namespace != "global" && s.Namespace != "secure" && s.Namespace != "system" {
			t.Errorf("memorySettings[%q].Namespace = %q, want global/secure/system", s.Key, s.Namespace)
		}
	}
}

func TestSettingKey_RoundTripsNamespaceAndKey(t *testing.T) {
	// Restore splits on "/" to rebuild the `settings put <ns> <key>` call, so the
	// key format and the split must agree.
	s := guestSetting{Namespace: "global", Key: "auto_sync", Value: "0", Restore: true}
	if got, want := settingKey(s), "global/auto_sync"; got != want {
		t.Errorf("settingKey = %q, want %q", got, want)
	}
}

func TestAllSettingKeys_AreUnique(t *testing.T) {
	// PriorSettings is keyed by settingKey, so a collision would lose a value.
	seen := map[string]string{}
	all := append(append(append([]guestSetting{}, memorySettings...), provisioningSettings...), animationSettings...)
	for _, s := range all {
		k := settingKey(s)
		if prev, dup := seen[k]; dup {
			t.Errorf("duplicate setting key %q (also from %q)", k, prev)
		}
		seen[k] = s.Key
	}
}

// A failed read must not be recorded as "null". Restore deletes any key recorded
// as unsetSettingValue, so conflating "could not read" with "was not set" would
// make `avdslim off` delete a setting that had a real value.
func TestApplySettings_DoesNotRecordUnreadableSettings(t *testing.T) {
	// /bin/false exits non-zero, so every getSetting call reports a failed read.
	c := &Client{adbPath: "/usr/bin/false"}

	prior := map[string]string{}
	c.applySettings("emulator-5554", memorySettings, prior)

	if len(prior) != 0 {
		t.Errorf("prior = %v, want empty: unreadable settings must not be recorded", prior)
	}
	for k, v := range prior {
		if v == unsetSettingValue {
			t.Errorf("%q recorded as %q after a failed read; restore would delete it", k, v)
		}
	}
}

// A genuinely unset setting is recorded as unsetSettingValue so restore knows to
// delete it rather than writing a fabricated default back.
func TestUnsetSettingValue_MatchesAndroidOutput(t *testing.T) {
	// `settings get` prints exactly this for a key with no value.
	if unsetSettingValue != "null" {
		t.Errorf("unsetSettingValue = %q, want \"null\" to match `settings get` output", unsetSettingValue)
	}
}

func TestGetSetting_ReportsFailureDistinctlyFromUnset(t *testing.T) {
	failing := &Client{adbPath: "/usr/bin/false"}
	if _, ok := failing.getSetting("emulator-5554", "global", "auto_sync"); ok {
		t.Error("getSetting reported ok=true when the adb call failed")
	}

	// Empty output with a zero exit is the "unset" case.
	empty := &Client{adbPath: "/usr/bin/true"}
	v, ok := empty.getSetting("emulator-5554", "global", "auto_sync")
	if !ok {
		t.Error("getSetting reported ok=false for a successful call")
	}
	if v != unsetSettingValue {
		t.Errorf("getSetting = %q for empty output, want %q", v, unsetSettingValue)
	}
}
