package main

import (
	"strings"
	"testing"
)

// nordPaleta is palettes/paleta/nord verbatim: fg, bg, cursor, colors 0-15.
const nordPaleta = `D8DEE9
2E3440
D8DEE9
3B4252
BF616A
A3BE8C
EBCB8B
81A1C1
B48EAD
88C0D0
E5E9F0
4C566A
BF616A
A3BE8C
EBCB8B
81A1C1
B48EAD
8FBCBB
ECEFF4
`

const vscodeKFC = `background=1e1e1e
foreground=d4d4d4
cursor=f44747
color00=1e1e1e
color01=f44747
color02=d7ba7d
color03=608b4e
color04=569cd6
color05=4ec9b0
color06=c586c0
color07=d4d4d4
color08=808080
color09=f44747
color10=d7ba7d
color11=608b4e
color12=569cd6
color13=4ec9b0
color14=c586c0
color15=d4d4d4
selection_background=1e1e1e
selection_foreground=d4d4d4`

func TestParsePaletaReadsFieldsInFileOrder(t *testing.T) {
	p, err := parsePaleta("nord", nordPaleta)
	if err != nil {
		t.Fatal(err)
	}
	if p.Foreground != "D8DEE9" || p.Background != "2E3440" || p.Cursor != "D8DEE9" {
		t.Errorf("fg/bg/cursor = %s/%s/%s", p.Foreground, p.Background, p.Cursor)
	}
	if p.Colors[0] != "3B4252" || p.Colors[1] != "BF616A" || p.Colors[15] != "ECEFF4" {
		t.Errorf("colors 0,1,15 = %s,%s,%s", p.Colors[0], p.Colors[1], p.Colors[15])
	}
}

func TestParsePaletaRejectsShortFile(t *testing.T) {
	if _, err := parsePaleta("short", "D8DEE9\n2E3440\n"); err == nil {
		t.Error("want an error for a 2-line paleta file, got none")
	}
}

func TestParsePaletaRejectsNonHexColor(t *testing.T) {
	bad := strings.Replace(nordPaleta, "BF616A", "BF616Z", 1)
	if _, err := parsePaleta("bad", bad); err == nil {
		t.Error("want an error for color BF616Z, got none")
	}
}

func TestParseKFCReadsKeyedColors(t *testing.T) {
	p, err := parseKFC("vscode", vscodeKFC)
	if err != nil {
		t.Fatal(err)
	}
	if p.Foreground != "D4D4D4" || p.Background != "1E1E1E" || p.Cursor != "F44747" {
		t.Errorf("fg/bg/cursor = %s/%s/%s", p.Foreground, p.Background, p.Cursor)
	}
	if p.Colors[0] != "1E1E1E" || p.Colors[9] != "F44747" || p.Colors[15] != "D4D4D4" {
		t.Errorf("colors 0,9,15 = %s,%s,%s", p.Colors[0], p.Colors[9], p.Colors[15])
	}
}

func TestParseKFCRejectsNonHexColor(t *testing.T) {
	bad := strings.Replace(vscodeKFC, "color04=569cd6", "color04=nothex", 1)
	if _, err := parseKFC("bad", bad); err == nil {
		t.Error("want an error for color04=nothex, got none")
	}
}

func TestParseKFCRejectsMissingColor(t *testing.T) {
	bad := strings.Replace(vscodeKFC, "color07=d4d4d4\n", "", 1)
	if _, err := parseKFC("bad", bad); err == nil {
		t.Error("want an error when color07 is absent, got none")
	}
}

func TestParseKFCRejectsMissingBackground(t *testing.T) {
	bad := strings.Replace(vscodeKFC, "background=1e1e1e\n", "", 1)
	if _, err := parseKFC("bad", bad); err == nil {
		t.Error("want an error when background is absent, got none")
	}
}

// A palette file that stops parsing must fail the build, not a user's
// terminal: every embedded palette has to load cleanly.
func TestEveryEmbeddedPaletteLoads(t *testing.T) {
	names := listPalettes()
	if len(names) < 400 {
		t.Fatalf("only %d palettes embedded; the palettes directory looks truncated", len(names))
	}
	for _, name := range names {
		if _, err := loadPalette(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestIsLightFollowsBackgroundBrightness(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"solarized-light", true},
		{"selenized-white", true},
		{"tempus_day", true}, // light, despite living in kfc/dark
		{"base16-greenscreen-light", true},
		{"solarized-dark", false},
		{"nord", false},
		{"dracula-dark", false},
	}
	for _, c := range cases {
		p, err := loadPalette(c.name)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.IsLight(); got != c.want {
			t.Errorf("%s: IsLight() = %v, want %v (background #%s)", c.name, got, c.want, p.Background)
		}
	}
}

func TestMatchingFiltersByTermsAndBrightness(t *testing.T) {
	cases := []struct {
		name   string
		filter filter
		want   []string
	}{
		{"single term", filter{terms: []string{"selenized"}},
			[]string{"selenized-black", "selenized-dark", "selenized-light", "selenized-white"}},
		{"terms are ANDed", filter{terms: []string{"selenized", "white"}},
			[]string{"selenized-white"}},
		{"case-insensitive", filter{terms: []string{"SELENIZED-W"}},
			[]string{"selenized-white"}},
		{"light only", filter{terms: []string{"selenized"}, light: true},
			[]string{"selenized-light", "selenized-white"}},
		{"dark only", filter{terms: []string{"selenized"}, dark: true},
			[]string{"selenized-black", "selenized-dark"}},
		{"no match", filter{terms: []string{"no-such-palette-xyz"}}, nil},
	}
	for _, c := range cases {
		got, err := matching(c.filter)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSuggestFindsNearbyNames(t *testing.T) {
	names := listPalettes()
	cases := []struct {
		typo string
		want string // must appear among the suggestions
	}{
		{"draculaa", "dracula-dark"},         // one extra letter
		{"gruvbx", "gruvbox-dark"},           // one missing letter
		{"dracula", "base16-dracula"},        // not a palette itself, but a substring of several
		{"solarised-dark", "solarized-dark"}, // one wrong letter in a full name
	}
	for _, c := range cases {
		got := suggest(c.typo, names, 6)
		found := false
		for _, g := range got {
			if g == c.want {
				found = true
			}
		}
		if !found {
			t.Errorf("suggest(%q) = %v, want it to include %q", c.typo, got, c.want)
		}
	}
}

func TestSuggestStaysQuietForUnrelatedInput(t *testing.T) {
	if got := suggest("qqqqzzzz", listPalettes(), 6); len(got) != 0 {
		t.Errorf("suggest(qqqqzzzz) = %v, want nothing", got)
	}
}

func TestSuggestRespectsLimit(t *testing.T) {
	if got := suggest("base16", listPalettes(), 3); len(got) != 3 {
		t.Errorf("suggest(base16, limit 3) returned %d names: %v", len(got), got)
	}
}
