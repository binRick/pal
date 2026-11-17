<div align="center">

<img src="docs/banner.svg" alt="pal — terminal palette switcher" width="100%"/>

<br/>

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://golang.org)
[![Palettes](https://img.shields.io/badge/Palettes-420-bd93f9?style=flat-square)](palettes/)
[![Zero deps](https://img.shields.io/badge/Dependencies-zero-50fa7b?style=flat-square)](#)
[![License](https://img.shields.io/badge/License-MIT-ff79c6?style=flat-square)](LICENSE)

**Switch your terminal's color palette instantly.
420 themes compiled into a single static binary. No config. No runtime deps.**

</div>

---

## ✨ Features

- 🎨 **420 palettes** — dracula, nord, gruvbox, monokai, solarized, rose-pine, and hundreds more
- ⚡ **Instant** — writes OSC escape sequences directly to stdout, takes effect immediately
- 📦 **Self-contained** — all palette data compiled in via `//go:embed`, nothing to install separately
- 🖥️ **Cross-terminal** — works in xterm, VTE, iTerm2, Kitty, Alacritty, WezTerm, and more
- 🔎 **Search & filter** — `pal list gruvbox --light`, with color swatches via `-v`
- 👁 **True preview** — see any palette in its own colors without changing yours
- 🎲 **Random mode** — explore palettes with `pal random`, optionally `--dark` / `--light`
- 💾 **Save** — `pal save <name>` keeps a theme for every new shell
- 🧹 **Reset** — `pal reset` hands the colors back to your terminal's own settings
- 🪟 **tmux-aware** — sequences are passed through to the outer terminal
- 📋 **Shell export** — dump a theme as shell code for your `.bashrc` / `.zshrc`

---

## 🖥️ Demo

<div align="center">
<a href="https://youtu.be/RFhNgTXXlZ8"><img src="docs/video.png" alt="Watch the pal demo video on YouTube" width="800"/></a>

▶️ **[Watch the two-minute demo on YouTube](https://youtu.be/RFhNgTXXlZ8)**
</div>

### Switch palettes — instantly, with typo suggestions and a way back

<div align="center">
<img src="docs/switch.gif" alt="Switching palettes with pal, pal random and pal reset" width="800"/>
</div>

### Browse — search, swatches, and a preview that doesn't touch your terminal

<div align="center">
<img src="docs/browse.gif" alt="pal list with filters and swatches, and pal preview" width="800"/>
</div>

### Save — keep the current palette for every new shell

<div align="center">
<img src="docs/save.gif" alt="pal save writing the current palette into the shell config" width="800"/>
</div>

---

## 🎨 Palette Showcase

<div align="center">
<img src="docs/palettes.svg" alt="palette showcase" width="880"/>
</div>

---

## 📦 Install

### One-line install (macOS, Linux, Windows)

macOS, Linux, and Git Bash / WSL on Windows:
```bash
curl -fsSL https://raw.githubusercontent.com/binRick/pal/main/install.sh | sh
```

Windows PowerShell:
```powershell
irm https://raw.githubusercontent.com/binRick/pal/main/install.ps1 | iex
```

The script downloads the release binary for your system (Intel or ARM) into `~/.local/bin`
(`%LOCALAPPDATA%\pal` on Windows) and verifies its checksum when the release publishes one.
Set `PAL_VERSION=v0.2.1` to pin a release, or `PAL_INSTALL_DIR` to install elsewhere.

### Homebrew (macOS)
```bash
brew install binRick/tap/pal
```

### Build from source
```bash
git clone https://github.com/binRick/pal
cd pal
go build -o pal .
sudo mv pal /usr/local/bin/
```

### Static binary (Docker, no Go required)
```bash
./build.sh          # produces ./pal (fully static, no libc)
sudo mv pal /usr/local/bin/
```

---

## 🚀 Usage

```
pal list [filter]        📋 list palettes (-v adds color swatches)
pal set <name>           🖌  apply a palette to the current terminal
pal preview <name>       👁  show a palette's colors without applying it
pal random [filter]      🎲 apply a random palette
pal save [name]          💾 keep the current (or named) palette for new shells (~/.zshrc, ~/.bashrc)
pal reset                🧹 restore the terminal's own colors
pal export <name>        📄 print shell code to apply the palette (for .bashrc)
pal <name>               ⚡ shorthand for "pal set <name>"
```

A **filter** is any number of words that must all appear in the name, plus
`--dark` or `--light` to select by background brightness.
Mistyped a name? `pal` suggests the closest matches.

### Quick examples

```bash
# Apply a theme
pal dracula-dark
pal nord
pal monokai-dark

# Look at a theme without changing your terminal
pal preview rose-pine-dark

# Search for themes
pal list gruvbox             # names containing "gruvbox"
pal list --light             # light backgrounds only
pal list -v base16 ocean     # with color swatches

# Random theme, dark backgrounds only
pal random --dark

# Keep the theme you have on now for every new shell
pal save

# ...or apply and keep a named one
pal save nord

# Back to your terminal's own colors
pal reset

# Export to .bashrc — no pal binary needed at shell startup
pal export dracula-dark >> ~/.bashrc
```

With [fzf](https://github.com/junegunn/fzf) installed you get a picker for free:

```bash
pal set "$(pal list | fzf --preview 'pal preview {}')"
```

---

## 💾 Save

`pal save` keeps the palette you last applied (pal remembers its name in
`~/.local/state/pal/current`); `pal save <name>` applies that one first. Either way it is written into the startup file of the
shell you ran it from — `~/.zshrc` (or `$ZDOTDIR/.zshrc`) for zsh, `~/.bashrc` for bash.
The code lives in a marked block, so saving again replaces the previous theme
instead of piling up, and the rest of the file is left alone:

```bash
# >>> pal >>>
# nord — written by `pal save`; run it again to change, or delete this block.
if [ -n "${TMUX:-}" ]; then … fi
# <<< pal <<<
```

To stop, delete the block. Other shells: use `pal export` below.

---

## 📄 Shell Export

`pal export <name>` prints the OSC escape sequences as a shell one-liner.
Paste it into your `.bashrc` or `.zshrc` to apply the palette on every terminal open —
**no `pal` binary required at runtime**. The snippet checks `$TMUX` itself and uses
tmux passthrough when the shell starts inside tmux.

```bash
$ pal export dracula-dark
if [ -n "${TMUX:-}" ]; then tmux set -gqs allow-passthrough on >/dev/null 2>&1; printf '\033Ptmux;…'; else printf '\033]4;0;#44475A\033\\\033]4;1;#FF5555\033\\ ... \033]12;#94A3A5\033\\'; fi
```

Add to your shell profile:
```bash
echo "$(pal export dracula-dark)" >> ~/.bashrc
```

---

## 🎨 All Palettes

<details>
<summary>Click to expand — 420 palettes across two formats</summary>

### Base16 family
`base16-3024-dark` · `base16-ashes-dark` · `base16-bespin-dark` · `base16-brewer-dark` · `base16-chalk-dark` · `base16-codeschool-dark` · `base16-default-dark` · `base16-dracula` · `base16-eighties-dark` · `base16-embers-dark` · `base16-google-dark` · `base16-grayscale-dark` · `base16-gruvbox-hard` · `base16-gruvbox-medium` · `base16-gruvbox-soft` · `base16-isotope-dark` · `base16-londontube-dark` · `base16-monokai-dark` · `base16-nord` · `base16-ocean-dark` · `base16-onedark` · `base16-paraiso-dark` · `base16-railscasts-dark` · `base16-solarized-dark` · `base16-tomorrow-night` · `base16-twilight-dark` · and many more

### Popular editor themes
`dracula-dark` · `monokai-dark` · `nord` · `gruvbox-dark` · `solarized-dark` · `solarized-light` · `rose-pine-dark` · `onedark-dark` · `tomorrow-night-dark` · `gotham-dark` · `cyberpunk-dark` · `fairyfloss-dark` · `night-owl-dark` · `vscode` · `atom-dark` · `ayu-dark` · `afterglow-dark` · `challengerdeep-dark`

### DKEG designer series
`dkeg-amiox` · `dkeg-bark` · `dkeg-blend` · `dkeg-blok` · `dkeg-branch` · `dkeg-coco` · `dkeg-corduroy` · `dkeg-depth` · `dkeg-fury` · `dkeg-kit` · `dkeg-leaf` · `dkeg-lumen` · `dkeg-owl` · `dkeg-poly` · `dkeg-shade` · `dkeg-sprout` · `dkeg-subtle` · `dkeg-urban` · `dkeg-victory` · and 30+ more

### Tempus series
`tempus_autumn` · `tempus_classic` · `tempus_day` · `tempus_dusk` · `tempus_future` · `tempus_night` · `tempus_rift` · `tempus_spring` · `tempus_summer` · `tempus_tempest` · `tempus_warp` · `tempus_winter`

### Sexy themes
`sexy-monokai` · `sexy-gotham` · `sexy-material` · `sexy-hybrid` · `sexy-tango` · `sexy-neon` · `sexy-zenburn` · and 50+ more

</details>

---

## ⚙️ How It Works

`pal` applies color palettes using **OSC (Operating System Command) escape sequences** — the same mechanism used by terminal emulators like iTerm2's built-in theme switcher.

```
\033]4;N;#RRGGBB\033\   →  set palette color N (0–15)
\033]10;#RRGGBB\033\    →  set foreground color
\033]11;#RRGGBB\033\    →  set background color
\033]12;#RRGGBB\033\    →  set cursor color
\033]104\033\ · 110 · 111 · 112  →  reset palette / foreground / background / cursor
```

These sequences are interpreted live by your terminal. **No restart required** — the change takes effect immediately.

- They go to stdout when it is a terminal, and to `/dev/tty` otherwise, so `pal` still
  recolors the terminal from scripts, hooks and `$(...)`.
- Inside tmux each sequence is wrapped in DCS passthrough (and `allow-passthrough` is
  switched on) so it reaches the outer terminal.
- `pal preview` and `pal list -v` paint with 24-bit color when `$COLORTERM` announces
  it, and fall back to the nearest of the 256 xterm colors otherwise.

### Palette formats

Two formats from [`c_palettes`](https://github.com/binRick/c_palettes) are supported and compiled in:

| Format | Location | Structure |
|--------|----------|-----------|
| `paleta` | `palettes/paleta/` | 19-line hex values: fg, bg, cursor, then colors 0–15 |
| `kfc` | `palettes/kfc/dark/` | `key=value` pairs: `color00=rrggbb`, `foreground=rrggbb`, … |

---

## 🐳 Static Build

Build a fully static binary (no libc, no cgo) using Docker:

```bash
./build.sh
```

The output `./pal` binary runs on any Linux x86_64 — drop it anywhere and it works.

---

## 📁 Project Structure

```
pal/
├── main.go              CLI — commands, filters, usage
├── palette.go           embedded palettes — parse, validate, search, suggest
├── term.go              terminal output — OSC sequences, tmux, preview, export
├── save.go              `pal save` — write a theme into the shell's rc file
├── *_test.go            tests (every embedded palette is checked)
├── go.mod               module: github.com/binRick/pal
├── install.sh           One-line installer (macOS, Linux, Git Bash)
├── install.ps1          One-line installer (Windows PowerShell)
├── build.sh             Docker static build script
├── docs/
│   ├── banner.svg       Hero banner
│   ├── *.gif            Demo recordings (switch, browse, save)
│   ├── video.png        Poster for the YouTube demo
│   └── palettes.svg     Palette showcase grid
└── palettes/
    ├── paleta/          169 paleta-format themes
    └── kfc/dark/        252 kfc-format themes (one name shared with paleta)
```

---

## 🙏 Credits

Palette data sourced from [binRick/c_palettes](https://github.com/binRick/c_palettes), a C-based terminal palette manager with an extensive collection of curated themes.

---

<div align="center">
<sub>Built with Go · OSC escape sequences · <code>//go:embed</code></sub>
</div>

<!-- scc-start -->
## Code Statistics

```mermaid
pie showData
    title Code by language
    "Go" : 1515
    "Markdown" : 257
    "YAML" : 191
    "SVG" : 159
    "Shell" : 89
    "Other" : 48
```

| Language | Files | Lines | Blanks | Comments | Code | Complexity |
|---|---|---|---|---|---|---|
| Go | 6 | 1,765 | 125 | 125 | 1,515 | 448 |
| Markdown | 2 | 351 | 94 | 0 | 257 | 0 |
| SVG | 2 | 213 | 26 | 28 | 159 | 0 |
| Shell | 2 | 117 | 17 | 11 | 89 | 15 |
| YAML | 2 | 232 | 39 | 2 | 191 | 0 |
| License | 1 | 21 | 4 | 0 | 17 | 0 |
| Powershell | 1 | 43 | 5 | 7 | 31 | 19 |
| **Total** | **16** | **2,742** | **310** | **173** | **2,259** | **482** |

- **Estimated Cost to Develop (organic):** $63,563
- **Estimated Schedule Effort (organic):** 4.83 months
- **Estimated People Required (organic):** 1.17
- **Processed:** 89,790 bytes (0.090 megabytes)

*Generated with [scc](https://github.com/boyter/scc) on 2026-11-17*
<!-- scc-end -->
