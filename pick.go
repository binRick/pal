package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// picker is the state of `pal pick`: the candidate palettes, what has
// been typed, and which match is highlighted.
type picker struct {
	all     []*Palette
	query   string
	matches []*Palette
	sel     int // index into matches
	top     int // first visible match
	rows    int // visible list rows
}

func (pk *picker) refilter() {
	terms := strings.Fields(strings.ToLower(pk.query))
	pk.matches = pk.matches[:0]
next:
	for _, p := range pk.all {
		name := strings.ToLower(p.Name)
		for _, t := range terms {
			if !strings.Contains(name, t) {
				continue next
			}
		}
		pk.matches = append(pk.matches, p)
	}
	pk.sel, pk.top = 0, 0
}

func (pk *picker) move(by int) {
	pk.sel = max(0, min(len(pk.matches)-1, pk.sel+by))
	if pk.sel < pk.top {
		pk.top = pk.sel
	}
	if pk.sel >= pk.top+pk.rows {
		pk.top = pk.sel - pk.rows + 1
	}
}

func (pk *picker) selected() *Palette {
	if len(pk.matches) == 0 {
		return nil
	}
	return pk.matches[pk.sel]
}

// Outcomes of feeding typed bytes to the picker.
const (
	pickContinue = iota
	pickAccept
	pickCancel
)

// feed applies typed bytes to the picker. It returns what the user
// decided and the bytes it could not interpret yet (a lone ESC may be
// the start of an arrow key whose rest has not arrived).
func (pk *picker) feed(buf []byte) (outcome int, pending []byte) {
	for i := 0; i < len(buf); i++ {
		c := buf[i]
		switch {
		case c == '\r' || c == '\n':
			if pk.selected() != nil {
				return pickAccept, nil
			}
		case c == 0x03: // Ctrl-C
			return pickCancel, nil
		case c == 0x7f || c == 0x08:
			if pk.query != "" {
				pk.query = pk.query[:len(pk.query)-1]
				pk.refilter()
			}
		case c == 0x15: // Ctrl-U
			pk.query = ""
			pk.refilter()
		case c == 0x10: // Ctrl-P
			pk.move(-1)
		case c == 0x0e: // Ctrl-N
			pk.move(1)
		case c == 0x1b:
			rest := buf[i+1:]
			if len(rest) == 0 {
				return pickContinue, buf[i:]
			}
			if rest[0] != '[' && rest[0] != 'O' {
				return pickCancel, nil
			}
			end := 1
			for end < len(rest) && (rest[end] < 0x40 || rest[end] > 0x7e) {
				end++
			}
			if end == len(rest) {
				return pickContinue, buf[i:]
			}
			switch string(rest[1 : end+1]) {
			case "A":
				pk.move(-1)
			case "B":
				pk.move(1)
			case "5~":
				pk.move(-pk.rows)
			case "6~":
				pk.move(pk.rows)
			case "H", "1~":
				pk.move(-len(pk.matches))
			case "F", "4~":
				pk.move(len(pk.matches))
			}
			i += end + 1
		case c >= 0x20 && c < 0x7f:
			pk.query += string(c)
			pk.refilter()
		}
	}
	return pickContinue, nil
}

// lines renders the picker: a prompt line, then the visible matches,
// each drawn in its own colors.
func (a *app) pickLines(pk *picker, cols int) []string {
	lines := []string{fmt.Sprintf("  %s%spick>%s %s%s▏%s  %s%d/%d · ↑↓ move · type to filter · enter keep · esc cancel%s",
		bold, cyan, reset, pk.query, gray, reset, gray, len(pk.matches), len(pk.all), reset)}
	for i := pk.top; i < pk.top+pk.rows; i++ {
		if i >= len(pk.matches) {
			lines = append(lines, "")
			continue
		}
		p := pk.matches[i]
		row := "  " + p.Name
		if cols >= 64 {
			row = a.swatchRow(p)
		}
		if i == pk.sel {
			row = bold + yellow + "❯ " + reset + strings.Replace(row, p.Name, bold+p.Name+reset, 1)
		} else {
			row = "  " + row
		}
		lines = append(lines, row)
	}
	return lines
}

// colorQueries asks the terminal for the colors it is showing now, so
// a cancelled pick can put them back exactly. The trailing device-
// attributes request is answered by every terminal, which marks the end
// of the replies without waiting for a timeout.
func colorQueries() string {
	var b strings.Builder
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&b, "\033]4;%d;?\033\\", i)
	}
	b.WriteString("\033]10;?\033\\\033]11;?\033\\\033]12;?\033\\\033[c")
	return b.String()
}

// parseColorReplies turns the terminal's answers to colorQueries into
// the sequences that set those same colors again.
func parseColorReplies(data string) []string {
	var seqs []string
	for _, part := range strings.Split(data, "\033]")[1:] {
		if end := strings.IndexAny(part, "\a\033"); end >= 0 {
			part = part[:end]
		}
		i := strings.LastIndex(part, ";rgb:")
		if i < 0 {
			continue
		}
		target, comps := part[:i], strings.Split(part[i+len(";rgb:"):], "/")
		if len(comps) != 3 {
			continue
		}
		hex := ""
		for _, c := range comps {
			v, err := strconv.ParseUint(c, 16, 16)
			if err != nil || len(c) == 0 || len(c) > 4 {
				hex = ""
				break
			}
			// Scale 1-4 hex digits to 8 bits.
			hex += fmt.Sprintf("%02X", v*255/(1<<(4*uint(len(c)))-1))
		}
		if hex == "" || (target != "10" && target != "11" && target != "12" && !strings.HasPrefix(target, "4;")) {
			continue
		}
		seqs = append(seqs, "\033]"+target+";#"+hex+"\033\\")
	}
	return seqs
}

// pick runs the interactive picker on a terminal already in raw mode
// whose reads time out (returning io.EOF) when nothing is typed.
// Moving the highlight applies that palette at once; Enter keeps it,
// Esc or Ctrl-C puts the previous colors back. leaveRaw is called once the
// picker has left the screen, before anything is printed for the shell.
func (a *app) pick(args []string, in io.Reader, out io.Writer, rows, cols int, leaveRaw func()) int {
	names, err := find(args, nil)
	if err != nil {
		return a.fail(err)
	}
	pk := &picker{rows: max(1, min(12, rows-3))}
	for _, n := range names {
		p, err := loadPalette(n)
		if err != nil {
			return a.fail(err)
		}
		pk.all = append(pk.all, p)
	}
	pk.refilter()
	if cur := a.current(); cur != "" {
		for i, p := range pk.matches {
			if p.Name == cur {
				pk.move(i)
			}
		}
	}

	buf := make([]byte, 4096)
	var pending []byte

	// What to send if the user cancels: the terminal's own report of its
	// colors when it gives one, else the palette pal last applied, else
	// the terminal's defaults.
	var restore []string
	if !a.tmux {
		io.WriteString(out, colorQueries())
		var replies []byte
		for idle := 0; idle < 3; {
			n, err := in.Read(buf)
			if n == 0 {
				if err != nil && err != io.EOF {
					return a.fail(err)
				}
				idle++
				continue
			}
			replies = append(replies, buf[:n]...)
			if i := strings.Index(string(replies), "\033[?"); i >= 0 {
				if j := strings.IndexByte(string(replies[i:]), 'c'); j >= 0 {
					pending = append(pending, replies[i+j+1:]...) // keys typed right behind the replies
					replies = replies[:i]
					break
				}
			}
		}
		restore = parseColorReplies(string(replies))
	} else {
		a.passthrough()
	}
	if len(restore) == 0 {
		restore = resetSequences
		if cur, err := loadPalette(a.current()); err == nil {
			restore = cur.sequences()
		}
	}

	var applied *Palette
	drawn := 0
	draw := func() {
		var b strings.Builder
		if drawn > 0 {
			fmt.Fprintf(&b, "\033[%dA", drawn)
		}
		lines := a.pickLines(pk, cols)
		for _, l := range lines {
			b.WriteString("\r\033[2K" + l + "\r\n")
		}
		drawn = len(lines)
		io.WriteString(out, b.String())
	}
	finish := func() {
		fmt.Fprintf(out, "\033[%dA\r\033[J\033[?25h", drawn)
		leaveRaw()
	}

	io.WriteString(out, "\033[?25l")
	draw()
	for {
		outcome := pickContinue
		if len(pending) > 0 {
			outcome, pending = pk.feed(pending)
		}
		if outcome == pickContinue {
			n, err := in.Read(buf)
			switch {
			case n > 0:
				outcome, pending = pk.feed(append(pending, buf[:n]...))
			case err != nil && err != io.EOF:
				outcome = pickCancel
			case string(pending) == "\033": // a lone Esc: nothing followed it
				outcome = pickCancel
			default:
				continue
			}
		}
		switch outcome {
		case pickAccept:
			p := pk.selected()
			if p != applied {
				io.WriteString(out, encode(p.sequences(), a.tmux))
			}
			finish()
			a.remember(p.Name)
			fmt.Fprintf(a.stderr, "Applied: %s\n", p.Name)
			return 0
		case pickCancel:
			if applied != nil {
				io.WriteString(out, encode(restore, a.tmux))
			}
			finish()
			return 1
		}
		if p := pk.selected(); p != nil && p != applied && (applied != nil || pk.sel != 0 || pk.query != "" || pk.top != 0) {
			io.WriteString(out, encode(p.sequences(), a.tmux))
			applied = p
		}
		draw()
	}
}

// pickTTY puts the controlling terminal into raw mode, runs the picker
// on it and restores the terminal afterwards.
func (a *app) pickTTY(args []string) int {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return a.fail(fmt.Errorf("pal pick needs an interactive terminal: %v", err))
	}
	defer tty.Close()
	stty := func(args ...string) (string, error) {
		cmd := exec.Command("stty", args...)
		cmd.Stdin = tty
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	saved, err := stty("-g")
	if err != nil {
		return a.fail(fmt.Errorf("pal pick needs stty to read keys: %v", err))
	}
	rows, cols := 24, 80
	if size, err := stty("size"); err == nil {
		fmt.Sscanf(size, "%d %d", &rows, &cols)
	}
	// Raw input, with reads that give up after 0.1 s so a lone Esc can
	// be told apart from the start of an arrow key.
	if _, err := stty("raw", "-echo", "min", "0", "time", "1"); err != nil {
		return a.fail(fmt.Errorf("pal pick could not set up the terminal: %v", err))
	}
	restored := false
	leaveRaw := func() {
		if !restored {
			restored = true
			stty(saved)
		}
	}
	defer leaveRaw()
	return a.pick(args, tty, tty, rows, cols, leaveRaw)
}
