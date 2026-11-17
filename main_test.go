package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// nordOSC is, written out by hand, exactly what applying nord must send:
// palette colors 0-15, then foreground, background and cursor.
const nordOSC = "\x1b]4;0;#3B4252\x1b\\" +
	"\x1b]4;1;#BF616A\x1b\\" +
	"\x1b]4;2;#A3BE8C\x1b\\" +
	"\x1b]4;3;#EBCB8B\x1b\\" +
	"\x1b]4;4;#81A1C1\x1b\\" +
	"\x1b]4;5;#B48EAD\x1b\\" +
	"\x1b]4;6;#88C0D0\x1b\\" +
	"\x1b]4;7;#E5E9F0\x1b\\" +
	"\x1b]4;8;#4C566A\x1b\\" +
	"\x1b]4;9;#BF616A\x1b\\" +
	"\x1b]4;10;#A3BE8C\x1b\\" +
	"\x1b]4;11;#EBCB8B\x1b\\" +
	"\x1b]4;12;#81A1C1\x1b\\" +
	"\x1b]4;13;#B48EAD\x1b\\" +
	"\x1b]4;14;#8FBCBB\x1b\\" +
	"\x1b]4;15;#ECEFF4\x1b\\" +
	"\x1b]10;#D8DEE9\x1b\\" +
	"\x1b]11;#2E3440\x1b\\" +
	"\x1b]12;#D8DEE9\x1b\\"

// unwrapTmux undoes DCS-tmux passthrough the way tmux does: strip each
// \ePtmux; ... \e\\ envelope and halve the doubled escapes inside it.
func unwrapTmux(t *testing.T, s string) string {
	t.Helper()
	var out strings.Builder
	for s != "" {
		const open = "\x1bPtmux;"
		if !strings.HasPrefix(s, open) {
			t.Fatalf("expected a tmux passthrough envelope at %q", s)
		}
		s = s[len(open):]
		// The envelope ends at the first ESC \ not preceded by another ESC.
		end := -1
		for i := 0; i+1 < len(s); i++ {
			if s[i] == 0x1b && s[i+1] == 0x1b {
				i++ // a doubled escape belongs to the payload
				continue
			}
			if s[i] == 0x1b && s[i+1] == '\\' {
				end = i
				break
			}
		}
		if end < 0 {
			t.Fatalf("unterminated tmux envelope in %q", s)
		}
		out.WriteString(strings.ReplaceAll(s[:end], "\x1b\x1b", "\x1b"))
		s = s[end+2:]
	}
	return out.String()
}

type harness struct {
	app                  *app
	env                  map[string]string
	parent               string // name of the invoking shell process
	stdout, stderr, term bytes.Buffer
	passthroughCalls     int
}

func newHarness() *harness {
	h := &harness{env: map[string]string{}}
	h.app = &app{
		getenv:      func(k string) string { return h.env[k] },
		parentShell: func() string { return h.parent },
		stdout:      &h.stdout,
		stderr:      &h.stderr,
		term:        &h.term,
		truecolor:   true,
		passthrough: func() { h.passthroughCalls++ },
	}
	return h
}

func TestSetSendsPaletteToTerminal(t *testing.T) {
	for _, args := range [][]string{{"set", "nord"}, {"apply", "nord"}, {"nord"}} {
		h := newHarness()
		if code := h.app.run(args); code != 0 {
			t.Fatalf("%v: exit %d, stderr %q", args, code, h.stderr.String())
		}
		if h.term.String() != nordOSC {
			t.Errorf("%v: terminal got %q", args, h.term.String())
		}
		if h.stdout.Len() != 0 {
			t.Errorf("%v: stdout should stay clean, got %q", args, h.stdout.String())
		}
		if h.passthroughCalls != 0 {
			t.Errorf("%v: tmux passthrough was touched outside tmux", args)
		}
	}
}

func TestSetInsideTmuxWrapsEverySequence(t *testing.T) {
	h := newHarness()
	h.app.tmux = true
	if code := h.app.run([]string{"set", "nord"}); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr.String())
	}
	got := h.term.String()
	const first = "\x1bPtmux;\x1b\x1b]4;0;#3B4252\x1b\x1b\\\x1b\\"
	if !strings.HasPrefix(got, first) {
		t.Errorf("first sequence not wrapped for tmux: %q", got[:min(len(got), 60)])
	}
	if unwrapTmux(t, got) != nordOSC {
		t.Errorf("unwrapped tmux output differs from the plain sequences: %q", got)
	}
	if h.passthroughCalls != 1 {
		t.Errorf("allow-passthrough enabled %d times, want once", h.passthroughCalls)
	}
}

func TestResetRestoresTerminalDefaults(t *testing.T) {
	h := newHarness()
	if code := h.app.run([]string{"reset"}); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr.String())
	}
	// OSC 104 = palette, 110 = foreground, 111 = background, 112 = cursor.
	const want = "\x1b]104\x1b\\\x1b]110\x1b\\\x1b]111\x1b\\\x1b]112\x1b\\"
	if h.term.String() != want {
		t.Errorf("terminal got %q, want %q", h.term.String(), want)
	}
}

func TestResetInsideTmuxIsWrapped(t *testing.T) {
	h := newHarness()
	h.app.tmux = true
	h.app.run([]string{"reset"})
	const want = "\x1b]104\x1b\\\x1b]110\x1b\\\x1b]111\x1b\\\x1b]112\x1b\\"
	if unwrapTmux(t, h.term.String()) != want {
		t.Errorf("terminal got %q", h.term.String())
	}
}

func TestSetReportsTerminalWriteFailure(t *testing.T) {
	h := newHarness()
	h.app.term = failingWriter{}
	if code := h.app.run([]string{"set", "nord"}); code == 0 {
		t.Error("exit 0 although the terminal write failed")
	}
	if !strings.Contains(h.stderr.String(), "terminal went away") {
		t.Errorf("stderr should carry the write error, got %q", h.stderr.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("terminal went away") }

// The swatch must show the palette that was asked for, in its own
// colors, and leave the terminal's palette alone.
func TestPreviewDrawsTheNamedPaletteWithoutApplyingIt(t *testing.T) {
	h := newHarness()
	if code := h.app.run([]string{"preview", "nord"}); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr.String())
	}
	out := h.stdout.String()
	for what, sgr := range map[string]string{
		"color 1 BF616A as a block":  "48;2;191;97;106",
		"color 14 8FBCBB as a block": "48;2;143;188;187",
		"background 2E3440":          "48;2;46;52;64",
		"foreground D8DEE9 as text":  "38;2;216;222;233",
		"color 4 81A1C1 as text":     "38;2;129;161;193",
	} {
		if !strings.Contains(out, sgr) {
			t.Errorf("preview is missing %s (%s)", what, sgr)
		}
	}
	if strings.Contains(out, "48;5;") || strings.Contains(out, "38;5;") {
		t.Error("preview used indexed colors, which show the terminal's current palette instead")
	}
	for _, text := range []string{"nord", "fg:#D8DEE9", "bg:#2E3440", "cursor:#D8DEE9", "dark"} {
		if !strings.Contains(out, text) {
			t.Errorf("preview is missing %q", text)
		}
	}
	if h.term.Len() != 0 {
		t.Errorf("preview changed the terminal palette: %q", h.term.String())
	}
}

func TestPreviewLabelsLightPalettes(t *testing.T) {
	h := newHarness()
	// tempus_day is a light palette whose name does not say so.
	h.app.run([]string{"preview", "tempus_day"})
	if !strings.Contains(h.stdout.String(), "light") {
		t.Errorf("preview of tempus_day does not say it is light: %q", h.stdout.String())
	}
}

func TestPreviewFallsBackTo256ColorsWithoutTruecolor(t *testing.T) {
	h := newHarness()
	h.app.truecolor = false
	h.app.run([]string{"preview", "nord"})
	out := h.stdout.String()
	if strings.Contains(out, ";2;") {
		t.Error("24-bit color used although the terminal did not announce truecolor")
	}
	// Background 2E3440 is closest to grey 237 in the xterm-256 palette.
	if !strings.Contains(out, "48;5;237") {
		t.Errorf("expected background as xterm color 237, got %q", out)
	}
}

func TestListPrintsEveryNameSorted(t *testing.T) {
	h := newHarness()
	if code := h.app.run([]string{"list"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(strings.TrimSuffix(h.stdout.String(), "\n"), "\n")
	if len(lines) < 400 {
		t.Fatalf("list printed %d names", len(lines))
	}
	if lines[0] != "3024" || lines[len(lines)-1] != "zenburn" {
		t.Errorf("first/last = %q/%q, want 3024/zenburn", lines[0], lines[len(lines)-1])
	}
	if strings.Contains(h.stdout.String(), "\x1b") {
		t.Error("plain list must stay free of escape codes so it can be piped")
	}
}

func TestListFilters(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"list", "gruvbox"}, "base16-gruvbox-hard\nbase16-gruvbox-medium\nbase16-gruvbox-pale\nbase16-gruvbox-soft\ngruvbox\ngruvbox-dark\ngruvbox-light\n"},
		{[]string{"list", "gruvbox", "--light"}, "gruvbox-light\n"},
		{[]string{"list", "--light", "gruvbox"}, "gruvbox-light\n"},
		{[]string{"ls", "--dark", "selenized"}, "selenized-black\nselenized-dark\n"},
		{[]string{"list", "base16", "gruvbox", "soft"}, "base16-gruvbox-soft\n"},
	}
	for _, c := range cases {
		h := newHarness()
		if code := h.app.run(c.args); code != 0 {
			t.Errorf("%v: exit %d, stderr %q", c.args, code, h.stderr.String())
		}
		if h.stdout.String() != c.want {
			t.Errorf("%v: got %q, want %q", c.args, h.stdout.String(), c.want)
		}
	}
}

func TestListWithNoMatchFails(t *testing.T) {
	h := newHarness()
	if code := h.app.run([]string{"list", "no-such-palette-xyz"}); code == 0 {
		t.Error("exit 0 for a search with no results; scripts could not tell")
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", h.stdout.String())
	}
}

func TestListRejectsContradictoryAndUnknownFlags(t *testing.T) {
	for _, args := range [][]string{
		{"list", "--dark", "--light"},
		{"list", "--bogus"},
		{"random", "--bogus"},
	} {
		h := newHarness()
		if code := h.app.run(args); code == 0 {
			t.Errorf("%v: exit 0, want failure", args)
		}
		if h.stdout.Len() != 0 || h.term.Len() != 0 {
			t.Errorf("%v: produced output despite bad flags", args)
		}
	}
}

func TestListSwatchesShowEachPalettesOwnColors(t *testing.T) {
	h := newHarness()
	if code := h.app.run([]string{"list", "-v", "selenized"}); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr.String())
	}
	lines := strings.Split(strings.TrimSuffix(h.stdout.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want 4 rows, got %d: %q", len(lines), h.stdout.String())
	}
	white := lines[3]
	if !strings.HasSuffix(white, "selenized-white") {
		t.Errorf("row should end with the palette name: %q", white)
	}
	// selenized-white: background FFFFFF, foreground 474747, color 1 D6000C.
	for what, sgr := range map[string]string{
		"background": "48;2;255;255;255",
		"foreground": "38;2;71;71;71",
		"color 1":    "48;2;214;0;12",
	} {
		if !strings.Contains(white, sgr) {
			t.Errorf("selenized-white row is missing its %s (%s): %q", what, sgr, white)
		}
	}
	if strings.Contains(lines[0], "48;2;255;255;255") {
		t.Errorf("selenized-black row carries selenized-white's background: %q", lines[0])
	}
}

func TestRandomHonoursFilter(t *testing.T) {
	light := map[string]bool{"selenized-light": true, "selenized-white": true}
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		h := newHarness()
		if code := h.app.run([]string{"random", "--light", "selenized"}); code != 0 {
			t.Fatalf("exit %d, stderr %q", code, h.stderr.String())
		}
		name := strings.TrimSpace(strings.TrimPrefix(h.stderr.String(), "Applied:"))
		if !light[name] {
			t.Fatalf("random --light selenized applied %q", name)
		}
		seen[name] = true
		if !strings.Contains(h.term.String(), "\x1b]11;#") {
			t.Fatalf("random did not send a palette to the terminal: %q", h.term.String())
		}
	}
	if len(seen) != 2 {
		t.Errorf("40 draws only ever produced %v", seen)
	}
}

func TestRandomWithNoMatchFails(t *testing.T) {
	h := newHarness()
	if code := h.app.run([]string{"random", "no-such-palette-xyz"}); code == 0 {
		t.Error("exit 0 although nothing matched")
	}
	if h.term.Len() != 0 {
		t.Errorf("terminal was written to: %q", h.term.String())
	}
}

func TestUnknownPaletteSuggestsNearMisses(t *testing.T) {
	for _, args := range [][]string{{"draculaa"}, {"set", "draculaa"}, {"preview", "draculaa"}, {"export", "draculaa"}} {
		h := newHarness()
		if code := h.app.run(args); code == 0 {
			t.Errorf("%v: exit 0 for a palette that does not exist", args)
		}
		if !strings.Contains(h.stderr.String(), "dracula-dark") {
			t.Errorf("%v: stderr should suggest dracula-dark, got %q", args, h.stderr.String())
		}
		if h.term.Len() != 0 || h.stdout.Len() != 0 {
			t.Errorf("%v: produced output for an unknown palette", args)
		}
	}
}

func TestMissingNameIsAnError(t *testing.T) {
	for _, cmd := range []string{"set", "preview", "export"} {
		h := newHarness()
		if code := h.app.run([]string{cmd}); code == 0 {
			t.Errorf("%s with no name: exit 0", cmd)
		}
	}
}

// runExported pastes `pal export nord` into a real shell, the way a
// .bashrc would run it, and returns what reaches the terminal.
func runExported(t *testing.T, tmuxEnv string) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on this system")
	}
	h := newHarness()
	if code := h.app.run([]string{"export", "nord"}); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr.String())
	}
	cmd := exec.Command(sh, "-c", h.stdout.String())
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	if tmuxEnv != "" {
		cmd.Env = append(cmd.Env, "TMUX="+tmuxEnv)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("exported snippet failed in sh: %v\nsnippet: %s", err, h.stdout.String())
	}
	return string(out)
}

func TestExportedSnippetAppliesPaletteInPlainShell(t *testing.T) {
	if got := runExported(t, ""); got != nordOSC {
		t.Errorf("shell emitted %q", got)
	}
}

func TestExportedSnippetWrapsForTmux(t *testing.T) {
	// A socket path that does not exist: the snippet's own tmux call
	// fails quietly instead of reconfiguring a real server.
	got := runExported(t, "/nonexistent/pal-test-socket,1,0")
	if !strings.HasPrefix(got, "\x1bPtmux;\x1b\x1b]4;0;#3B4252\x1b\x1b\\\x1b\\") {
		t.Errorf("snippet did not wrap for tmux: %q", got[:min(len(got), 60)])
	}
	if unwrapTmux(t, got) != nordOSC {
		t.Errorf("unwrapped snippet output differs from the plain sequences: %q", got)
	}
}

func TestExportDoesNotTouchTheTerminal(t *testing.T) {
	h := newHarness()
	h.app.run([]string{"export", "nord"})
	if h.term.Len() != 0 {
		t.Errorf("export wrote to the terminal: %q", h.term.String())
	}
}

func TestXterm256PicksNearestColor(t *testing.T) {
	cases := []struct {
		hex  string
		want int
	}{
		{"000000", 16},  // cube black
		{"FFFFFF", 231}, // cube white
		{"FF0000", 196}, // cube red
		{"5F87AF", 67},  // exact cube entry (95,135,175)
		{"808080", 244}, // exact grey-ramp entry (128)
		{"2E3440", 237}, // nord background: nearer the grey ramp than the cube
	}
	for _, c := range cases {
		if got := xterm256(c.hex); got != c.want {
			t.Errorf("xterm256(%s) = %d, want %d", c.hex, got, c.want)
		}
	}
}

// Sequences must reach the terminal even when stdout is redirected, as
// in `pal random > /tmp/log` or a script that captures stdout.
func TestTerminalPrefersStdoutThenTTY(t *testing.T) {
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skip(err)
	}
	defer devnull.Close()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	fakeTTY, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatal(err)
	}
	defer fakeTTY.Close()

	opened := 0
	openOK := func() (*os.File, error) { opened++; return fakeTTY, nil }
	openFail := func() (*os.File, error) { opened++; return nil, errors.New("no controlling terminal") }

	// A character device on stdout is used directly.
	if w, _ := terminal(devnull, openOK); w != devnull || opened != 0 {
		t.Errorf("stdout is a device: got %v after %d tty opens, want stdout and 0", w.Name(), opened)
	}
	// A pipe on stdout sends the sequences to the controlling terminal.
	if w, _ := terminal(pw, openOK); w != fakeTTY {
		t.Errorf("stdout is a pipe: got %v, want the tty", w.Name())
	}
	// With no controlling terminal (ssh host pal nord), stdout is all there is.
	if w, _ := terminal(pw, openFail); w != pw {
		t.Errorf("no tty available: got %v, want stdout", w.Name())
	}
}

// saveHarness is a harness with a throwaway home directory.
func saveHarness(t *testing.T, shell string) (*harness, string) {
	t.Helper()
	h := newHarness()
	home := t.TempDir()
	h.env["HOME"] = home
	h.env["SHELL"] = shell
	return h, home
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// runRC sources an rc file the way a new shell would and returns what
// it prints.
func runRC(t *testing.T, path string) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on this system")
	}
	cmd := exec.Command(sh, path)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rc file failed in sh: %v", err)
	}
	return string(out)
}

func TestSaveWritesPaletteIntoShellConfigAndAppliesIt(t *testing.T) {
	h, home := saveHarness(t, "/bin/zsh")
	if code := h.app.run([]string{"save", "nord"}); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr.String())
	}
	rc := home + "/.zshrc"
	if got := runRC(t, rc); got != nordOSC {
		t.Errorf("a new shell would emit %q", got)
	}
	if h.term.String() != nordOSC {
		t.Errorf("save did not apply the palette now: %q", h.term.String())
	}
	if !strings.Contains(h.stderr.String(), rc) {
		t.Errorf("save should say which file it wrote, got %q", h.stderr.String())
	}
}

func TestSaveReplacesEarlierSaveAndKeepsTheRestOfTheFile(t *testing.T) {
	h, home := saveHarness(t, "/bin/bash")
	rc := home + "/.bashrc"
	if err := os.WriteFile(rc, []byte("export EDITOR=vim\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.app.run([]string{"save", "dracula-dark"})
	if err := os.WriteFile(rc, []byte(readFile(t, rc)+"alias ll='ls -l'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.app.run([]string{"save", "nord"}); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr.String())
	}
	got := readFile(t, rc)
	if !strings.HasPrefix(got, "export EDITOR=vim\n") || !strings.HasSuffix(got, "alias ll='ls -l'\n") {
		t.Errorf("lines around the pal block were disturbed:\n%s", got)
	}
	if strings.Contains(got, "44475A") {
		t.Error("the earlier dracula-dark save is still in the file")
	}
	if out := runRC(t, rc); out != nordOSC {
		t.Errorf("a new shell would emit %q", out)
	}
	if fi, _ := os.Stat(rc); fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode changed to %v", fi.Mode().Perm())
	}
}

func TestSavePicksConfigOfTheInvokingShell(t *testing.T) {
	cases := []struct {
		name, shellEnv, parent, zdotdir, want string
	}{
		{"login shell from $SHELL", "/bin/bash", "", "", ".bashrc"},
		{"invoking shell beats $SHELL", "/bin/zsh", "-bash", "", ".bashrc"},
		{"non-shell parent falls back to $SHELL", "/bin/zsh", "make", "", ".zshrc"},
		{"zsh honours ZDOTDIR", "/bin/zsh", "", "zdot", "zdot/.zshrc"},
	}
	for _, c := range cases {
		h, home := saveHarness(t, c.shellEnv)
		h.parent = c.parent
		if c.zdotdir != "" {
			h.env["ZDOTDIR"] = home + "/" + c.zdotdir
			os.Mkdir(home+"/"+c.zdotdir, 0o755)
		}
		if code := h.app.run([]string{"save", "nord"}); code != 0 {
			t.Errorf("%s: exit %d, stderr %q", c.name, code, h.stderr.String())
			continue
		}
		if _, err := os.Stat(home + "/" + c.want); err != nil {
			t.Errorf("%s: expected %s to be written: %v", c.name, c.want, err)
		}
	}
}

func TestSaveRefusesShellsItCannotWriteFor(t *testing.T) {
	h, home := saveHarness(t, "/usr/bin/fish")
	if code := h.app.run([]string{"save", "nord"}); code == 0 {
		t.Error("exit 0 for fish, whose config pal cannot write")
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("files were created anyway: %v", entries)
	}
	if h.term.Len() != 0 {
		t.Error("palette applied although save failed")
	}
}

func TestSaveUnknownPaletteLeavesConfigAlone(t *testing.T) {
	h, home := saveHarness(t, "/bin/zsh")
	if code := h.app.run([]string{"save", "draculaa"}); code == 0 {
		t.Error("exit 0 for an unknown palette")
	}
	if _, err := os.Stat(home + "/.zshrc"); err == nil {
		t.Error(".zshrc was created for an unknown palette")
	}
}

func TestSaveWithoutNameSavesTheLastAppliedPalette(t *testing.T) {
	for _, apply := range [][]string{{"set", "nord"}, {"nord"}, {"random", "nord", "--dark", "ord"}} {
		h, home := saveHarness(t, "/bin/zsh")
		h.app.run(apply)
		if apply[0] == "random" && !strings.Contains(h.stderr.String(), "Applied: nord\n") {
			continue // the draw picked base16-nord; nothing to compare against
		}
		h.term.Reset()
		if code := h.app.run([]string{"save"}); code != 0 {
			t.Fatalf("after %v: exit %d, stderr %q", apply, code, h.stderr.String())
		}
		if got := runRC(t, home+"/.zshrc"); got != nordOSC {
			t.Errorf("after %v: a new shell would emit %q", apply, got)
		}
	}
}

func TestSaveWithoutNameNeedsACurrentPalette(t *testing.T) {
	h, home := saveHarness(t, "/bin/zsh")
	if code := h.app.run([]string{"save"}); code == 0 {
		t.Error("exit 0 although no palette was ever applied")
	}
	if _, err := os.Stat(home + "/.zshrc"); err == nil {
		t.Error(".zshrc was written with nothing to save")
	}
}

func TestResetForgetsTheCurrentPalette(t *testing.T) {
	h, _ := saveHarness(t, "/bin/zsh")
	h.app.run([]string{"set", "nord"})
	h.app.run([]string{"reset"})
	if code := h.app.run([]string{"save"}); code == 0 {
		t.Error("save succeeded after reset; there is no pal theme to save")
	}
}

func TestCurrentPaletteHonoursXDGStateHome(t *testing.T) {
	h, home := saveHarness(t, "/bin/zsh")
	h.env["XDG_STATE_HOME"] = home + "/state"
	h.app.run([]string{"set", "nord"})
	if got := strings.TrimSpace(readFile(t, home+"/state/pal/current")); got != "nord" {
		t.Errorf("state file holds %q, want nord", got)
	}
}
