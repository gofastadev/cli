package commands

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Gofasta brand palette (shared with the website).
//
//	Primary:   Go Cyan  #00ADD8 → rgb(0, 173, 216)
//	Accent:    Dark Navy #00283A → rgb(0, 40, 58) — used for shading if we ever add it
//
// When the terminal doesn't report truecolor support we fall back to the
// closest 256-color ANSI slot (xterm-256 color 38, DeepSkyBlue2). Every
// terminal emulator made in the last decade supports 256 colors, so we
// don't bother with a 16-color fallback.
const (
	ansiReset         = "\x1b[0m"
	ansiCyanTrueColor = "\x1b[38;2;0;173;216m" // 24-bit #00ADD8
	ansiCyan256       = "\x1b[38;5;38m"        // xterm-256 "DeepSkyBlue2"
)

// banner is the figlet-standard rendering of "gofasta". Trailing whitespace
// has been stripped from every line so the color escape closes tightly.
const banner = `                __           _
    __ _  ___  / _| __ _ ___| |_ __ _
   / _` + "`" + ` |/ _ \| |_ / _` + "`" + ` / __| __/ _` + "`" + ` |
  | (_| | (_) |  _| (_| \__ \ || (_| |
   \__, |\___/|_|  \__,_|___/\__\__,_|
   |___/`

// tagline appears under the banner — branded color, plain ASCII.
const tagline = "Gofasta — Go backend toolkit"

// bannerStream is a package-level seam so tests can swap stderr.
var bannerStream io.Writer = os.Stderr

// bannerShown guards against printing the banner twice in a single process.
// It's set to true the first time the banner is emitted and stays sticky
// until resetBannerShown is called (used by tests). The typical sequence
// that would double-print without this flag: PersistentPreRun fires on the
// root command, then the Run function calls cmd.Help(), which routes through
// HelpFunc, which also tries to print the banner.
var bannerShown bool

// resetBannerShown is an internal helper exposed to tests so each test can
// start from a clean slate.
func resetBannerShown() { bannerShown = false }

// colorSupportFn decides whether the given writer should receive ANSI escapes.
// Overridable by tests.
var colorSupportFn = func(w io.Writer) (truecolor, any bool) {
	// The NO_COLOR convention (https://no-color.org) takes precedence over
	// everything else: if it's set to any non-empty value, no color at all.
	if v := os.Getenv("NO_COLOR"); v != "" {
		return false, false
	}
	// FORCE_COLOR lets users explicitly demand color even when we'd otherwise
	// suppress it (e.g. inside CI pipelines that allocate a pseudo-TTY).
	if v := os.Getenv("FORCE_COLOR"); v != "" && v != "0" {
		return true, true
	}
	// Only emit color when the target writer is an interactive terminal.
	if f, ok := w.(*os.File); ok {
		info, err := f.Stat()
		if err != nil {
			return false, false
		}
		if info.Mode()&os.ModeCharDevice == 0 {
			return false, false
		}
	} else {
		// Not an *os.File — likely a buffer under test. Keep it plain.
		return false, false
	}
	// Truecolor is advertised via $COLORTERM. Common values: "truecolor",
	// "24bit". Anything else means the terminal probably only does 256/16.
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	truecolor = ct == "truecolor" || ct == "24bit"
	return truecolor, true
}

// bannerSuppressedFn returns true when something in the environment tells us
// to hide the banner entirely (opt-out flags, CI tooling, etc).
var bannerSuppressedFn = func() bool {
	if v := os.Getenv("GOFASTA_NO_BANNER"); v != "" && v != "0" {
		return true
	}
	return false
}

// printBanner writes the branded banner to bannerStream, applying the
// best-available color format and falling back gracefully when color is
// unavailable. It guards against double-printing within a single process
// via the bannerShown flag — useful because PersistentPreRun and HelpFunc
// both try to print the banner and would otherwise double up on bare
// `gofasta` (PersistentPreRun fires, then Run calls cmd.Help() which
// routes through HelpFunc).
func printBanner() {
	if bannerShown {
		return
	}
	bannerShown = true
	writeBanner(bannerStream)
}

func writeBanner(w io.Writer) {
	if bannerSuppressedFn() {
		return
	}
	truecolor, anyColor := colorSupportFn(w)
	var colorOn, colorOff string
	switch {
	case truecolor:
		colorOn, colorOff = ansiCyanTrueColor, ansiReset
	case anyColor:
		// Fallback to 256-color palette, which is nearly universal today.
		colorOn, colorOff = ansiCyan256, ansiReset
	}
	_, _ = fmt.Fprintln(w, colorOn+banner+colorOff)
	_, _ = fmt.Fprintln(w, colorOn+"   "+tagline+colorOff)
	_, _ = fmt.Fprintln(w)
}
