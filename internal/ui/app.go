package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/googlesky/sstop/internal/model"
)

// ViewMode tracks which view is active.
type ViewMode int

const (
	ViewProcessTable ViewMode = iota
	ViewProcessDetail
	ViewRemoteHosts
	ViewListenPorts
)

// SnapshotMsg delivers a new snapshot to the UI.
type SnapshotMsg model.Snapshot

// IntervalSetter is implemented by the collector to allow dynamic interval changes.
type IntervalSetter interface {
	SetInterval(d time.Duration)
}

// Preset refresh interval steps (sorted fastest→slowest)
var intervalPresets = []time.Duration{
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
}

// Model is the root bubbletea model for sstop.
type Model struct {
	width  int
	height int

	mode     ViewMode
	snapshot model.Snapshot

	table       processTable
	detail      processDetail
	remoteHosts remoteHostsView
	listenPorts listenPortsView

	// Help overlay
	showHelp bool

	// Kill process overlay
	kill killOverlay

	// Search
	searching   bool
	searchInput textinput.Model

	// Pause
	paused         bool
	pausedSnapshot model.Snapshot

	// Interface selection
	ifaceNames  []string // available interface names
	ifaceIdx    int      // -1 = all, 0..N = specific interface
	activeIface string   // "" = all

	// Refresh interval
	intervalIdx int            // index into intervalPresets
	collector   IntervalSetter // callback to change collector interval

	// Snapshot channel (for tea.Cmd polling)
	snapCh <-chan model.Snapshot
}

// New creates a new UI model.
func New(snapCh <-chan model.Snapshot) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 64

	return Model{
		table:       newProcessTable(),
		remoteHosts: newRemoteHostsView(),
		listenPorts: newListenPortsView(),
		searchInput: ti,
		snapCh:      snapCh,
		ifaceIdx:    -1, // all interfaces
		intervalIdx: 3,  // default 1s (index into intervalPresets)
	}
}

// SetCollector sets the collector reference for dynamic interval changes.
func (m *Model) SetCollector(c IntervalSetter) {
	m.collector = c
}

// SetDefaultInterface sets the initial active interface (auto-detected).
func (m *Model) SetDefaultInterface(name string) {
	if name != "" {
		m.activeIface = name
		m.ifaceIdx = 0 // will be corrected when interface list arrives
	}
}

// WaitForSnapshot returns a tea.Cmd that waits for the next snapshot.
// Returns tea.Quit if the channel is closed (collector stopped).
func WaitForSnapshot(ch <-chan model.Snapshot) tea.Cmd {
	return func() tea.Msg {
		snap, ok := <-ch
		if !ok {
			return tea.Quit()
		}
		return SnapshotMsg(snap)
	}
}

func (m Model) Init() tea.Cmd {
	return WaitForSnapshot(m.snapCh)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case SnapshotMsg:
		snap := model.Snapshot(msg)
		snap.ActiveIface = m.activeIface

		// Update available interfaces list
		m.updateIfaceList(snap.Interfaces)

		if !m.paused {
			m.snapshot = snap
			m.table.update(m.snapshot.Processes)

			// If in detail view, check process still exists
			if m.mode == ViewProcessDetail {
				found := false
				for _, p := range m.snapshot.Processes {
					if p.PID == m.detail.pid {
						found = true
						break
					}
				}
				if !found {
					m.mode = ViewProcessTable
				}
			}
		}

		return m, WaitForSnapshot(m.snapCh)

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)
	}

	return m, nil
}

func (m *Model) updateIfaceList(ifaces []model.InterfaceStats) {
	names := make([]string, len(ifaces))
	for i, iface := range ifaces {
		names[i] = iface.Name
	}
	m.ifaceNames = names

	// Sync ifaceIdx with activeIface name
	if m.activeIface != "" {
		m.ifaceIdx = -1
		for i, name := range names {
			if name == m.activeIface {
				m.ifaceIdx = i
				break
			}
		}
		// If activeIface not found in list, reset to all
		if m.ifaceIdx < 0 {
			m.activeIface = ""
		}
	}
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Kill overlay — intercept all keys when active
	if m.kill.active {
		if m.kill.showResult {
			// Any key closes the result
			m.kill.close()
			return m, nil
		}
		action := matchKey(msg)
		switch action {
		case keyUp:
			m.kill.moveUp()
		case keyDown:
			m.kill.moveDown()
		case keyEnter:
			m.kill.sendSignal()
		case keyEsc:
			m.kill.close()
		}
		return m, nil
	}

	// Help overlay — ? toggles, any key closes
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}

	// If searching, handle search input
	if m.searching {
		switch msg.String() {
		case "enter", "esc":
			m.searching = false
			if msg.String() == "esc" {
				m.searchInput.SetValue("")
				m.table.filter = ""
				m.table.applyFilterAndSort()
			} else {
				m.table.filter = m.searchInput.Value()
				m.table.applyFilterAndSort()
			}
			m.searchInput.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			m.table.filter = m.searchInput.Value()
			m.table.applyFilterAndSort()
			return m, cmd
		}
	}

	action := matchKey(msg)

	// Global actions (work in any mode)
	switch action {
	case keyHelp:
		m.showHelp = !m.showHelp
		return m, nil
	case keyPause:
		m.paused = !m.paused
		if m.paused {
			m.pausedSnapshot = m.snapshot
		}
		return m, nil
	case keyNextIface:
		m.cycleInterface()
		return m, nil
	case keyIntervalUp:
		m.changeInterval(-1) // faster = lower index
		return m, nil
	case keyIntervalDown:
		m.changeInterval(1) // slower = higher index
		return m, nil
	}

	switch m.mode {
	case ViewProcessTable:
		switch action {
		case keyQuit:
			return m, tea.Quit
		case keyUp:
			m.table.moveUp()
		case keyDown:
			m.table.moveDown()
		case keyPageUp:
			m.table.pageUp()
		case keyPageDown:
			m.table.pageDown()
		case keyHome:
			m.table.goHome()
		case keyEnd:
			m.table.goEnd()
		case keyEnter:
			if sel := m.table.selected(); sel != nil {
				m.mode = ViewProcessDetail
				m.detail = newProcessDetail(sel.PID)
			}
		case keySortNext:
			m.table.nextSort()
		case keySearch:
			m.searching = true
			m.searchInput.Focus()
			return m, m.searchInput.Cursor.BlinkCmd()
		case keyRemoteHosts:
			m.mode = ViewRemoteHosts
			m.remoteHosts.cursor = 0
			m.remoteHosts.offset = 0
		case keyListenPorts:
			m.mode = ViewListenPorts
			m.listenPorts.cursor = 0
			m.listenPorts.offset = 0
		case keyKillProcess:
			if sel := m.table.selected(); sel != nil {
				m.kill.open(sel.PID, sel.Name)
			}
		}

	case ViewProcessDetail:
		switch action {
		case keyQuit:
			return m, tea.Quit
		case keyEsc:
			m.mode = ViewProcessTable
		case keyUp:
			m.detail.moveUp()
		case keyDown:
			proc := m.findProcess(m.detail.pid)
			if proc != nil {
				m.detail.moveDown(len(proc.Connections) - 1)
			}
		case keyPageUp:
			m.detail.pageUp()
		case keyPageDown:
			proc := m.findProcess(m.detail.pid)
			if proc != nil {
				m.detail.pageDown(len(proc.Connections) - 1)
			}
		case keyToggleDNS:
			m.detail.toggleDNS()
		case keyKillProcess:
			proc := m.findProcess(m.detail.pid)
			if proc != nil {
				m.kill.open(proc.PID, proc.Name)
			}
		}

	case ViewRemoteHosts:
		switch action {
		case keyQuit:
			return m, tea.Quit
		case keyEsc:
			m.mode = ViewProcessTable
		case keyUp:
			m.remoteHosts.moveUp()
		case keyDown:
			m.remoteHosts.moveDown(len(m.snapshot.RemoteHosts) - 1)
		case keyPageUp:
			m.remoteHosts.pageUp()
		case keyPageDown:
			m.remoteHosts.pageDown(len(m.snapshot.RemoteHosts) - 1)
		case keyHome:
			m.remoteHosts.goHome()
		case keyEnd:
			m.remoteHosts.goEnd(len(m.snapshot.RemoteHosts) - 1)
		}

	case ViewListenPorts:
		switch action {
		case keyQuit:
			return m, tea.Quit
		case keyEsc:
			m.mode = ViewProcessTable
		case keyUp:
			m.listenPorts.moveUp()
		case keyDown:
			m.listenPorts.moveDown(len(m.snapshot.ListenPorts) - 1)
		case keyPageUp:
			m.listenPorts.pageUp()
		case keyPageDown:
			m.listenPorts.pageDown(len(m.snapshot.ListenPorts) - 1)
		case keyHome:
			m.listenPorts.goHome()
		case keyEnd:
			m.listenPorts.goEnd(len(m.snapshot.ListenPorts) - 1)
		}
	}

	return m, nil
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.kill.active || m.showHelp {
		return m, nil
	}

	switch msg.Action {
	case tea.MouseActionPress:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			switch m.mode {
			case ViewProcessTable:
				m.table.moveUp()
			case ViewProcessDetail:
				m.detail.moveUp()
			case ViewRemoteHosts:
				m.remoteHosts.moveUp()
			case ViewListenPorts:
				m.listenPorts.moveUp()
			}
		case tea.MouseButtonWheelDown:
			switch m.mode {
			case ViewProcessTable:
				m.table.moveDown()
			case ViewProcessDetail:
				proc := m.findProcess(m.detail.pid)
				if proc != nil {
					m.detail.moveDown(len(proc.Connections) - 1)
				}
			case ViewRemoteHosts:
				m.remoteHosts.moveDown(len(m.snapshot.RemoteHosts) - 1)
			case ViewListenPorts:
				m.listenPorts.moveDown(len(m.snapshot.ListenPorts) - 1)
			}
		case tea.MouseButtonLeft:
			return m.handleMouseClick(msg)
		}
	}

	return m, nil
}

func (m Model) handleMouseClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Calculate header height to determine content area
	snap := m.snapshot
	header := renderHeader(snap, m.width, m.paused, m.activeIface)
	headerHeight := strings.Count(header, "\n") + 1

	contentY := msg.Y - headerHeight

	switch m.mode {
	case ViewProcessTable:
		if contentY < 0 {
			return m, nil
		}
		// row 0 is header, row 1+ are data
		rowIdx := contentY - 1 + m.table.offset
		if rowIdx >= 0 && rowIdx < len(m.table.filtered) {
			if rowIdx == m.table.cursor {
				// Double-click effect: enter detail
				if sel := m.table.selected(); sel != nil {
					m.mode = ViewProcessDetail
					m.detail = newProcessDetail(sel.PID)
				}
			} else {
				m.table.cursor = rowIdx
			}
		}
	case ViewProcessDetail:
		// Click on connection rows (approximate positioning)
		if contentY >= 0 {
			proc := m.findProcess(m.detail.pid)
			if proc != nil && len(proc.Connections) > 0 {
				connRowIdx := contentY + m.detail.offset
				if connRowIdx >= 0 && connRowIdx < len(proc.Connections) {
					m.detail.cursor = connRowIdx
				}
			}
		}
	case ViewRemoteHosts:
		if contentY < 0 {
			return m, nil
		}
		rowIdx := contentY - 1 + m.remoteHosts.offset
		if rowIdx >= 0 && rowIdx < len(m.snapshot.RemoteHosts) {
			m.remoteHosts.cursor = rowIdx
		}
	case ViewListenPorts:
		if contentY < 0 {
			return m, nil
		}
		rowIdx := contentY - 2 + m.listenPorts.offset // -2 for title + header
		if rowIdx >= 0 && rowIdx < len(m.snapshot.ListenPorts) {
			m.listenPorts.cursor = rowIdx
		}
	}

	return m, nil
}

func (m *Model) changeInterval(delta int) {
	newIdx := m.intervalIdx + delta
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx >= len(intervalPresets) {
		newIdx = len(intervalPresets) - 1
	}
	if newIdx == m.intervalIdx {
		return
	}
	m.intervalIdx = newIdx
	if m.collector != nil {
		m.collector.SetInterval(intervalPresets[m.intervalIdx])
	}
}

func (m *Model) cycleInterface() {
	// Cycle: all → iface0 → iface1 → ... → all
	if len(m.ifaceNames) == 0 {
		return
	}
	m.ifaceIdx++
	if m.ifaceIdx >= len(m.ifaceNames) {
		m.ifaceIdx = -1
	}

	if m.ifaceIdx < 0 {
		m.activeIface = ""
	} else {
		m.activeIface = m.ifaceNames[m.ifaceIdx]
	}
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	snap := m.snapshot

	// Header: 2-4 lines
	header := renderHeader(snap, m.width, m.paused, m.activeIface)
	headerHeight := strings.Count(header, "\n") + 1

	// Footer: 1 line
	footer := m.renderFooter()
	footerHeight := 1

	// Content area
	contentHeight := m.height - headerHeight - footerHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	var content string
	switch m.mode {
	case ViewProcessTable:
		content = m.table.render(m.width, contentHeight)
	case ViewProcessDetail:
		proc := m.findProcess(m.detail.pid)
		content = m.detail.render(proc, m.width, contentHeight)
	case ViewRemoteHosts:
		content = m.remoteHosts.render(m.snapshot.RemoteHosts, m.width, contentHeight)
	case ViewListenPorts:
		content = m.listenPorts.render(m.snapshot.ListenPorts, m.width, contentHeight)
	}

	// Pad content to fill available height so footer stays at bottom
	contentLines := strings.Count(content, "\n") + 1
	if contentLines < contentHeight {
		content += strings.Repeat("\n", contentHeight-contentLines)
	}

	// Search bar (replaces footer when active)
	if m.searching {
		footer = styleSearchPrompt.Render("Filter: ") + m.searchInput.View()
	}

	result := lipgloss.JoinVertical(lipgloss.Left,
		header,
		content,
		footer,
	)

	// Overlays on top of everything
	if m.kill.active {
		result = m.kill.render(m.width, m.height)
	} else if m.showHelp {
		result = renderHelp(m.width, m.height)
	}

	return result
}

func (m Model) renderFooter() string {
	var parts []string

	// Simplified footer with help hint
	parts = append(parts,
		styleFooterKey.Render("?")+styleFooter.Render(" help"),
		styleFooterKey.Render("/")+styleFooter.Render(" filter"),
		styleFooterKey.Render("q")+styleFooter.Render(" quit"),
	)

	if m.table.filter != "" && !m.searching {
		parts = append(parts,
			styleSearchPrompt.Render("filter:")+styleFooter.Render(m.table.filter),
		)
	}

	if m.paused {
		parts = append(parts, stylePaused.Render("PAUSED"))
	}

	// Refresh interval indicator
	interval := intervalPresets[m.intervalIdx]
	intervalStr := formatInterval(interval)
	parts = append(parts,
		styleFooterKey.Render("+/-")+styleFooter.Render(" ")+
			styleHeaderValue.Render(intervalStr),
	)

	return "  " + strings.Join(parts, "  ")
}

func formatInterval(d time.Duration) string {
	ms := d.Milliseconds()
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	s := float64(ms) / 1000.0
	if s == float64(int(s)) {
		return fmt.Sprintf("%ds", int(s))
	}
	return fmt.Sprintf("%.1fs", s)
}

func (m Model) findProcess(pid uint32) *model.ProcessSummary {
	for i := range m.snapshot.Processes {
		if m.snapshot.Processes[i].PID == pid {
			return &m.snapshot.Processes[i]
		}
	}
	return nil
}
