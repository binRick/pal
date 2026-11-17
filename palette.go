package main

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
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

// normHex validates a color as six hex digits and upper-cases it.
func normHex(s string) (string, bool) {
	if len(s) != 6 {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return "", false
		}
	}
	return strings.ToUpper(s), true
}

// parsePaleta parses the 19-line paleta format:
// line 0: foreground, line 1: background, line 2: cursor
// lines 3-18: colors 00-15
func parsePaleta(name, data string) (*Palette, error) {
	var lines []string
	for _, l := range strings.Split(data, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) < 19 {
		return nil, fmt.Errorf("paleta %q: need 19 lines, got %d", name, len(lines))
	}
	for i, l := range lines[:19] {
		hex, ok := normHex(l)
		if !ok {
			return nil, fmt.Errorf("paleta %q: line %d: %q is not a 6-digit hex color", name, i+1, l)
		}
		lines[i] = hex
	}
	p := &Palette{Name: name, Format: "paleta", Foreground: lines[0], Background: lines[1], Cursor: lines[2]}
	copy(p.Colors[:], lines[3:19])
	return p, nil
}

// parseKFC parses the key=value format:
// background=rrggbb, foreground=rrggbb, cursor=rrggbb, color00=rrggbb ... color15=rrggbb
// Other keys (selection_background, ...) are ignored.
func parseKFC(name, data string) (*Palette, error) {
	p := &Palette{Name: name, Format: "kfc"}
	for _, line := range strings.Split(data, "\n") {
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		var dst *string
		switch {
		case key == "foreground":
			dst = &p.Foreground
		case key == "background":
			dst = &p.Background
		case key == "cursor":
			dst = &p.Cursor
		case strings.HasPrefix(key, "color"):
			idx, err := strconv.Atoi(key[len("color"):])
			if err != nil || idx < 0 || idx > 15 {
				return nil, fmt.Errorf("kfc %q: unknown color key %q", name, key)
			}
			dst = &p.Colors[idx]
		default:
			continue
		}
		val := strings.TrimSpace(line[eq+1:])
		hex, ok := normHex(val)
		if !ok {
			return nil, fmt.Errorf("kfc %q: %s=%q is not a 6-digit hex color", name, key, val)
		}
		*dst = hex
	}
	missing := func(what, v string) error {
		if v == "" {
			return fmt.Errorf("kfc %q: %s is missing", name, what)
		}
		return nil
	}
	for _, f := range []struct{ what, v string }{
		{"foreground", p.Foreground}, {"background", p.Background}, {"cursor", p.Cursor},
	} {
		if err := missing(f.what, f.v); err != nil {
			return nil, err
		}
	}
	for i, c := range p.Colors {
		if err := missing(fmt.Sprintf("color%02d", i), c); err != nil {
			return nil, err
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

// rgb splits a validated 6-digit hex color into its channels.
func rgb(hex string) (r, g, b int) {
	v, _ := strconv.ParseUint(hex, 16, 32)
	return int(v >> 16), int(v >> 8 & 0xFF), int(v & 0xFF)
}

// IsLight reports whether the palette has a light background, by
// perceived brightness (ITU-R BT.601 weights) of the background color.
func (p *Palette) IsLight() bool {
	r, g, b := rgb(p.Background)
	return (r*299+g*587+b*114)/1000 > 127
}

// filter narrows the palette list: every term must appear in the name
// (case-insensitive), and dark/light restrict by background brightness.
type filter struct {
	terms       []string
	dark, light bool
}

// matching returns the sorted names of the palettes that pass f.
func matching(f filter) ([]string, error) {
	var out []string
names:
	for _, name := range listPalettes() {
		lower := strings.ToLower(name)
		for _, t := range f.terms {
			if !strings.Contains(lower, strings.ToLower(t)) {
				continue names
			}
		}
		if f.dark || f.light {
			p, err := loadPalette(name)
			if err != nil {
				return nil, err
			}
			if p.IsLight() != f.light {
				continue
			}
		}
		out = append(out, name)
	}
	return out, nil
}

// suggest returns up to limit palette names close to a mistyped one:
// names containing it, then names within a few edits of it or of one of
// their dash/underscore-separated words.
func suggest(name string, names []string, limit int) []string {
	q := strings.ToLower(name)
	if len(q) < 2 {
		return nil
	}
	allowed := 3
	if len(q) <= 4 {
		allowed = 1
	} else if len(q) <= 8 {
		allowed = 2
	}
	type hit struct {
		name string
		dist int
	}
	var hits []hit
	for _, n := range names {
		lower := strings.ToLower(n)
		dist := 0
		if !strings.Contains(lower, q) {
			dist = editDistance(q, lower)
			words := strings.FieldsFunc(lower, func(r rune) bool { return r == '-' || r == '_' })
			for _, w := range words {
				if d := editDistance(q, w); d < dist {
					dist = d
				}
			}
			if dist > allowed {
				continue
			}
		}
		hits = append(hits, hit{n, dist})
	}
	sort.Slice(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.dist != b.dist {
			return a.dist < b.dist
		}
		if len(a.name) != len(b.name) {
			return len(a.name) < len(b.name)
		}
		return a.name < b.name
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.name
	}
	return out
}

// editDistance is the Levenshtein distance between two ASCII strings.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
