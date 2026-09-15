package cli

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The artwork is a design asset: it is reproduced verbatim, never reflowed.
// These lines are typed out again here on purpose, so that an accidental
// reindentation of the constants fails the build instead of shipping.
const arkVerbatim = `            ________
           /        \
          /__________\
          |  ______  |
          |  | () |  |
    ______|__|____|__|______
    \                      /
     \____________________/
   ~~~~~~~~~~~~~~~~~~~~~~~~~~`

func TestBannerReproducesTheArkVerbatim(t *testing.T) {
	for _, unicode := range []bool{true, false} {
		got := stripANSI(Banner(BannerOpts{Version: "1.2.3", Unicode: unicode, Color: false}))
		for _, line := range strings.Split(arkVerbatim, "\n") {
			if !strings.Contains(got, line) {
				t.Fatalf("unicode=%v: banner lost an ark line.\nwant line: %q\ngot:\n%s", unicode, line, got)
			}
		}
	}
}

func TestBannerStaysWithin32Columns(t *testing.T) {
	// The asset is specified to fit terminals of 40 columns.
	for name, art := range map[string]string{
		"banner":       Banner(BannerOpts{Version: "1.2.3", Unicode: true}),
		"banner-ascii": Banner(BannerOpts{Version: "1.2.3", Unicode: false}),
		"mark":         Mark(BannerOpts{Unicode: true}),
		"mark-ascii":   Mark(BannerOpts{Unicode: false}),
		"ark":          Ark(BannerOpts{}),
	} {
		for _, line := range strings.Split(stripANSI(art), "\n") {
			if w := utf8.RuneCountInString(line); w > 32 {
				t.Errorf("%s: line is %d columns, want <= 32: %q", name, w, line)
			}
		}
	}
}

func TestASCIIBannerIsPureASCII(t *testing.T) {
	got := stripANSI(Banner(BannerOpts{Version: "1.2.3", Unicode: false}))
	for i, r := range got {
		if r > 127 {
			t.Fatalf("byte %d is %q; the fallback banner must survive a non-UTF-8 terminal", i, r)
		}
	}
}

func TestUnicodeBannerUsesTheBlockWordmark(t *testing.T) {
	got := stripANSI(Banner(BannerOpts{Version: "1.2.3", Unicode: true}))
	if !strings.Contains(got, "▄▀▀▀█") {
		t.Fatalf("expected the block-letter wordmark, got:\n%s", got)
	}
}

func TestBannerCarriesVersionAndTagline(t *testing.T) {
	got := stripANSI(Banner(BannerOpts{Version: "1.2.3", Unicode: true}))
	if !strings.Contains(got, "1.2.3") {
		t.Errorf("banner omits the version:\n%s", got)
	}
	if !strings.Contains(got, Tagline) {
		t.Errorf("banner omits the tagline:\n%s", got)
	}
}

// The compact variant sets its text beside the drawing rather than under it,
// so the 32-column rule of the stacked banner does not apply to it. It still
// has to fit a terminal without wrapping.
func TestCompactBannerFitsANarrowTerminal(t *testing.T) {
	for _, line := range strings.Split(stripANSI(CompactBanner("1.2.3")), "\n") {
		if w := utf8.RuneCountInString(line); w > 60 {
			t.Errorf("compact banner line is %d columns, want <= 60: %q", w, line)
		}
	}
}

func TestCompactBannerFitsAHelpHeader(t *testing.T) {
	got := stripANSI(CompactBanner("1.2.3"))
	if !strings.Contains(got, "arca 1.2.3") {
		t.Errorf("compact banner should name the version once:\n%s", got)
	}
	if !strings.Contains(got, Tagline) {
		t.Errorf("compact banner omits the tagline:\n%s", got)
	}
	if n := strings.Count(got, "\n"); n > 4 {
		t.Errorf("compact banner is %d lines; it heads --help and must stay small:\n%s", n+1, got)
	}
}

func TestMarkFollowsTheLocale(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_CTYPE", "")

	t.Setenv("LANG", "C")
	for _, r := range stripANSI(Mark(AutoBannerOpts(""))) {
		if r > 127 {
			t.Fatalf("the mark must degrade to ASCII when the locale is not UTF-8:\n%s", Mark(AutoBannerOpts("")))
		}
	}

	t.Setenv("LANG", "en_US.UTF-8")
	if !strings.Contains(stripANSI(Mark(AutoBannerOpts(""))), "▄▀▀▀█") {
		t.Error("a UTF-8 locale should get the block lettering")
	}
}

// The mark is the artwork alone; the version and tagline belong to the banner.
func TestMarkCarriesNoText(t *testing.T) {
	got := stripANSI(Mark(BannerOpts{Unicode: true}))
	if strings.Contains(got, Tagline) {
		t.Errorf("the mark must not carry the tagline:\n%s", got)
	}
}

// The ark is the drawing without the lettering, for a screen with less room.
func TestArkIsTheDrawingAlone(t *testing.T) {
	got := stripANSI(Ark(BannerOpts{}))
	if !strings.Contains(got, "____________________") {
		t.Errorf("the ark should be the drawing:\n%s", got)
	}
	if strings.Contains(got, "▄▀▀▀█") || strings.Contains(got, `\__,_|_|`) {
		t.Errorf("the ark must carry no lettering:\n%s", got)
	}
}

func TestUnicodeOKReadsTheLocale(t *testing.T) {
	cases := []struct {
		name                       string
		term, lcAll, lcCtype, lang string
		want                       bool
	}{
		{name: "utf8 lang", lang: "pt_BR.UTF-8", want: true},
		{name: "utf8 lowercase", lang: "en_US.utf8", want: true},
		{name: "latin1", lang: "en_US.ISO-8859-1", want: false},
		{name: "posix", lang: "C", want: false},
		{name: "unset", want: false},
		{name: "lc_all wins", lcAll: "C", lang: "en_US.UTF-8", want: false},
		{name: "lc_ctype beats lang", lcCtype: "en_US.UTF-8", lang: "C", want: true},
		{name: "dumb terminal", term: "dumb", lang: "en_US.UTF-8", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TERM", tc.term)
			t.Setenv("LC_ALL", tc.lcAll)
			t.Setenv("LC_CTYPE", tc.lcCtype)
			t.Setenv("LANG", tc.lang)
			if got := UnicodeOK(); got != tc.want {
				t.Errorf("UnicodeOK() = %v, want %v", got, tc.want)
			}
		})
	}
}

// stripANSI removes styling so the tests assert on the artwork, not on colors.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func TestVersionTextIsPlainWhenNotATerminal(t *testing.T) {
	got := VersionText("1.2.3", false)
	if got != "arca version 1.2.3\n" {
		t.Fatalf("piped --version must stay machine-readable, got %q", got)
	}
}

func TestVersionTextShowsTheMarkOnATerminal(t *testing.T) {
	got := stripANSI(VersionText("1.2.3", true))
	if !strings.Contains(got, "____________________") {
		t.Errorf("--version on a terminal should show the ark:\n%s", got)
	}
	if !strings.Contains(got, "1.2.3") {
		t.Errorf("--version must still report the version:\n%s", got)
	}
}

func TestHelpHeaderCarriesTheMarkOnlyOnATerminal(t *testing.T) {
	restore := fancyOutput
	t.Cleanup(func() { fancyOutput = restore })

	fancyOutput = func() bool { return true }
	if long := stripANSI(NewRootCommand(nil).Long); !strings.Contains(long, "__|_o_|__") {
		t.Errorf("--help on a terminal should be headed by the compact mark:\n%s", long)
	}

	fancyOutput = func() bool { return false }
	if long := NewRootCommand(nil).Long; strings.Contains(long, "__|_o_|__") {
		t.Errorf("piped --help must stay plain:\n%s", long)
	}
}
