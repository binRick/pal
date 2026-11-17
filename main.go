package main

import (
	"bufio"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
)

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	cyan   = "\033[36m"
	yellow = "\033[33m"
	purple = "\033[35m"
	white  = "\033[97m"
	gray   = "\033[90m"
)

// app carries everything a command touches, so tests can drive the CLI
// without a real terminal.
type app struct {
	stdout, stderr io.Writer
	term           io.Writer // where OSC sequences go; nil = found on first use
	tmux           bool      // wrap sequences in DCS-tmux passthrough
	truecolor      bool      // terminal announced 24-bit color
	passthrough    func()    // turns on tmux allow-passthrough
	getenv         func(string) string
	parentShell    func() string // name of the process that ran pal
}

func main() {
	colorterm := os.Getenv("COLORTERM")
	a := &app{
		stdout:      os.Stdout,
		stderr:      os.Stderr,
		tmux:        inTmux(),
		truecolor:   colorterm == "truecolor" || colorterm == "24bit",
		passthrough: enableTmuxPassthrough,
		getenv:      os.Getenv,
		parentShell: parentProcessName,
	}
	os.Exit(a.run(os.Args[1:]))
}

func (a *app) usage() {
	w := a.stderr
	rows := []struct{ cmd, arg, icon, desc string }{
		{"pal list", "[filter]", "📋", "list palettes (-v adds color swatches)"},
		{"pal set", "<name>", "🖌 ", "apply a palette to the current terminal"},
		{"pal preview", "<name>", "👁 ", "show a palette's colors without applying it"},
		{"pal pick", "[filter]", "🎯", "choose interactively, with live preview"},
		{"pal random", "[filter]", "🎲", "apply a random palette"},
		{"pal save", "[name]", "💾", "keep the current (or named) palette for new shells (~/.zshrc, ~/.bashrc)"},
		{"pal reset", "", "🧹", "restore the terminal's own colors"},
		{"pal export", "<name>", "📄", "print shell code to embed in .bashrc (no pal needed)"},
		{"pal", "<name>", "⚡", `shorthand for "pal set <name>"`},
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s%s🎨 pal%s%s — terminal palette switcher%s\n", bold, cyan, reset, gray, reset)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s%sUsage%s\n", bold, white, reset)
	fmt.Fprintln(w)
	for _, r := range rows {
		pad := strings.Repeat(" ", 24-len(r.cmd)-len(r.arg))
		fmt.Fprintf(w, "    %s%s%s%s %s%s%s%s%s %s%s%s\n",
			bold, yellow, r.cmd, reset, purple, r.arg, reset, pad, r.icon, gray, r.desc, reset)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s%sFilter%s  %swords that must appear in the name, and --dark or --light%s\n", bold, white, reset, gray, reset)
	fmt.Fprintf(w, "          %se.g. pal list gruvbox --light · pal random --dark%s\n", gray, reset)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s%d palettes embedded · paleta + kfc/dark formats%s\n", gray, len(listPalettes()), reset)
	fmt.Fprintln(w)
}

func (a *app) run(args []string) int {
	if len(args) == 0 {
		a.usage()
		return 0
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "list", "ls", "-l", "--list":
		return a.list(rest)

	case "set", "apply":
		p := a.named("set", rest)
		if p == nil {
			return 1
		}
		return a.apply(p)

	case "save":
		if len(rest) == 0 {
			name := a.current()
			if name == "" {
				return a.fail(fmt.Errorf("no current palette to save: apply one first, or run pal save <name>"))
			}
			rest = []string{name}
		}
		p := a.named("save", rest)
		if p == nil {
			return 1
		}
		return a.save(p)

	case "preview", "show", "p":
		p := a.named("preview", rest)
		if p == nil {
			return 1
		}
		var b strings.Builder
		a.preview(&b, p)
		io.WriteString(a.stdout, b.String())

	case "export", "shell", "bashrc":
		p := a.named("export", rest)
		if p == nil {
			return 1
		}
		fmt.Fprint(a.stdout, exportSnippet(p))

	case "pick", "choose", "i":
		return a.pickTTY(rest)

	case "random", "rand", "r":
		return a.random(rest)

	case "reset":
		if err := a.send(resetSequences); err != nil {
			return a.fail(err)
		}
		a.remember("")

	case "-h", "--help", "help":
		a.usage()

	default:
		// Try treating the argument as a palette name directly
		p, err := loadPalette(cmd)
		if err != nil {
			fmt.Fprintf(a.stderr, "unknown command or palette: %s\n", cmd)
			if !a.didYouMean(cmd) {
				a.usage()
			}
			return 1
		}
		return a.apply(p)
	}
	return 0
}

func (a *app) fail(err error) int {
	fmt.Fprintln(a.stderr, err)
	return 1
}

// named loads the palette a command was given. On failure it has
// already told the user why and returns nil.
func (a *app) named(cmd string, args []string) *Palette {
	if len(args) == 0 {
		fmt.Fprintf(a.stderr, "usage: pal %s <name>\n", cmd)
		return nil
	}
	p, err := loadPalette(args[0])
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		a.didYouMean(args[0])
		return nil
	}
	return p
}

// didYouMean prints palettes close to a mistyped name, if there are any.
func (a *app) didYouMean(name string) bool {
	near := suggest(name, listPalettes(), 6)
	if len(near) == 0 {
		return false
	}
	fmt.Fprintf(a.stderr, "did you mean: %s?\n", strings.Join(near, ", "))
	return true
}

func (a *app) apply(p *Palette) int {
	if err := a.send(p.sequences()); err != nil {
		return a.fail(err)
	}
	a.remember(p.Name)
	return 0
}

// parseFilter reads a filter from command arguments: --dark, --light,
// and words to match against names. flags names any further switches
// the command accepts.
func parseFilter(args []string, flags map[string]*bool) (filter, error) {
	var f filter
	for _, arg := range args {
		switch {
		case arg == "--dark":
			f.dark = true
		case arg == "--light":
			f.light = true
		case flags[arg] != nil:
			*flags[arg] = true
		case strings.HasPrefix(arg, "-"):
			return f, fmt.Errorf("unknown flag: %s", arg)
		default:
			f.terms = append(f.terms, arg)
		}
	}
	if f.dark && f.light {
		return f, fmt.Errorf("--dark and --light exclude each other")
	}
	return f, nil
}

// find returns the palettes a command's filter arguments select; an
// empty result is an error so scripts can tell.
func find(args []string, flags map[string]*bool) ([]string, error) {
	f, err := parseFilter(args, flags)
	if err != nil {
		return nil, err
	}
	names, err := matching(f)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no palettes match")
	}
	return names, nil
}

func (a *app) list(args []string) int {
	swatches := false
	names, err := find(args, map[string]*bool{"-v": &swatches, "--swatches": &swatches})
	if err != nil {
		return a.fail(err)
	}
	w := bufio.NewWriter(a.stdout)
	defer w.Flush()
	for _, name := range names {
		if !swatches {
			fmt.Fprintln(w, name)
			continue
		}
		p, err := loadPalette(name)
		if err != nil {
			w.Flush()
			return a.fail(err)
		}
		fmt.Fprintln(w, a.swatchRow(p))
	}
	return 0
}

func (a *app) random(args []string) int {
	names, err := find(args, nil)
	if err != nil {
		return a.fail(err)
	}
	p, err := loadPalette(names[rand.Intn(len(names))])
	if err != nil {
		return a.fail(err)
	}
	if code := a.apply(p); code != 0 {
		return code
	}
	fmt.Fprintf(a.stderr, "Applied: %s\n", p.Name)
	return 0
}
