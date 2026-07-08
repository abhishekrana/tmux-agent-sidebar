package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abhishekrana/tmux-agent-sidebar/internal/model"
	"github.com/abhishekrana/tmux-agent-sidebar/internal/tmux"
)

const (
	spinnerInterval  = 200 * time.Millisecond
	snapshotInterval = time.Second

	// wait-for channel signalled by jumps; sidebars adopt the shared
	// selection immediately instead of on the next tick.
	refreshChannel = "tmux-agent-sidebar-refresh"
)

type tickMsg time.Time

type snapMsg struct {
	snap   model.Snapshot
	sel    string // global @sidebar_selected at snapshot time
	signal bool   // woken by the wait-for channel, not the 1s tick
}

// App is the Bubble Tea model for the sidebar. In mockup mode the
// snapshot is static fake data and Enter just flashes what it would do.
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

	// live-mode plumbing (nil in mockup mode)
	runner   tmux.Runner
	branches *tmux.BranchCache
	current  string // session the sidebar pane lives in
	debug    string // log file path (@agent-sidebar-debug), "" = off
	lastSel  string // last @sidebar_selected value we adopted
	attached string // attachedKey of the last snapshot
}

// NewLive builds the sidebar against the real tmux server.
func NewLive(theme Theme) App {
	runner := tmux.Exec{}
	debug, _ := runner.Run("show-option", "-gqv", "@agent-sidebar-debug")
	app := App{
		theme:    theme,
		runner:   runner,
		branches: tmux.NewBranchCache(),
		current:  tmux.CurrentSession(runner),
		debug:    strings.TrimSpace(debug),
	}
	app.setSnapshot(tmux.Snapshot(runner, app.branches, app.current))
	app.attached = attachedKey(app.snap)
	// Selection is shared across sidebars via the global @sidebar_selected.
	if sel, err := runner.Run("show-option", "-gqv", "@sidebar_selected"); err == nil {
		app.lastSel = strings.TrimSpace(sel)
		app.selectPane(app.lastSel)
	}
	app.register()
	return app
}

// attachedKey fingerprints the sessions that have a client attached AND
// at least one agent. Ignoring agent-less sessions keeps the transition
// pending: switching to a session before its agent starts changes nothing,
// so the agent still gets the highlight when it appears.
func attachedKey(snap model.Snapshot) string {
	var names []string
	for _, s := range snap.Sessions {
		if s.Attached && len(s.Agents) > 0 {
			names = append(names, s.Name)
		}
	}
	return strings.Join(names, ",")
}

// scriptPath locates one of the plugin's shell scripts relative to the
// running binary (bin/tmux-agent-sidebar -> scripts/<name>).
func scriptPath(name string) string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(filepath.Dir(exe)), "scripts", name)
}

// register stamps this sidebar's session options and follow hook, so a
// sidebar started outside open.sh (tmux-resurrect restore) works fully.
func (a App) register() {
	pane := os.Getenv("TMUX_PANE")
	follow := scriptPath("follow.sh")
	if pane == "" || a.current == "" || follow == "" {
		return
	}
	_, _ = a.runner.Run(
		"set-option", "-t", a.current, "-q", "@sidebar_pane", pane, ";",
		"set-option", "-t", a.current, "-q", "@sidebar_on", "1", ";",
		"set-hook", "-t", a.current, "session-window-changed",
		"run-shell '"+follow+" #{session_name}'", ";",
		// Session switches wake every sidebar so the highlight follows.
		"set-hook", "-g", "client-session-changed",
		"run-shell 'tmux wait-for -S "+refreshChannel+"'",
	)
}

// debugf appends a timestamped line to the debug log when enabled via
// `tmux set -g @agent-sidebar-debug /path/to/log`.
func (a App) debugf(format string, args ...any) {
	if a.debug == "" {
		return
	}
	f, err := os.OpenFile(a.debug, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format("15:04:05.000 ")+format+"\n", args...)
}

// setSnapshot swaps in fresh data, keeping the selection anchored to the
// same pane across refreshes.
func (a *App) setSnapshot(snap model.Snapshot) {
	var selected string
	if a.blockSelectable(a.cursor) {
		b := a.blocks[a.cursor]
		selected = a.snap.Sessions[b.session].Agents[b.agent].PaneID
	}
	a.snap = snap
	a.rebuild()
	if selected == "" {
		return
	}
	for i, b := range a.blocks {
		if b.selectable() && snap.Sessions[b.session].Agents[b.agent].PaneID == selected {
			a.cursor = i
			return
		}
	}
}

func (a App) snapshotTick() tea.Cmd {
	return tea.Tick(snapshotInterval, func(time.Time) tea.Msg {
		return a.gather(false)
	})
}

// waitRefresh blocks until a jump or session switch signals the channel.
// The blocked wait-for child dies with the pane.
func (a App) waitRefresh() tea.Cmd {
	return func() tea.Msg {
		if _, err := a.runner.Run("wait-for", refreshChannel); err != nil {
			return nil // degrade to tick-based adoption
		}
		return a.gather(true)
	}
}

// gather takes a fresh snapshot plus the shared selection.
func (a App) gather(signal bool) snapMsg {
	sel, _ := a.runner.Run("show-option", "-gqv", "@sidebar_selected")
	return snapMsg{
		snap:   tmux.Snapshot(a.runner, a.branches, a.current),
		sel:    strings.TrimSpace(sel),
		signal: signal,
	}
}

// focusNewlyAttached selects the agent of a session newly in the
// attached-with-agents set: a session switch made outside the sidebar,
// or an agent starting in the attached session right after one.
func (a *App) focusNewlyAttached() {
	old := map[string]bool{}
	for n := range strings.SplitSeq(a.attached, ",") {
		old[n] = true
	}
	for _, s := range a.snap.Sessions {
		if !s.Attached || old[s.Name] || len(s.Agents) == 0 {
			continue
		}
		pane := s.Agents[0].PaneID
		for _, ag := range s.Agents {
			if ag.Focused {
				pane = ag.PaneID
				break
			}
		}
		a.selectPane(pane)
		return
	}
}

// selectPane moves the cursor to the block owning pane, if it's listed.
func (a *App) selectPane(pane string) {
	if pane == "" {
		return
	}
	for i, b := range a.blocks {
		if b.selectable() && a.snap.Sessions[b.session].Agents[b.agent].PaneID == pane {
			a.cursor = i
			return
		}
	}
}

// NewMockup builds the sidebar with representative fake data so the
// layout and palette can be previewed in any pane.
func NewMockup(theme Theme) App {
	now := time.Now()
	// Sessions in alphabetical order, as the real snapshot delivers them.
	snap := model.Snapshot{Sessions: []model.Session{
		{Name: "api-server", Current: true, Agents: []model.Agent{
			{PaneID: "%1", WindowIndex: 1, Command: "claude", Branch: "feat/rate-limit-middleware-rollout",
				State: model.StateWorking, Since: now.Add(-2 * time.Minute), Subagents: 2},
			{PaneID: "%2", WindowIndex: 3, Command: "claude", Branch: "fix/csrf-rotation",
				State: model.StatePermission, Since: now.Add(-40 * time.Second)},
		}},
		{Name: "blog", Agents: []model.Agent{
			{PaneID: "%7", WindowIndex: 2, Command: "claude", Branch: "draft/tmux-agents-post",
				State: model.StateDone, Since: now.Add(-12 * time.Minute)},
			{PaneID: "%8", WindowIndex: 4, Command: "claude", Branch: "main",
				State: model.StateDone, Seen: true, Since: now.Add(-33 * time.Minute)},
		}},
		{Name: "dotfiles", Agents: []model.Agent{
			{PaneID: "%5", WindowIndex: 1, Command: "claude", Branch: "main",
				State: model.StateQuestion, Since: now.Add(-4 * time.Minute)},
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

func (a App) Init() tea.Cmd {
	if a.mockup {
		return tick()
	}
	return tea.Batch(tick(), a.snapshotTick(), a.waitRefresh())
}

func tick() tea.Cmd {
	return tea.Tick(spinnerInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		a.frame++
		return a, tick()
	case snapMsg:
		a.setSnapshot(msg.snap)
		key := attachedKey(a.snap)
		switch {
		case msg.sel != a.lastSel: // explicit jump wins
			a.lastSel = msg.sel
			a.selectPane(msg.sel)
		case key != a.attached: // session switch: follow to its agent
			a.focusNewlyAttached()
		}
		a.attached = key
		if msg.signal {
			return a, a.waitRefresh()
		}
		return a, a.snapshotTick()
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
	a.debugf("mouse action=%v button=%v x=%d y=%d cursor=%d", m.Action, m.Button, m.X, m.Y, a.cursor)
	switch {
	case m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonWheelUp:
		a.moveCursor(-1)
	case m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonWheelDown:
		a.moveCursor(1)
	// Jump on release, not press: terminals eat the press of a click
	// that also focuses their window, but always deliver the release.
	case m.Action == tea.MouseActionRelease && m.Button == tea.MouseButtonLeft:
		l := a.layout()
		idx := l.start + m.Y - 2 // 2 header lines above the body
		a.debugf("click start=%d avail=%d idx=%d owners=%d", l.start, l.avail, idx, len(l.owners))
		if m.Y >= 2 && m.Y < 2+l.avail && idx < len(l.owners) && a.blockSelectable(l.owners[idx]) {
			a.cursor = l.owners[idx]
			return a.activate()
		}
	}
	return a, nil
}

// activate jumps to the agent under the cursor (Enter or click).
func (a App) activate() (tea.Model, tea.Cmd) {
	if !a.blockSelectable(a.cursor) {
		return a, nil
	}
	b := a.blocks[a.cursor]
	sess := a.snap.Sessions[b.session]
	ag := sess.Agents[b.agent]
	if a.mockup {
		a.flash = "would jump to " + ag.PaneID
		return a, nil
	}
	// Address the client explicitly: with several clients attached,
	// tmux's "current client" guess can switch the wrong one.
	args := []string{"switch-client"}
	if tty := tmux.ClientFor(a.runner, a.current); tty != "" {
		args = append(args, "-c", tty)
	}
	args = append(args,
		"-t", sess.Name, ";",
		"select-window", "-t", fmt.Sprintf("%s:%d", sess.Name, ag.WindowIndex), ";",
		"select-pane", "-t", ag.PaneID, ";",
		// Publish + signal so every sidebar highlights it immediately.
		"set-option", "-g", "@sidebar_selected", ag.PaneID, ";",
		"wait-for", "-S", refreshChannel,
	)
	a.lastSel = ag.PaneID
	_, err := a.runner.Run(args...)
	a.debugf("jump session=%s window=%d pane=%s args=%v err=%v", sess.Name, ag.WindowIndex, ag.PaneID, args, err)
	if err != nil {
		a.flash = "jump failed: " + err.Error()
	}
	return a, nil
}

func (a App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if !a.mockup {
			// The toggle is global: q hides the sidebar everywhere,
			// same as prefix+e. The script also kills this pane.
			if script := scriptPath("toggle.sh"); script != "" {
				_ = exec.Command("bash", script).Start()
			}
		}
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
