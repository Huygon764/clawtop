package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	brass = "#e0b15a"
	teal  = "#6ec8c4"
	moss  = "#8fbf7a"
	coral = "#e07a5f"
	steel = "#7ea0c4"
	sand  = "#f0d7a8"
	ink   = "#3c2a24"
	helpC = "#d2b48c"
)

var (
	nameStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(brass)).Bold(true)
	metaStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(sand))
	inStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(teal))
	outOKStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(moss))
	outBadStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(coral)).Bold(true)
	wasteStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(coral)).Bold(true)
	timeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(steel))
	userStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(sand))
	toolStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(steel))
	headStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(brass))
	liveStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(moss)).Bold(true)
	holdStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(brass)).Bold(true)
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(helpC))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(coral))
	wasteBG     = lipgloss.Color(ink)
)

type model struct {
	sessions []Session
	idx      int
	follow   bool
	offset   int
	width    int
	height   int
	err      error
	ready    bool
}

type loadedMsg struct {
	sessions []Session
	err      error
}

type tickMsg struct{}

func main() {
	p := tea.NewProgram(model{follow: true})
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "clawtop: %v\n", err)
		os.Exit(1)
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tea.RequestWindowSize, reloadCmd())
}

func reloadCmd() tea.Cmd {
	return func() tea.Msg {
		sessions, err := Load()
		return loadedMsg{sessions: sessions, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.pin()
		return m, nil
	case loadedMsg:
		m.apply(msg.sessions, msg.err)
		return m, tickCmd()
	case tickMsg:
		return m, reloadCmd()
	case tea.KeyPressMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func (m model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	case "j", "down":
		m.scroll(1)
	case "k", "up":
		m.scroll(-1)
	case "f":
		m.follow = true
		m.pin()
	case "n":
		m.shiftSession(1)
	case "p":
		m.shiftSession(-1)
	default:
		switch msg.Code {
		case tea.KeyDown:
			m.scroll(1)
		case tea.KeyUp:
			m.scroll(-1)
		}
	}
	return m, nil
}

func (m *model) apply(sessions []Session, err error) {
	prev := ""
	if m.ready && m.idx >= 0 && m.idx < len(m.sessions) {
		prev = m.sessions[m.idx].ID
	}
	m.sessions = sessions
	m.err = err
	m.idx = len(sessions) - 1
	if prev != "" {
		for i, session := range sessions {
			if session.ID == prev {
				m.idx = i
				break
			}
		}
	}
	m.ready = true
	m.pin()
}

func (m *model) shiftSession(delta int) {
	if len(m.sessions) == 0 {
		return
	}
	next := m.idx + delta
	if next < 0 || next >= len(m.sessions) {
		return
	}
	m.idx = next
	m.follow = true
	m.pin()
}

func (m *model) scroll(delta int) {
	vis := m.visible()
	maxOff := m.maxOffset(vis)
	next := m.offset + delta
	if next < 0 {
		next = 0
	}
	if next > maxOff {
		next = maxOff
	}
	m.offset = next
	m.follow = next == maxOff
}

func (m *model) pin() {
	vis := m.visible()
	maxOff := m.maxOffset(vis)
	if m.follow || m.offset > maxOff {
		m.offset = maxOff
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m model) visible() int {
	n := m.height - 3
	if n < 1 {
		return 1
	}
	return n
}

func (m model) maxOffset(visible int) int {
	n := 0
	if session := m.current(); session != nil {
		n = len(session.Calls)
	}
	if n > visible {
		return n - visible
	}
	return 0
}

func (m model) current() *Session {
	if m.idx < 0 || m.idx >= len(m.sessions) {
		return nil
	}
	return &m.sessions[m.idx]
}

func (m model) View() tea.View {
	body := m.render()
	v := tea.NewView(body)
	v.AltScreen = true
	v.WindowTitle = "clawtop"
	return v
}

func (m model) render() string {
	if m.width < 24 || m.height < 4 {
		return nameStyle.Render("clawtop") + "\n" + helpStyle.Render("make the terminal taller")
	}
	session := m.current()
	lines := make([]string, 0, m.height)
	lines = append(lines, m.header(session))
	lines = append(lines, m.columns())
	visible := m.visible()
	shown := 0
	if session == nil {
		msg := "no model calls yet"
		if !m.ready {
			msg = "reading transcripts"
		}
		lines = append(lines, metaStyle.Render(msg))
		shown = 1
	} else {
		end := m.offset + visible
		if end > len(session.Calls) {
			end = len(session.Calls)
		}
		for _, call := range session.Calls[m.offset:end] {
			lines = append(lines, m.row(call))
			shown++
		}
	}
	for shown < visible {
		lines = append(lines, "")
		shown++
	}
	lines = append(lines, m.footer())
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	return strings.Join(lines, "\n")
}

func (m model) header(session *Session) string {
	if session == nil {
		title := nameStyle.Render("clawtop")
		if m.err != nil {
			return title + errStyle.Render("  "+m.err.Error())
		}
		return title
	}
	total, waste := 0, 0
	for _, call := range session.Calls {
		total += call.Input
		if call.Output <= 3 {
			waste += call.Input
		}
	}
	status := liveStyle.Render("  live")
	if !m.follow {
		status = holdStyle.Render("  hold")
	}
	line := nameStyle.Render(session.DisplayKey()) +
		metaStyle.Render(fmt.Sprintf("  %d calls", len(session.Calls))) +
		inStyle.Render("  in "+formatTokens(total)) +
		wasteStyle.Render("  waste "+formatTokens(waste)) +
		status
	return lipgloss.NewStyle().MaxWidth(m.width).Render(line)
}

func (m model) columns() string {
	return fitCells(m.width, "time", "in", "out", "label", headStyle, headStyle, headStyle, headStyle, false)
}

func (m model) row(call Call) string {
	tStyle := timeStyle
	iStyle := inStyle
	oStyle := outOKStyle
	lStyle := userStyle
	if call.Tool {
		lStyle = toolStyle
	}
	waste := call.Output <= 3
	if waste {
		oStyle = outBadStyle
		lStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(brass))
		tStyle = tStyle.Background(wasteBG)
		iStyle = iStyle.Background(wasteBG)
		oStyle = oStyle.Background(wasteBG)
		lStyle = lStyle.Background(wasteBG)
	}
	return fitCells(m.width, formatClock(call.At), formatTokens(call.Input), formatTokens(call.Output), call.Label, tStyle, iStyle, oStyle, lStyle, waste)
}

func fitCells(width int, clock, in, out, label string, clockS, inS, outS, labelS lipgloss.Style, waste bool) string {
	const timeW, numW = 12, 8
	labelW := width - timeW - numW - numW
	if labelW < 1 {
		labelW = 1
	}
	line := lipgloss.JoinHorizontal(lipgloss.Left,
		clockS.Width(timeW).Render(clock),
		inS.Width(numW).Align(lipgloss.Right).Render(in),
		outS.Width(numW).Align(lipgloss.Right).Render(out),
		labelS.Width(labelW).Render(" "+label),
	)
	if waste {
		return lipgloss.NewStyle().Background(wasteBG).Width(width).Render(line)
	}
	return line
}

func (m model) footer() string {
	if m.err != nil {
		return errStyle.MaxWidth(m.width).Render(m.err.Error())
	}
	return helpStyle.MaxWidth(m.width).Render("j/k scroll    f follow    n/p session    q quit")
}
