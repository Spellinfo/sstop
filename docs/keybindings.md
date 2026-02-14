# Keybindings

sstop uses vim-style navigation with additional shortcuts for each view.

## Navigation (all views)

| Key | Action |
|-----|--------|
| `j` / `↓` | Move cursor down |
| `k` / `↑` | Move cursor up |
| `PgUp` / `Ctrl+U` | Page up (half screen) |
| `PgDown` / `Ctrl+D` | Page down (half screen) |
| `g` / `Home` | Jump to first item |
| `G` / `End` | Jump to last item |

## Process Table View

| Key | Action |
|-----|--------|
| `Enter` | Open process detail view |
| `s` | Cycle sort column (Rate → Down → Up → PID → Name → Conns) |
| `/` | Open search/filter prompt |
| `h` | Switch to Remote Hosts view |
| `l` | Switch to Listen Ports view |
| `K` | Open kill process overlay |

## Process Detail View

| Key | Action |
|-----|--------|
| `d` | Toggle DNS hostname resolution for remote addresses |
| `K` | Open kill process overlay |
| `Esc` | Return to process table |

## Remote Hosts View

| Key | Action |
|-----|--------|
| `Esc` | Return to process table |
| Navigation keys | Same as above |

## Listen Ports View

| Key | Action |
|-----|--------|
| `Esc` | Return to process table |
| Navigation keys | Same as above |

## Global (any view)

| Key | Action |
|-----|--------|
| `i` / `Tab` | Cycle through interfaces (all → eth0 → wlan0 → ... → all) |
| `+` / `=` | Increase refresh speed (shorter interval) |
| `-` | Decrease refresh speed (longer interval) |
| `Space` | Pause/resume data updates |
| `?` | Toggle help overlay |
| `q` / `Ctrl+C` | Quit |

## Search/Filter Mode

| Key | Action |
|-----|--------|
| Any character | Type into search field (live filtering) |
| `Enter` | Confirm filter and return to normal mode |
| `Esc` | Cancel and clear filter |

Search matches case-insensitively against process name, full command line, and PID.

## Kill Overlay

| Key | Action |
|-----|--------|
| `j` / `k` / `↑` / `↓` | Navigate signal list |
| `Enter` | Send selected signal to process |
| `Esc` | Cancel and close overlay |
| Any key (on result) | Dismiss result message |

Available signals: SIGTERM (15), SIGKILL (9), SIGINT (2), SIGHUP (1), SIGSTOP (19), SIGCONT (18), SIGUSR1 (10), SIGUSR2 (12).

## Mouse

| Action | Effect |
|--------|--------|
| Left click | Select the clicked row |
| Click selected row | Enter detail view (process table only) |
| Scroll wheel up | Move cursor up |
| Scroll wheel down | Move cursor down |

Mouse is disabled when help overlay or kill overlay is active.

## Refresh Intervals

Adjustable with `+`/`-`:

| Level | Interval |
|-------|----------|
| 0 | 100ms |
| 1 | 250ms |
| 2 | 500ms |
| **3** | **1s (default)** |
| 4 | 2s |
| 5 | 5s |
| 6 | 10s |
