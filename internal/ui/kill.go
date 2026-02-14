package ui

import (
	"fmt"
	"strings"
	"syscall"

	"github.com/charmbracelet/lipgloss"
)

// signalEntry represents a Unix signal option.
type signalEntry struct {
	num  syscall.Signal
	name string
	desc string
}

var signalList = []signalEntry{
	{syscall.SIGTERM, "SIGTERM", "graceful termination"},
	{syscall.SIGKILL, "SIGKILL", "force kill"},
	{syscall.SIGINT, "SIGINT", "interrupt"},
	{syscall.SIGHUP, "SIGHUP", "hangup"},
	{syscall.SIGSTOP, "SIGSTOP", "stop process"},
	{syscall.SIGCONT, "SIGCONT", "continue process"},
	{syscall.SIGUSR1, "SIGUSR1", "user signal 1"},
	{syscall.SIGUSR2, "SIGUSR2", "user signal 2"},
}

// killOverlay manages the kill signal selection state.
type killOverlay struct {
	active      bool
	pid         uint32
	processName string
	cursor      int
	result      string // status message after kill attempt
	showResult  bool
}

func (k *killOverlay) open(pid uint32, name string) {
	k.active = true
	k.pid = pid
	k.processName = name
	k.cursor = 0
	k.result = ""
	k.showResult = false
}

func (k *killOverlay) close() {
	k.active = false
	k.showResult = false
}

func (k *killOverlay) moveUp() {
	if k.cursor > 0 {
		k.cursor--
	}
}

func (k *killOverlay) moveDown() {
	if k.cursor < len(signalList)-1 {
		k.cursor++
	}
}

func (k *killOverlay) sendSignal() {
	if k.cursor < 0 || k.cursor >= len(signalList) {
		k.result = "Error: invalid signal selection"
		k.showResult = true
		return
	}
	sig := signalList[k.cursor]
	err := syscall.Kill(int(k.pid), sig.num)
	if err != nil {
		k.result = fmt.Sprintf("Failed: %v", err)
	} else {
		k.result = fmt.Sprintf("Sent %s to PID %d", sig.name, k.pid)
	}
	k.showResult = true
}

var (
	styleKillBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorRed).
			Background(colorBg).
			Padding(1, 2)

	styleKillTitle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	styleKillSignal = lipgloss.NewStyle().
			Foreground(colorFg)

	styleKillSignalSelected = lipgloss.NewStyle().
				Background(colorSelection).
				Foreground(colorFg).
				Bold(true)

	styleKillNum = lipgloss.NewStyle().
			Foreground(colorYellow).
			Bold(true)

	styleKillDesc = lipgloss.NewStyle().
			Foreground(colorFgDim)

	styleKillResult = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	styleKillResultErr = lipgloss.NewStyle().
				Foreground(colorRed).
				Bold(true)
)

func (k *killOverlay) render(width, height int) string {
	if k.showResult {
		resultStyle := styleKillResult
		if strings.HasPrefix(k.result, "Failed") {
			resultStyle = styleKillResultErr
		}
		content := resultStyle.Render(k.result) + "\n\n" +
			styleDetailLabel.Render("Press any key to close")
		box := styleKillBorder.Render(content)
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
	}

	title := styleKillTitle.Render(fmt.Sprintf("  Kill: %s (PID %d)", k.processName, k.pid))

	var lines []string
	for i, sig := range signalList {
		num := fmt.Sprintf("%2d", sig.num)
		name := fmt.Sprintf("%-10s", sig.name)
		desc := sig.desc

		if i == k.cursor {
			line := styleKillSignalSelected.Render(
				fmt.Sprintf(" ▸ %s  %s  %s ", num, name, desc),
			)
			lines = append(lines, line)
		} else {
			line := lipgloss.JoinHorizontal(lipgloss.Top,
				"   ",
				styleKillNum.Render(num),
				"  ",
				styleKillSignal.Render(name),
				"  ",
				styleKillDesc.Render(desc),
			)
			lines = append(lines, line)
		}
	}

	signalRows := strings.Join(lines, "\n")
	hint := styleDetailLabel.Render("  j/k navigate  enter send  esc cancel")

	content := title + "\n\n" + signalRows + "\n\n" + hint

	box := styleKillBorder.Render(content)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
