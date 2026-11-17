package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	saveBegin = "# >>> pal >>>"
	saveEnd   = "# <<< pal <<<"
)

// parentProcessName asks ps what ran pal, e.g. "-zsh" or "/bin/bash".
// Empty when that cannot be found out (no ps, Windows).
func parentProcessName() string {
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(os.Getppid())).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// shellName reduces "/bin/zsh" or a login shell's "-zsh" to "zsh".
func shellName(s string) string {
	return strings.TrimPrefix(filepath.Base(strings.TrimSpace(s)), "-")
}

// shellConfig returns the startup file of the shell pal was run from:
// the invoking process if it is a shell pal can write for, else $SHELL.
func (a *app) shellConfig() (string, error) {
	home := a.getenv("HOME")
	if home == "" {
		return "", fmt.Errorf("cannot save: $HOME is not set")
	}
	shell := ""
	for _, candidate := range []string{a.parentShell(), a.getenv("SHELL")} {
		shell = shellName(candidate)
		switch shell {
		case "zsh":
			if dir := a.getenv("ZDOTDIR"); dir != "" {
				return filepath.Join(dir, ".zshrc"), nil
			}
			return filepath.Join(home, ".zshrc"), nil
		case "bash":
			return filepath.Join(home, ".bashrc"), nil
		}
	}
	return "", fmt.Errorf("cannot save for shell %q: only zsh and bash are supported (use `pal export` instead)", shell)
}

// withSavedBlock returns rc with its pal block replaced by block, or
// with block appended when there is none yet.
func withSavedBlock(rc, block string) string {
	if start := strings.Index(rc, saveBegin); start >= 0 {
		if end := strings.Index(rc[start:], saveEnd); end >= 0 {
			rest := strings.TrimPrefix(rc[start+end+len(saveEnd):], "\n")
			return rc[:start] + block + rest
		}
	}
	if rc != "" && !strings.HasSuffix(rc, "\n") {
		rc += "\n"
	}
	if rc != "" {
		rc += "\n"
	}
	return rc + block
}

// save applies p and writes it into the invoking shell's startup file,
// so every new shell starts with it.
func (a *app) save(p *Palette) int {
	path, err := a.shellConfig()
	if err != nil {
		return a.fail(err)
	}
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return a.fail(err)
	}
	block := saveBegin + "\n" +
		"# " + p.Name + " — written by `pal save`; run it again to change, or delete this block.\n" +
		exportSnippet(p) +
		saveEnd + "\n"
	// Written in place (not renamed over) so a symlinked dotfile and
	// the file's permissions survive.
	if err := os.WriteFile(path, []byte(withSavedBlock(string(old), block)), 0o644); err != nil {
		return a.fail(err)
	}
	if code := a.apply(p); code != 0 {
		return code
	}
	fmt.Fprintf(a.stderr, "Saved %s to %s\n", p.Name, path)
	return 0
}

// statePath is where the name of the last applied palette is kept, so
// `pal save` knows what "current" means. Empty when there is no home.
func (a *app) statePath() string {
	if dir := a.getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "pal", "current")
	}
	if home := a.getenv("HOME"); home != "" {
		return filepath.Join(home, ".local", "state", "pal", "current")
	}
	return ""
}

// remember records name as the current palette ("" forgets it). Best
// effort: applying a palette must not fail over a state file.
func (a *app) remember(name string) {
	path := a.statePath()
	if path == "" {
		return
	}
	if name == "" {
		os.Remove(path)
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
		os.WriteFile(path, []byte(name+"\n"), 0o644)
	}
}

// current returns the last applied palette, or "" if none is known.
func (a *app) current() string {
	path := a.statePath()
	if path == "" {
		return ""
	}
	b, _ := os.ReadFile(path)
	return strings.TrimSpace(string(b))
}
