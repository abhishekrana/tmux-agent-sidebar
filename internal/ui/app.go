package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abhishekrana/tmux-agent-sidebar/internal/model"
)

const spinnerInterval = 200 * time.Millisecond

type tickMsg time.Time

// App is the Bubble Tea model for the sidebar.
//
// In mockup mode the snapshot is static fake data and Enter just flashes
// what it would do; the real snapshot loop arrives in milestone 3.
type App struct {
	theme  Theme
	snap   model.Snapshot
	blocks []block
	cursor int // index into blocks; kept on a selectable block when possible
	frame  int
	width  int
	height int
	flash  string
	mockup bool
}

// NewMockup builds the sidebar with representative fake data for visual
// approval of the layout/palette before any real plumbing exists.
func NewMockup(theme Theme) App {
	now := time.Now()
	// Sessions in alphabetical order, as the real snapshot will deliver.
	snap := model.Snapshot{Sessions: []model.Session{
		{Name: "api-server", Current: true, Agents: []model.Agent{
			{PaneID: "%1", WindowIndex: 1, WindowName: "claude", Branch: "feat/rate-limit-middleware-rollout", State: model.StateWorking, Since: now.Add(-2 * time.Minute), Subagents: 2},
			{PaneID: "%2", WindowIndex: 3, WindowName: "claude", Branch: "fix/csrf-rotation", State: model.StatePermission, Since: now.Add(-40 * time.Second)},
		}},
		{Name: "blog", Agents: []model.Agent{
			{PaneID: "%7", WindowIndex: 2, WindowName: "claude", Branch: "draft/tmux-agents-post", State: model.StateDone, Since: now.Add(-12 * time.Minute)},
			{PaneID: "%8", WindowIndex: 4, WindowName: "claude", Branch: "main", State: model.StateDone, Seen: true, Since: now.Add(-33 * time.Minute)},
		}},
		{Name: "dotfiles", Agents: []model.Agent{
			{PaneID: "%5", WindowIndex: 1, WindowName: "claude", Branch: "main", State: model.StateQuestion, Since: now.Add(-4 * time.Minute)},
		}},
		{Name: "scratch"},
	}}
	app := App{
		theme:  theme,
		snap:   snap,
		mockup: true,
	}
	app.rebuild()
	return app
}

func (a *App) rebuild() {
	a.blocks = buildBlocks(a.snap)
	if !a.blockSelectable(a.cursor) {
		a.moveCursor(1)
	}
}

func (a App) blockSelectable(i int) bool {
	return i >= 0 && i < len(a.blocks) && a.blocks[i].selectable()
}

// moveCursor advances to the next selectable block in direction dir,
// starting from the current position (or the top when nothing selected).
func (a *App) moveCursor(dir int) {
	if len(a.blocks) == 0 {
		a.cursor = 0
		return
	}
	i := a.cursor
	for step := 0; step < len(a.blocks); step++ {
		i += dir
		if i < 0 || i >= len(a.blocks) {
			return // stay put at the edge
		}
		if a.blocks[i].selectable() {
			a.cursor = i
			return
		}
	}
}

func (a App) Init() tea.Cmd { return tick() }

func tick() tea.Cmd {
	return tea.Tick(spinnerInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		a.frame++
		return a, tick()
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a, nil
	case tea.KeyMsg:
		return a.handleKey(msg)
	case tea.MouseMsg:
		return a.handleMouse(msg)
	}
	return a, nil
}

// layout describes the body area in display lines: which block owns each
// line and the scroll window that keeps the cursor block fully visible.
type layout struct {
	owners []int // line index -> block index
	start  int   // first visible line
	avail  int   // visible body lines
}

func (a App) layout() layout {
	l := layout{avail: a.height - 5} // 2 header + 3 footer lines are fixed
	if a.flash != "" {
		l.avail--
	}
	if l.avail < 1 {
		l.avail = 1
	}
	firsts := make([]int, len(a.blocks))
	for i, b := range a.blocks {
		firsts[i] = len(l.owners)
		for n := blockLineCount(b, a.snap); n > 0; n-- {
			l.owners = append(l.owners, i)
		}
	}
	if a.blockSelectable(a.cursor) {
		first := firsts[a.cursor]
		last := first + blockLineCount(a.blocks[a.cursor], a.snap) - 1
		if last >= l.start+l.avail {
			l.start = last - l.avail + 1
		}
		if first < l.start {
			l.start = first
		}
	}
	if l.start+l.avail > len(l.owners) {
		l.start = max(0, len(l.owners)-l.avail)
	}
	return l
}

func (a App) handleMouse(m tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonWheelUp:
		a.moveCursor(-1)
	case m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonWheelDown:
		a.moveCursor(1)
	case m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft:
		l := a.layout()
		idx := l.start + m.Y - 2 // 2 header lines above the body
		if m.Y >= 2 && m.Y < 2+l.avail && idx < len(l.owners) && a.blockSelectable(l.owners[idx]) {
			a.cursor = l.owners[idx]
			return a.activate()
		}
	}
	return a, nil
}

// activate acts on the block under the cursor (Enter or click).
func (a App) activate() (tea.Model, tea.Cmd) {
	if !a.blockSelectable(a.cursor) {
		return a, nil
	}
	b := a.blocks[a.cursor]
	ag := a.snap.Sessions[b.session].Agents[b.agent]
	if a.mockup {
		a.flash = "would jump to " + ag.PaneID
	}
	// Real jump lands in milestone 4.
	return a, nil
}

func (a App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "j", "down":
		a.moveCursor(1)
	case "k", "up":
		a.moveCursor(-1)
	case "g", "home":
		a.cursor = -1
		a.moveCursor(1)
	case "G", "end":
		a.cursor = len(a.blocks)
		a.moveCursor(-1)
	case "enter", " ":
		return a.activate()
	}
	return a, nil
}

func (a App) View() string {
	if a.width == 0 {
		return ""
	}
	now := time.Now()
	// Clamp the name column so icon+name+label+time always fit the width.
	nameW := max(6, min(nameColumn(a.snap), a.width-23))
	r := renderer{theme: a.theme, width: a.width, nameW: nameW}

	var b strings.Builder
	b.WriteString(r.header(a.snap, a.frame) + "\n")
	b.WriteString(r.sep() + "\n")

	var body []string
	for i, blk := range a.blocks {
		sess := a.snap.Sessions[blk.session]
		switch blk.kind {
		case blockSession:
			body = append(body, r.sessionRow(sess))
		case blockAgent:
			body = append(body, r.agentBlock(sess.Agents[blk.agent], i == a.cursor, a.frame, now)...)
		}
	}

	l := a.layout()
	for i := l.start; i < len(body) && i < l.start+l.avail; i++ {
		b.WriteString(body[i] + "\n")
	}
	for i := len(body); i < l.avail; i++ {
		b.WriteString("\n")
	}

	if a.flash != "" {
		b.WriteString(" " + a.flash + "\n")
	}
	b.WriteString(r.footer(a.snap))
	return b.String()
}
