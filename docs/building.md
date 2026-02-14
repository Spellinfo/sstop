# Building from Source

## Prerequisites

- Go 1.21 or later
- Git

## Build

```bash
git clone https://github.com/googlesky/sstop.git
cd sstop
go build -o sstop .
```

## Cross-Compile

```bash
# Linux amd64
GOOS=linux GOARCH=amd64 go build -o sstop-linux-amd64 .

# Linux arm64
GOOS=linux GOARCH=arm64 go build -o sstop-linux-arm64 .

# macOS Apple Silicon
GOOS=darwin GOARCH=arm64 go build -o sstop-darwin-arm64 .

# macOS Intel
GOOS=darwin GOARCH=amd64 go build -o sstop-darwin-amd64 .
```

## Install

```bash
go install github.com/googlesky/sstop@latest
```

The binary will be placed in `$GOPATH/bin/` (or `$HOME/go/bin/` by default).

## Run Tests

```bash
go test ./...
```

## Build Tags

The project uses build tags for platform-specific code:

- `linux` — Netlink, /proc, AF_PACKET support
- `darwin` — netstat/lsof-based collection

No manual build tags are needed; Go selects the correct files automatically based on `GOOS`.

## Dependencies

| Package | Purpose |
|---------|---------|
| `charmbracelet/bubbletea` | TUI framework (Elm architecture) |
| `charmbracelet/bubbles` | Text input widget (search bar) |
| `charmbracelet/lipgloss` | Terminal styling and layout |
| `mdlayher/netlink` | Linux netlink socket communication |

All dependencies are managed via Go modules (`go.mod`).
