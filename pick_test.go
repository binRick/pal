package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// scriptedTTY plays back what a terminal would send: each element is one
// read; "" is a read that timed out with nothing typed.
type scriptedTTY struct{ chunks []string }

func (s *scriptedTTY) Read(p []byte) (int, error) {
	if len(s.chunks) == 0 {
		return 0, errors.New("script ran out: the picker kept reading after it should have finished")
	}
	c := s.chunks[0]
	s.chunks = s.chunks[1:]
	if c == "" {
		return 0, io.EOF
	}
	return copy(p, c), nil
}

const (
	keyUp, keyDown, keyEnter, keyEsc = "\x1b[A", "\x1b[B", "\r", "\x1b"
	// What a terminal answers when it does not support color queries:
	// only the device-attributes reply that ends the probe.
	noColors = "\x1b[?62;c"
)

// runPick drives the picker with a script and returns exit code and screen output.
func runPick(t *testing.T, h *harness, args []string, script ...string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := h.app.pick(args, &scriptedTTY{script}, &out, 24, 100, func() {})
	// Drop the opening color probe (it ends with the device-attributes
	// request) so the tests see only what was drawn and applied.
	screen := out.String()
	if i := strings.Index(screen, "\x1b[c"); i >= 0 {
		screen = screen[i+len("\x1b[c"):]
	}
	return code, screen
}

// lastBackground returns the last OSC 11 (background) value sent.
func lastBackground(out string) string {
	i := strings.LastIndex(out, "\x1b]11;")
	if i < 0 {
		return ""
	}
	rest := out[i+len("\x1b]11;"):]
	return rest[:strings.Index(rest, "\x1b\\")]
}

func TestPickTypeToFilterThenEnterKeepsHighlightedPalette(t *testing.T) {
	h, _ := saveHarness(t, "/bin/zsh")
	// "nord" matches base16-nord then nord; one step down selects nord.
	code, out := runPick(t, h, nil, noColors, "nord", keyDown, keyEnter)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr.String())
	}
	if !strings.Contains(out, nordOSC) {
		t.Error("nord was never sent to the terminal")
	}
	if got := lastBackground(out); got != "#2E3440" {
		t.Errorf("terminal left on background %s, want nord's #2E3440", got)
	}
	if !strings.Contains(h.stderr.String(), "Applied: nord\n") {
		t.Errorf("stderr should name the kept palette, got %q", h.stderr.String())
	}
	if h.app.current() != "nord" {
		t.Errorf("current palette recorded as %q, want nord (so pal save works next)", h.app.current())
	}
	if !strings.HasSuffix(out, "\x1b[?25h") {
		t.Error("cursor was left hidden")
	}
}

func TestPickAppliesEachPaletteAsTheSelectionMoves(t *testing.T) {
	h, _ := saveHarness(t, "/bin/zsh")
	_, out := runPick(t, h, []string{"selenized"}, noColors, keyDown, keyDown, keyUp, keyEnter)
	// selenized-black 181818, -dark 103C48, -light FBF3DB, -white FFFFFF
	var seen []string
	for _, part := range strings.Split(out, "\x1b]11;")[1:] {
		seen = append(seen, part[:7])
	}
	if strings.Join(seen, " ") != "#103C48 #FBF3DB #103C48" {
		t.Errorf("backgrounds sent while moving: %v", seen)
	}
}

func TestPickSelectionStopsAtTheEnds(t *testing.T) {
	h, _ := saveHarness(t, "/bin/zsh")
	runPick(t, h, []string{"selenized"}, noColors, keyUp, keyUp, keyEnter)
	if h.app.current() != "selenized-black" {
		t.Errorf("up from the first row landed on %q", h.app.current())
	}
	h, _ = saveHarness(t, "/bin/zsh")
	runPick(t, h, []string{"selenized"}, noColors, keyDown, keyDown, keyDown, keyDown, keyDown, keyDown, keyEnter)
	if h.app.current() != "selenized-white" {
		t.Errorf("down past the last row landed on %q", h.app.current())
	}
}

func TestPickHonoursFilterArguments(t *testing.T) {
	h, _ := saveHarness(t, "/bin/zsh")
	runPick(t, h, []string{"selenized", "--light"}, noColors, keyEnter)
	if h.app.current() != "selenized-light" {
		t.Errorf("first row of `pick selenized --light` was %q", h.app.current())
	}
}

func TestPickBackspaceWidensTheFilterAgain(t *testing.T) {
	h, _ := saveHarness(t, "/bin/zsh")
	// "nordx" matches nothing; deleting the x brings base16-nord back.
	runPick(t, h, nil, noColors, "nordx", "\x7f", keyEnter)
	if h.app.current() != "base16-nord" {
		t.Errorf("after backspace the selection was %q", h.app.current())
	}
}

func TestPickEnterWithNoMatchKeepsWaiting(t *testing.T) {
	h, _ := saveHarness(t, "/bin/zsh")
	code, _ := runPick(t, h, nil, noColors, "zzzzqq", keyEnter, keyEsc, "")
	if code == 0 {
		t.Error("exit 0 although nothing was picked")
	}
	if h.app.current() != "" {
		t.Errorf("Enter on an empty list kept %q", h.app.current())
	}
}

func TestPickEscRestoresTheColorsTheTerminalReported(t *testing.T) {
	h, _ := saveHarness(t, "/bin/zsh")
	replies := "\x1b]4;0;rgb:1111/2222/3333\x1b\\" + "\x1b]4;1;rgb:aa/bb/cc\x07" +
		"\x1b]10;rgb:dddd/eeee/ffff\x1b\\" + "\x1b]11;rgb:0101/0202/0303\x1b\\" + "\x1b]12;rgb:4444/5555/6666\x1b\\" + noColors
	code, out := runPick(t, h, nil, replies, "nord", keyEsc, "")
	if code == 0 {
		t.Error("exit 0 after cancelling")
	}
	for _, want := range []string{"\x1b]4;0;#112233\x1b\\", "\x1b]4;1;#AABBCC\x1b\\", "\x1b]10;#DDEEFF\x1b\\", "\x1b]12;#445566\x1b\\"} {
		if !strings.Contains(out, want) {
			t.Errorf("restore is missing %q", want)
		}
	}
	if got := lastBackground(out); got != "#010203" {
		t.Errorf("terminal left on background %s, want the original #010203", got)
	}
	if h.app.current() != "" {
		t.Errorf("a cancelled pick recorded %q as current", h.app.current())
	}
}

func TestPickEscFallsBackToTheRememberedPalette(t *testing.T) {
	h, _ := saveHarness(t, "/bin/zsh")
	h.app.run([]string{"set", "gruvbox-dark"})
	_, out := runPick(t, h, nil, noColors, "nord", keyEsc, "")
	if got := lastBackground(out); got != "#282828" {
		t.Errorf("terminal left on background %s, want gruvbox-dark's #282828", got)
	}
	if h.app.current() != "gruvbox-dark" {
		t.Errorf("current palette became %q", h.app.current())
	}
}

func TestPickEscWithNothingKnownResetsTheTerminal(t *testing.T) {
	h, _ := saveHarness(t, "/bin/zsh")
	_, out := runPick(t, h, nil, "", "", "", "nord", "\x03")
	if !strings.HasSuffix(strings.TrimSuffix(out, "\x1b[?25h"), "\x1b]104\x1b\\\x1b]110\x1b\\\x1b]111\x1b\\\x1b]112\x1b\\") &&
		!strings.Contains(out, "\x1b]104\x1b\\\x1b]110\x1b\\\x1b]111\x1b\\\x1b]112\x1b\\") {
		t.Errorf("no reset sequences after cancelling with nothing to restore")
	}
}

func TestPickEscWithoutMovingSendsNothing(t *testing.T) {
	h, _ := saveHarness(t, "/bin/zsh")
	_, out := runPick(t, h, nil, noColors, keyEsc, "")
	if strings.Contains(out, "\x1b]4;") || strings.Contains(out, "\x1b]104") {
		t.Error("colors were touched although nothing was previewed")
	}
}

func TestPickInsideTmuxWrapsAndSkipsTheColorProbe(t *testing.T) {
	h, _ := saveHarness(t, "/bin/zsh")
	h.app.tmux = true
	_, out := runPick(t, h, nil, "nord", keyDown, keyEnter)
	if strings.Contains(out, "\x1b]11;?") {
		t.Error("probed colors inside tmux, whose answers do not describe the outer terminal")
	}
	if !strings.Contains(out, "\x1bPtmux;\x1b\x1b]11;#2E3440\x1b\x1b\\\x1b\\") {
		t.Error("nord's background was not sent through tmux passthrough")
	}
}

func TestPickListShowsNamesAndTheirOwnColors(t *testing.T) {
	h, _ := saveHarness(t, "/bin/zsh")
	_, out := runPick(t, h, []string{"selenized"}, noColors, keyEsc, "")
	for _, want := range []string{"selenized-black", "selenized-white", "48;2;255;255;255", "4/4"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker screen is missing %q", want)
		}
	}
}
