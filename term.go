package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// sequences returns the OSC escapes that apply p: palette colors 0-15
// (OSC 4), then foreground (10), background (11) and cursor (12).
// These work in xterm, VTE-based terminals, iTerm2, Kitty, and most
// modern terminals.
func (p *Palette) sequences() []string {
	seqs := make([]string, 0, 19)
	for i, c := range p.Colors {
		seqs = append(seqs, fmt.Sprintf("\033]4;%d;#%s\033\\", i, c))
	}
	return append(seqs,
		"\033]10;#"+p.Foreground+"\033\\",
		"\033]11;#"+p.Background+"\033\\",
		"\033]12;#"+p.Cursor+"\033\\",
	)
}

// resetSequences hand the colors back to the terminal's own
// configuration: palette (OSC 104), foreground (110), background (111)
// and cursor (112).
var resetSequences = []string{
	"\033]104\033\\",
	"\033]110\033\\",
	"\033]111\033\\",
	"\033]112\033\\",
}

// inTmux reports whether we're running inside a tmux session. Set by
// tmux itself for every child process; absent on every other path.
func inTmux() bool { return os.Getenv("TMUX") != "" }

// wrapForTmux wraps an OSC escape so it punches through tmux's parser
// and reaches the outer terminal. Tmux normally interprets OSC
// sequences (e.g. OSC 4 for palette) and applies them only to its own
// internal state — without the wrapper they never reach rbterm /
// alacritty / iTerm2 underneath, so the outer terminal's palette and
// the tmux palette drift out of sync. The DCS-tmux passthrough format
// is `\ePtmux;<inner-with-each-ESC-doubled>\e\\`. Tmux strips the
// wrapper and forwards the inner sequence verbatim. Requires the
// `allow-passthrough on` server option (see enableTmuxPassthrough).
func wrapForTmux(seq string) string {
	doubled := strings.ReplaceAll(seq, "\033", "\033\033")
	return "\033Ptmux;" + doubled + "\033\\"
}

// enableTmuxPassthrough turns on tmux's allow-passthrough server
// option so DCS-tmux passthrough sequences are honoured. Idempotent:
// if it's already on this is a no-op. Silently failing is fine — the
// next-best path is for the user to set it manually (or not, in
// which case the visible bug is "pal looks like it didn't change
// the palette inside tmux", which is the bug we're already trying
// to fix).
func enableTmuxPassthrough() {
	_ = exec.Command("tmux", "set", "-gqs", "allow-passthrough", "on").Run()
}

// encode joins sequences into the bytes to send. Inside tmux each one
// is wrapped in DCS-tmux passthrough so the outer terminal sees it too
// — without that, only tmux's internal palette changes and apps using
// palette-indexed cells (starship, p10k, etc.) keep rendering with the
// outer terminal's stale palette.
func encode(seqs []string, tmux bool) string {
	var b strings.Builder
	for _, s := range seqs {
		if tmux {
			s = wrapForTmux(s)
		}
		b.WriteString(s)
	}
	return b.String()
}

// send delivers escape sequences to the terminal in a single write.
func (a *app) send(seqs []string) error {
	w := a.term
	if w == nil {
		f, done := terminal(os.Stdout, openTTY)
		defer done()
		w = f
	}
	if a.tmux {
		a.passthrough()
	}
	_, err := io.WriteString(w, encode(seqs, a.tmux))
	return err
}

func openTTY() (*os.File, error) { return os.OpenFile("/dev/tty", os.O_WRONLY, 0) }

// terminal picks where escape sequences go. Stdout when it is a
// device; otherwise the controlling terminal, so that redirecting or
// capturing stdout (`pal random > log`, a script, a prompt hook) still
// recolors the terminal. With no controlling terminal either — as in
// `ssh host pal nord` — stdout is the only path back to a terminal.
// The returned func releases whatever was opened.
func terminal(stdout *os.File, openTTY func() (*os.File, error)) (*os.File, func()) {
	if fi, err := stdout.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return stdout, func() {}
	}
	if tty, err := openTTY(); err == nil {
		return tty, func() { tty.Close() }
	}
	return stdout, func() {}
}

// exportSnippet returns shell code that applies p without needing pal
// at runtime, for pasting into ~/.bashrc or ~/.zshrc. It makes the same
// choice pal does when it runs: plain sequences normally, passthrough-
// wrapped ones inside tmux.
func exportSnippet(p *Palette) string {
	seqs := p.sequences()
	return fmt.Sprintf(
		"if [ -n \"${TMUX:-}\" ]; then tmux set -gqs allow-passthrough on >/dev/null 2>&1; printf '%s'; else printf '%s'; fi\n",
		printfEscape(encode(seqs, true)), printfEscape(encode(seqs, false)))
}

// printfEscape spells raw bytes as a POSIX printf format string that is
// safe inside single quotes.
func printfEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\033", `\033`, "%", "%%").Replace(s)
}

// xterm256 maps a color to the nearest entry of the xterm 256-color
// palette: the 6x6x6 cube (16-231) or the grey ramp (232-255).
func xterm256(hex string) int {
	r, g, b := rgb(hex)
	levels := [6]int{0, 95, 135, 175, 215, 255}
	nearest := func(v int) int {
		best := 0
		for i, l := range levels {
			if abs(v-l) < abs(v-levels[best]) {
				best = i
			}
		}
		return best
	}
	dist := func(r2, g2, b2 int) int {
		return (r-r2)*(r-r2) + (g-g2)*(g-g2) + (b-b2)*(b-b2)
	}
	ri, gi, bi := nearest(r), nearest(g), nearest(b)
	step := min(max(((r+g+b)/3-8+5)/10, 0), 23)
	grey := 8 + 10*step
	if dist(grey, grey, grey) < dist(levels[ri], levels[gi], levels[bi]) {
		return 232 + step
	}
	return 16 + 36*ri + 6*gi + bi
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// paint returns the escape selecting hex as background (layer 48) or
// foreground (layer 38). Colors are always given by value, never by
// palette index, so a swatch shows the palette it describes rather than
// whatever the terminal currently has loaded.
func (a *app) paint(layer int, hex string) string {
	if a.truecolor {
		r, g, b := rgb(hex)
		return fmt.Sprintf("\033[%d;2;%d;%d;%dm", layer, r, g, b)
	}
	return fmt.Sprintf("\033[%d;5;%dm", layer, xterm256(hex))
}

func (a *app) bg(hex string) string { return a.paint(48, hex) }
func (a *app) fg(hex string) string { return a.paint(38, hex) }

// preview draws p on its own background: the 16 colors as blocks, the
// same colors as text (which is where a weak palette shows), and a
// line of foreground text ending in the cursor color.
func (a *app) preview(w io.Writer, p *Palette) {
	kind := "dark"
	if p.IsLight() {
		kind = "light"
	}
	back := a.bg(p.Background)
	row := func(content string) {
		fmt.Fprintf(w, "  %s  %s%s  %s\n", back, content, back, reset)
	}
	blocks := func(colors []string) string {
		var b strings.Builder
		for _, c := range colors {
			b.WriteString(a.bg(c) + "    ")
		}
		return b.String()
	}
	labels := func(colors []string) string {
		var b strings.Builder
		for i, c := range colors {
			b.WriteString(a.fg(c) + [8]string{"blk ", "red ", "grn ", "yel ", "blu ", "mag ", "cyn ", "wht "}[i])
		}
		return b.String()
	}
	blank := strings.Repeat(" ", 32)

	fmt.Fprintf(w, "\n  palette: %s (%s)\n\n", p.Name, kind)
	row(blank)
	row(blocks(p.Colors[:8]))
	row(blocks(p.Colors[8:]))
	row(blank)
	row(labels(p.Colors[:8]))
	row(labels(p.Colors[8:]))
	row(a.fg(p.Foreground) + "The quick brown fox jumps over " + a.bg(p.Cursor) + " ")
	row(blank)
	fmt.Fprintf(w, "\n  fg:#%s  bg:#%s  cursor:#%s\n\n", p.Foreground, p.Background, p.Cursor)
}

// swatchRow is one line of `pal list -v`: a text sample on the
// palette's background, its 16 colors, then its name.
func (a *app) swatchRow(p *Palette) string {
	var b strings.Builder
	b.WriteString(a.bg(p.Background) + a.fg(p.Foreground) + " Aa " + reset + " ")
	for _, c := range p.Colors {
		b.WriteString(a.bg(c) + "  ")
	}
	b.WriteString(reset + "  " + p.Name)
	return b.String()
}
