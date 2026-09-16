package bloat

import "fmt"

// StandardBloatCategories lists non-essential background packages safe to disable
// for everyday Android / Flutter development.
// Core OS functionality, WebView, Flutter runtime, networking, and Google Play
// Services core APIs (Auth, FCM push notifications) remain fully functional.
var StandardBloatCategories = map[string][]string{
	"Assistant & Search (~250-400 MB RAM)": {
		"com.google.android.googlequicksearchbox",
		"com.google.android.as",
		"com.google.android.as.oss",
	},
	"Media & Consumer Apps (~150-250 MB RAM)": {
		"com.google.android.apps.photos",
		"com.google.android.youtube",
		"com.google.android.videos",
		"com.google.android.music",
		"com.google.android.apps.youtube.music",
		"com.google.android.apps.maps",
		"com.google.android.gm",
		"com.android.gallery3d",
		"com.android.music",
	},
	"Telephony & Messaging Daemons (~100-150 MB RAM)": {
		"com.google.android.apps.messaging",
		"com.google.android.dialer",
		"com.google.android.contacts",
		"com.android.mms",
		"com.android.dialer",
		"com.android.contacts",
	},
	"Accessibility & Speech (~50-100 MB RAM)": {
		"com.google.android.tts",
		"com.google.android.marvin.talkback",
	},
	"Printing & Background Spoolers (~30-60 MB RAM)": {
		"com.android.printspooler",
		"com.android.bips",
		"com.google.android.printservice.recommendation",
	},
	"Telemetry, Feedback & Screensavers (~40-80 MB RAM)": {
		"com.google.android.feedback",
		"com.android.traceur",
		"com.android.dreams.basic",
		"com.android.dreams.phototable",
		"com.android.wallpaper.livepicker",
		"com.google.android.apps.wallpaper",
		"com.android.emergency",
		"com.android.calendar",
		"com.google.android.calendar",
		"com.android.deskclock",
		"com.google.android.deskclock",
		"com.android.calculator2",
		"com.google.android.calculator",
	},
}

// AggressiveBloatPackages includes packages that can be disabled when maximum
// resource conservation is desired.
var AggressiveBloatPackages = []string{
	"com.android.vending", // Google Play Store background auto-updater
	"com.android.chrome",  // Chrome background sync (Webview remains functional)
	"com.google.android.partnersetup",
	"com.google.android.setupwizard",
	"com.android.setupwizard",
}

func PrintProfiles() {
	fmt.Println("📋 AVD-SLIM Slimming Categories & Packages:")
	fmt.Println()

	for category, pkgs := range StandardBloatCategories {
		fmt.Printf("📦 %s:\n", category)
		for _, pkg := range pkgs {
			fmt.Printf("   • %s\n", pkg)
		}
		fmt.Println()
	}

	fmt.Println("🔥 Aggressive Mode Additional Packages (via `avdslim on --aggressive`):")
	for _, pkg := range AggressiveBloatPackages {
		fmt.Printf("   • %s\n", pkg)
	}
	fmt.Println()

	fmt.Println("🔒 NOT IN ANY DISABLE LIST ABOVE:")
	fmt.Println("   • Core Android OS & SystemUI")
	fmt.Println("   • Android System WebView & JavaScript runtimes")
	fmt.Println("   • Flutter / React Native / native APK execution")
	fmt.Println("   • TCP/UDP sockets, DNS & localhost port forwarding")
	fmt.Println("   • com.google.android.gms — Play Services core (Firebase Auth, FCM, Maps SDK)")
	fmt.Println("   • GPU hardware acceleration")
	fmt.Println()
	fmt.Println("⚠️  Caveats — this is a `pm disable-user` wrapper, not a guarantee:")
	fmt.Println("   • Aggressive mode disables com.android.vending (Play Store), which breaks")
	fmt.Println("     Play Billing and Play Integrity testing and can affect FCM registration.")
	fmt.Println("   • Aggressive mode disables the setup wizard packages.")
	fmt.Println("   • com.google.android.as (System Intelligence) is in the standard list; on")
	fmt.Println("     some Android versions this affects notification ranking and autofill.")
	fmt.Println("   • Only testing your own app can confirm your app still behaves.")
	fmt.Println("   • Use --keep=<package> to preserve anything you depend on.")
	fmt.Println()
}
