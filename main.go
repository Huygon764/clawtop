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
	selStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(brass)).Bold(true)
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(helpC))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(coral))
	failStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(coral)).Bold(true)
	wasteBG     = lipgloss.Color(ink)
)

type screen int

const (
	screenMain screen = iota
	screenDetail
	screenSessions
)

type model struct {
	sessions   []Session
	idx        int
	mainSel    int
	detailSel  int
	pickSel    int
	mainOff    int
	detailOff  int
	pickOff    int
	anchor     int
	stepAnchor int
	showHB     bool
	screen     screen
	width      int
	height     int
	err        error
	ready      bool
}

type loadedMsg struct {
	sessions []Session
	err      error
}

type tickMsg struct{}

func main() {
	p := tea.NewProgram(model{})
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
		m.keepVisible()
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
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.screen != screenMain {
			m.screen = screenMain
			m.keepVisible()
		}
		return m, nil
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "f":
		m.jumpNewest()
	case "h":
		if m.screen == screenMain {
			m.showHB = !m.showHB
			m.restoreMain()
		}
	case "s":
		if m.screen == screenMain {
			m.screen = screenSessions
			m.pickSel = m.idx
			m.keepVisible()
		}
	case "n":
		m.shiftSession(1)
	case "p":
		m.shiftSession(-1)
	case "right", "enter":
		m.open()
	case "left":
		if m.screen != screenMain {
			m.screen = screenMain
			m.keepVisible()
		}
	default:
		switch msg.Code {
		case tea.KeyDown:
			m.move(1)
		case tea.KeyUp:
			m.move(-1)
		case tea.KeyRight, tea.KeyEnter:
			m.open()
		case tea.KeyLeft:
			if m.screen != screenMain {
				m.screen = screenMain
				m.keepVisible()
			}
		}
	}
	return m, nil
}

func (m *model) apply(sessions []Session, err error) {
	prevID := ""
	prevAnchor := m.anchor
	prevStep := m.stepAnchor
	if m.ready && m.idx >= 0 && m.idx < len(m.sessions) {
		prevID = m.sessions[m.idx].ID
	}
	m.sessions = sessions
	m.err = err
	if !m.ready {
		m.idx = len(sessions) - 1
		m.selectNewest()
		m.ready = true
		m.keepVisible()
		return
	}
	m.idx = len(sessions) - 1
	if prevID != "" {
		for i, session := range sessions {
			if session.ID == prevID {
				m.idx = i
				break
			}
		}
	}
	m.anchor = prevAnchor
	m.stepAnchor = prevStep
	m.restoreMain()
	m.restoreDetail()
	m.ready = true
	m.keepVisible()
}

func (m *model) restoreMain() {
	vis := m.visibleReqs()
	if i := requestIndex(vis, m.anchor); i >= 0 {
		m.mainSel = i
		return
	}
	if m.mainSel >= len(vis) {
		m.mainSel = len(vis) - 1
	}
	if m.mainSel < 0 {
		m.mainSel = 0
	}
	if m.mainSel >= 0 && m.mainSel < len(vis) {
		m.anchor = vis[m.mainSel].Seq
	}
}

func (m *model) restoreDetail() {
	req := m.selectedRequest()
	if req == nil {
		m.screen = screenMain
		return
	}
	if i := stepIndex(req.Steps, m.stepAnchor); i >= 0 {
		m.detailSel = i
		return
	}
	if m.detailSel >= len(req.Steps) {
		m.detailSel = len(req.Steps) - 1
	}
	if m.detailSel < 0 {
		m.detailSel = 0
	}
	if m.detailSel >= 0 && m.detailSel < len(req.Steps) {
		m.stepAnchor = req.Steps[m.detailSel].Seq
	}
}

func (m *model) selectNewest() {
	vis := m.visibleReqs()
	m.mainSel = len(vis) - 1
	if m.mainSel >= 0 {
		m.anchor = vis[m.mainSel].Seq
	}
}

func (m *model) jumpNewest() {
	switch m.screen {
	case screenSessions:
		if len(m.sessions) == 0 {
			return
		}
		m.pickSel = len(m.sessions) - 1
	case screenDetail:
		req := m.selectedRequest()
		if req == nil || len(req.Steps) == 0 {
			return
		}
		m.detailSel = len(req.Steps) - 1
		m.stepAnchor = req.Steps[m.detailSel].Seq
	default:
		m.selectNewest()
	}
	m.keepVisible()
}

func (m *model) shiftSession(delta int) {
	if len(m.sessions) == 0 {
		return
	}
	if m.screen == screenSessions {
		m.move(delta)
		return
	}
	next := m.idx + delta
	if next < 0 || next >= len(m.sessions) {
		return
	}
	m.idx = next
	m.screen = screenMain
	m.selectNewest()
	m.keepVisible()
}

func (m *model) move(delta int) {
	switch m.screen {
	case screenSessions:
		m.pickSel = clamp(m.pickSel+delta, 0, len(m.sessions)-1)
	case screenDetail:
		req := m.selectedRequest()
		n := 0
		if req != nil {
			n = len(req.Steps)
		}
		m.detailSel = clamp(m.detailSel+delta, 0, n-1)
		if req != nil && m.detailSel >= 0 && m.detailSel < len(req.Steps) {
			m.stepAnchor = req.Steps[m.detailSel].Seq
		}
	default:
		vis := m.visibleReqs()
		m.mainSel = clamp(m.mainSel+delta, 0, len(vis)-1)
		if m.mainSel >= 0 && m.mainSel < len(vis) {
			m.anchor = vis[m.mainSel].Seq
		}
	}
	m.keepVisible()
}

func (m *model) open() {
	switch m.screen {
	case screenSessions:
		if m.pickSel < 0 || m.pickSel >= len(m.sessions) {
			return
		}
		m.idx = m.pickSel
		m.screen = screenMain
		m.selectNewest()
	case screenMain:
		req := m.selectedRequest()
		if req == nil || len(req.Steps) == 0 {
			return
		}
		m.screen = screenDetail
		m.detailSel = len(req.Steps) - 1
		m.stepAnchor = req.Steps[m.detailSel].Seq
	}
	m.keepVisible()
}

func (m *model) keepVisible() {
	switch m.screen {
	case screenSessions:
		m.pickOff = reveal(m.pickSel, m.pickOff, m.lineBudget(1))
	case screenDetail:
		m.detailOff = reveal(m.detailSel, m.detailOff, m.lineBudget(2))
	default:
		m.mainOff = reveal(m.mainSel, m.mainOff, m.rowBudget())
	}
}

func (m model) rowBudget() int {
	n := (m.height - 2) / 2
	if n < 1 {
		return 1
	}
	return n
}

func (m model) lineBudget(chrome int) int {
	n := m.height - chrome
	if n < 1 {
		return 1
	}
	return n
}

func reveal(sel, offset, visible int) int {
	if sel < 0 {
		return 0
	}
	if sel < offset {
		return sel
	}
	if sel >= offset+visible {
		return sel - visible + 1
	}
	return offset
}

func clamp(n, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func requestIndex(reqs []Request, seq int) int {
	for i, req := range reqs {
		if req.Seq == seq {
			return i
		}
	}
	return -1
}

func stepIndex(steps []Step, seq int) int {
	for i, step := range steps {
		if step.Seq == seq {
			return i
		}
	}
	return -1
}

func (m model) current() *Session {
	if m.idx < 0 || m.idx >= len(m.sessions) {
		return nil
	}
	return &m.sessions[m.idx]
}

func (m model) visibleReqs() []Request {
	session := m.current()
	if session == nil {
		return nil
	}
	return session.visible(m.showHB)
}

func (m model) selectedRequest() *Request {
	vis := m.visibleReqs()
	if m.mainSel < 0 || m.mainSel >= len(vis) {
		return nil
	}
	return &vis[m.mainSel]
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
	switch m.screen {
	case screenSessions:
		return m.renderSessions()
	case screenDetail:
		return m.renderDetail()
	default:
		return m.renderMain()
	}
}

func (m model) renderMain() string {
	session := m.current()
	lines := []string{m.header(session)}
	budget := m.rowBudget()
	shown := 0
	vis := m.visibleReqs()
	if len(vis) == 0 {
		msg := "no user requests"
		if !m.ready {
			msg = "reading transcripts"
		}
		lines = append(lines, metaStyle.Render(msg), "")
		shown = 1
	} else {
		end := m.mainOff + budget
		if end > len(vis) {
			end = len(vis)
		}
		start := m.mainOff
		if start < 0 {
			start = 0
		}
		for i := start; i < end; i++ {
			lines = append(lines, m.requestLines(vis[i], i == m.mainSel)...)
			shown++
		}
	}
	for shown < budget {
		lines = append(lines, "", "")
		shown++
	}
	lines = append(lines, m.footer())
	return fitHeight(lines, m.height)
}

func (m model) requestLines(req Request, selected bool) []string {
	mark := " "
	clockS := timeStyle
	iStyle := inStyle
	oStyle := outOKStyle
	textS := userStyle
	replyS := toolStyle
	if selected {
		mark = ">"
		clockS = selStyle
		iStyle = selStyle
		oStyle = selStyle
		textS = selStyle
		replyS = selStyle
	}
	label := clip(req.User, userLabelRunes)
	top := m.summaryTop(mark+formatClock(req.At), formatTokens(req.Input), formatTokens(req.Output), label, req.Failed, clockS, iStyle, oStyle, textS)
	reply := clip(req.Reply, userLabelRunes)
	bot := replyS.Render(strings.Repeat(" ", 28) + reply)
	return []string{clipWidth(top, m.width), clipWidth(bot, m.width)}
}

func (m model) renderDetail() string {
	session := m.current()
	req := m.selectedRequest()
	lines := []string{m.detailHeader(session, req)}
	if req == nil {
		lines = append(lines, metaStyle.Render("request is gone"))
		lines = append(lines, m.footer())
		return fitHeight(lines, m.height)
	}
	lines = append(lines, m.columns())
	budget := m.lineBudget(3)
	start := m.detailOff
	if start < 0 {
		start = 0
	}
	end := start + budget
	if end > len(req.Steps) {
		end = len(req.Steps)
	}
	shown := 0
	for i := start; i < end; i++ {
		lines = append(lines, m.stepLine(req.Steps[i], i == m.detailSel))
		shown++
	}
	for shown < budget {
		lines = append(lines, "")
		shown++
	}
	lines = append(lines, m.footer())
	return fitHeight(lines, m.height)
}

func (m model) stepLine(step Step, selected bool) string {
	if step.IsError {
		style := errStyle
		if selected {
			style = selStyle
		}
		return fitCells(m.width, "", "", "", step.Label, style, style, style, style, false)
	}
	tStyle := timeStyle
	iStyle := inStyle
	oStyle := outOKStyle
	lStyle := userStyle
	if step.Tool != "" && step.Label == step.Tool {
		lStyle = toolStyle
	}
	waste := step.Waste
	if waste {
		oStyle = outBadStyle
		lStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(brass))
		tStyle = tStyle.Background(wasteBG)
		iStyle = iStyle.Background(wasteBG)
		oStyle = oStyle.Background(wasteBG)
		lStyle = lStyle.Background(wasteBG)
	}
	if selected && !waste {
		tStyle = selStyle
		iStyle = selStyle
		oStyle = selStyle
		lStyle = selStyle
	}
	mark := " "
	if selected {
		mark = ">"
	}
	return fitCells(m.width, mark+formatClock(step.At), formatTokens(step.Input), formatTokens(step.Output), step.Label, tStyle, iStyle, oStyle, lStyle, waste)
}

func (m model) renderSessions() string {
	lines := []string{nameStyle.Render("sessions") + metaStyle.Render(fmt.Sprintf("  %d", len(m.sessions)))}
	budget := m.lineBudget(2)
	start := m.pickOff
	if start < 0 {
		start = 0
	}
	end := start + budget
	if end > len(m.sessions) {
		end = len(m.sessions)
	}
	shown := 0
	for i := start; i < end; i++ {
		lines = append(lines, m.sessionLine(m.sessions[i], i, i == m.pickSel))
		shown++
	}
	for shown < budget {
		lines = append(lines, "")
		shown++
	}
	lines = append(lines, m.footer())
	return fitHeight(lines, m.height)
}

func (m model) sessionLine(session Session, index int, selected bool) string {
	mark := " "
	style := userStyle
	if index == m.idx {
		mark = "*"
	}
	if selected {
		mark = ">"
		style = selStyle
	}
	last := ""
	reqs := session.visible(false)
	if n := len(reqs); n > 0 {
		last = clip(reqs[n-1].User, 40)
	}
	when := ""
	if !session.Latest.IsZero() {
		when = formatClock(session.Latest)
	}
	text := fmt.Sprintf("%s %s  %d req  in %s  %s  %s", mark, when, len(reqs), formatTokens(session.Input()), session.DisplayKey(), last)
	return clipWidth(style.Render(text), m.width)
}

func (m model) header(session *Session) string {
	title := nameStyle.Render("clawtop")
	if session == nil {
		if m.err != nil {
			return title + errStyle.Render("  "+m.err.Error())
		}
		return title
	}
	pos := fmt.Sprintf("  %d/%d", m.idx+1, len(m.sessions))
	line := title +
		metaStyle.Render(pos) +
		metaStyle.Render("  "+session.DisplayKey()) +
		inStyle.Render("  in "+formatTokens(session.Input())) +
		wasteStyle.Render("  waste "+formatTokens(session.Waste()))
	if m.showHB {
		line += metaStyle.Render("  hb")
	}
	return clipWidth(line, m.width)
}

func (m model) detailHeader(session *Session, req *Request) string {
	name := "detail"
	if session != nil {
		name = session.DisplayKey()
	}
	user := ""
	if req != nil {
		user = clip(req.User, 40)
	}
	return clipWidth(nameStyle.Render(name)+metaStyle.Render("  detail  ")+userStyle.Render(user), m.width)
}

func (m model) columns() string {
	return fitCells(m.width, "time", "in", "out", "label", headStyle, headStyle, headStyle, headStyle, false)
}

func (m model) summaryTop(clock, in, out, label string, failed bool, clockS, inS, outS, textS lipgloss.Style) string {
	const timeW, numW = 12, 8
	labelW := m.width - timeW - numW - numW
	if labelW < 1 {
		labelW = 1
	}
	labelCell := textS.Render(" " + label)
	if failed {
		labelCell = lipgloss.JoinHorizontal(lipgloss.Left, labelCell, failStyle.Render(" fail"))
	}
	labelCell = lipgloss.NewStyle().Width(labelW).MaxWidth(labelW).Render(labelCell)
	return lipgloss.JoinHorizontal(lipgloss.Left,
		clockS.Width(timeW).Render(clock),
		inS.Width(numW).Align(lipgloss.Right).Render(in),
		outS.Width(numW).Align(lipgloss.Right).Render(out),
		labelCell,
	)
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
	if m.err != nil && m.screen == screenMain {
		return errStyle.MaxWidth(m.width).Render(m.err.Error())
	}
	help := "j/k move    f newest    enter detail    h heartbeats    s sessions    n/p    q quit"
	switch m.screen {
	case screenDetail:
		help = "j/k move    f newest    esc back    q quit"
	case screenSessions:
		help = "j/k move    enter open    esc back    q quit"
	}
	return helpStyle.MaxWidth(m.width).Render(help)
}

func fitHeight(lines []string, height int) string {
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func clipWidth(s string, width int) string {
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}
