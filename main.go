package main

import (
	"embed"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"os/exec"
	"sort"
	"strings"
)

//go:embed palettes
var paletteFS embed.FS

type Palette struct {
	Name       string
	Format     string
	Foreground string
	Background string
	Cursor     string
	Colors     [16]string
}

// parsePaleta parses the 19-line paleta format:
// line 0: foreground, line 1: background, line 2: cursor
// lines 3-18: colors 00-15
func parsePaleta(name, data string) (*Palette, error) {
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(data), "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) < 19 {
		return nil, fmt.Errorf("paleta %q: need 19 lines, got %d", name, len(lines))
	}
	p := &Palette{Name: name, Format: "paleta"}
	p.Foreground = lines[0]
	p.Background = lines[1]
	p.Cursor = lines[2]
	for i := 0; i < 16; i++ {
		p.Colors[i] = lines[3+i]
	}
	return p, nil
}

// parseKFC parses the key=value format:
// background=rrggbb, foreground=rrggbb, cursor=rrggbb, color00=rrggbb ... color15=rrggbb
func parseKFC(name, data string) (*Palette, error) {
	p := &Palette{Name: name, Format: "kfc"}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "foreground":
			p.Foreground = val
		case "background":
			p.Background = val
		case "cursor":
			p.Cursor = val
		default:
			if strings.HasPrefix(key, "color") && len(key) >= 7 {
				var idx int
				fmt.Sscanf(key[5:], "%d", &idx)
				if idx >= 0 && idx < 16 {
					p.Colors[idx] = val
				}
			}
		}
	}
	return p, nil
}

func loadPalette(name string) (*Palette, error) {
	if data, err := paletteFS.ReadFile("palettes/paleta/" + name); err == nil {
		return parsePaleta(name, string(data))
	}
	if data, err := paletteFS.ReadFile("palettes/kfc/dark/" + name); err == nil {
		return parseKFC(name, string(data))
	}
	return nil, fmt.Errorf("palette %q not found", name)
}

func listPalettes() []string {
	seen := map[string]bool{}
	var names []string
	for _, dir := range []string{"palettes/paleta", "palettes/kfc/dark"} {
		entries, err := fs.ReadDir(paletteFS, dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && !seen[e.Name()] {
				names = append(names, e.Name())
				seen[e.Name()] = true
			}
		}
	}
	sort.Strings(names)
	return names
}

// export prints a shell printf one-liner that applies the palette without needing pal at runtime.
// Paste the output into ~/.bashrc or ~/.zshrc.
func export(p *Palette) {
	var seqs []string
	for i, c := range p.Colors {
		if c != "" {
			seqs = append(seqs, fmt.Sprintf("\\033]4;%d;#%s\\033\\\\", i, strings.ToUpper(c)))
		}
	}
	if p.Foreground != "" {
		seqs = append(seqs, fmt.Sprintf("\\033]10;#%s\\033\\\\", strings.ToUpper(p.Foreground)))
	}
	if p.Background != "" {
		seqs = append(seqs, fmt.Sprintf("\\033]11;#%s\\033\\\\", strings.ToUpper(p.Background)))
	}
	if p.Cursor != "" {
		seqs = append(seqs, fmt.Sprintf("\\033]12;#%s\\033\\\\", strings.ToUpper(p.Cursor)))
	}
	fmt.Printf("printf '%s'\n", strings.Join(seqs, ""))
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
// `allow-passthrough on` server option (which we enable below).
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
	if !inTmux() {
		return
	}
	cmd := exec.Command("tmux", "set", "-gqs", "allow-passthrough", "on")
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run()
}

// apply writes OSC escape sequences to the terminal to set the palette.
// These work in xterm, VTE-based terminals, iTerm2, Kitty, and most modern terminals.
// Inside tmux we wrap each escape in DCS-tmux passthrough so the outer
// terminal sees them too — without that, only tmux's internal palette
// changes and apps using palette-indexed cells (starship, p10k, etc.)
// keep rendering with the outer terminal's stale palette.
func apply(p *Palette) {
	w := os.Stdout
	tmuxed := inTmux()
	if tmuxed {
		enableTmuxPassthrough()
	}
	emit := func(seq string) {
		if tmuxed {
			fmt.Fprint(w, wrapForTmux(seq))
		} else {
			fmt.Fprint(w, seq)
		}
	}
	for i, c := range p.Colors {
		if c != "" {
			emit(fmt.Sprintf("\033]4;%d;#%s\033\\", i, strings.ToUpper(c)))
		}
	}
	if p.Foreground != "" {
		emit(fmt.Sprintf("\033]10;#%s\033\\", strings.ToUpper(p.Foreground)))
	}
	if p.Background != "" {
		emit(fmt.Sprintf("\033]11;#%s\033\\", strings.ToUpper(p.Background)))
	}
	if p.Cursor != "" {
		emit(fmt.Sprintf("\033]12;#%s\033\\", strings.ToUpper(p.Cursor)))
	}
}

func preview(p *Palette) {
	fmt.Printf("\n  palette: %s\n\n  ", p.Name)
	for i := 0; i < 8; i++ {
		fmt.Printf("\033[48;5;%dm  \033[0m", i)
	}
	fmt.Printf("\n  ")
	for i := 8; i < 16; i++ {
		fmt.Printf("\033[48;5;%dm  \033[0m", i)
	}
	fmt.Printf("\n\n  fg:#%s  bg:#%s  cursor:#%s\n\n",
		strings.ToUpper(p.Foreground),
		strings.ToUpper(p.Background),
		strings.ToUpper(p.Cursor),
	)
}

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	cyan   = "\033[36m"
	yellow = "\033[33m"
	green  = "\033[32m"
	purple = "\033[35m"
	white  = "\033[97m"
	gray   = "\033[90m"
)

func usage() {
	w := os.Stderr
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s%s🎨 pal%s%s — terminal palette switcher%s\n", bold, cyan, reset, gray, reset)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s%sUsage%s\n", bold, white, reset)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "    %s%spal list%s               %s📋 list all available palettes%s\n", bold, yellow, reset, gray, reset)
	fmt.Fprintf(w, "    %s%spal set %s%s<name>%s         %s🖌  apply a palette to the current terminal%s\n", bold, yellow, reset, purple, reset, gray, reset)
	fmt.Fprintf(w, "    %s%spal preview %s%s<name>%s     %s👁  apply and show a color swatch%s\n", bold, yellow, reset, purple, reset, gray, reset)
	fmt.Fprintf(w, "    %s%spal random%s             %s🎲 apply a random palette%s\n", bold, yellow, reset, gray, reset)
	fmt.Fprintf(w, "    %s%spal export %s%s<name>%s      %s📄 print shell code to embed in .bashrc (no pal needed)%s\n", bold, yellow, reset, purple, reset, gray, reset)
	fmt.Fprintf(w, "    %s%spal %s%s<name>%s            %s⚡ shorthand for \"pal set <name>\"%s\n", bold, yellow, reset, purple, reset, gray, reset)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s420 palettes embedded · paleta + kfc/dark formats%s\n", gray, reset)
	fmt.Fprintln(w)
}

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		usage()
		return
	}

	switch args[0] {
	case "list", "ls", "-l", "--list":
		for _, n := range listPalettes() {
			fmt.Println(n)
		}

	case "set", "apply":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: pal set <name>")
			os.Exit(1)
		}
		p, err := loadPalette(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		apply(p)

	case "preview", "show", "p":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: pal preview <name>")
			os.Exit(1)
		}
		p, err := loadPalette(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		preview(p)

	case "export", "shell", "bashrc":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: pal export <name>")
			os.Exit(1)
		}
		p, err := loadPalette(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		export(p)

	case "random", "rand", "r":
		names := listPalettes()
		if len(names) == 0 {
			fmt.Fprintln(os.Stderr, "no palettes found")
			os.Exit(1)
		}
		name := names[rand.Intn(len(names))]
		p, err := loadPalette(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		apply(p)
		fmt.Fprintf(os.Stderr, "Applied: %s\n", name)

	case "-h", "--help", "help":
		usage()

	default:
		// Try treating the argument as a palette name directly
		p, err := loadPalette(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "unknown command or palette: %s\n\n", args[0])
			usage()
			os.Exit(1)
		}
		apply(p)
	}
}
