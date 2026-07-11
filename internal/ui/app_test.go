package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abhishekrana/tmux-agent-sidebar/internal/model"
)

// fakeRunner records every tmux invocation and replies from a canned map
// keyed on the joined argument string.
type fakeRunner struct {
	calls   [][]string
	replies map[string]string
}

func (f *fakeRunner) Run(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	return f.replies[strings.Join(args, " ")], nil
}

func twoSessionSnap() model.Snapshot {
	return model.Snapshot{Sessions: []model.Session{
		{Name: "alpha-1", Current: true, Agents: []model.Agent{
			{PaneID: "%0", WindowIndex: 1, Branch: "main", State: model.StateIdle},
		}},
		{Name: "alpha-2", Agents: []model.Agent{
			{PaneID: "%6", WindowIndex: 1, Branch: "alpha-2", State: model.StateIdle},
		}},
	}}
}

// testApp builds a live-mode App around a fake runner with the two-session
// snapshot already applied. Block layout: 0=alpha-1 header, 1=%0 agent,
// 2=alpha-2 header, 3=%6 agent.
func testApp(r *fakeRunner) App {
	a := App{runner: r, current: "alpha-1"}
	a.setSnapshot(twoSessionSnap())
	return a
}

func TestSnapMsgAdoptsSharedSelection(t *testing.T) {
	a := testApp(&fakeRunner{})
	if a.cursor != 1 {
		t.Fatalf("initial cursor = %d, want 1 (first agent)", a.cursor)
	}
	m, _ := a.Update(snapMsg{snap: twoSessionSnap(), sel: "%6"})
	a = m.(App)
	if a.cursor != 3 {
		t.Errorf("cursor = %d after adopting %%6, want 3", a.cursor)
	}
	if a.lastSel != "%6" {
		t.Errorf("lastSel = %q, want %%6", a.lastSel)
	}
}

func TestSnapMsgUnchangedSelectionKeepsLocalCursor(t *testing.T) {
	a := testApp(&fakeRunner{})
	a.lastSel = "%6" // already adopted earlier
	a.cursor = 3
	a.moveCursor(-1) // k: onto alpha-2's header (now selectable)
	a.moveCursor(-1) // k again: onto %0's agent block
	if a.cursor != 1 {
		t.Fatalf("moveCursor: cursor = %d, want 1", a.cursor)
	}
	m, _ := a.Update(snapMsg{snap: twoSessionSnap(), sel: "%6"})
	a = m.(App)
	if a.cursor != 1 {
		t.Errorf("unchanged shared selection stomped local cursor: got %d, want 1", a.cursor)
	}
}

func TestSnapMsgUnknownPaneKeepsCursor(t *testing.T) {
	a := testApp(&fakeRunner{})
	m, _ := a.Update(snapMsg{snap: twoSessionSnap(), sel: "%99"})
	a = m.(App)
	if a.cursor != 1 {
		t.Errorf("unknown pane moved cursor: got %d, want 1", a.cursor)
	}
}

func TestSignalSnapAdoptsSelectionImmediately(t *testing.T) {
	a := testApp(&fakeRunner{})
	m, cmd := a.Update(snapMsg{snap: twoSessionSnap(), sel: "%6", signal: true})
	a = m.(App)
	if a.cursor != 3 {
		t.Errorf("cursor = %d after signal refresh, want 3", a.cursor)
	}
	if cmd == nil {
		t.Error("signal snapMsg must re-arm waitRefresh, got nil cmd")
	}
}

// A session switch made outside the sidebar moves the highlight to the
// newly attached session's focused agent.
func TestAttachedSessionChangeMovesHighlight(t *testing.T) {
	a := testApp(&fakeRunner{})
	snap := twoSessionSnap()
	snap.Sessions[0].Attached = true
	m, _ := a.Update(snapMsg{snap: snap, sel: ""})
	a = m.(App)
	if a.cursor != 1 {
		t.Fatalf("cursor = %d after alpha-1 attach, want 1", a.cursor)
	}

	snap = twoSessionSnap()
	snap.Sessions[1].Attached = true
	snap.Sessions[1].Agents[0].Focused = true
	m, _ = a.Update(snapMsg{snap: snap, sel: ""})
	a = m.(App)
	if a.cursor != 3 {
		t.Errorf("cursor = %d after switch to alpha-2, want 3", a.cursor)
	}

	// No change: local j/k position must survive the next tick. Two k's
	// step past alpha-2's now-selectable header onto %0's agent block.
	a.moveCursor(-1)
	a.moveCursor(-1)
	m, _ = a.Update(snapMsg{snap: snap, sel: ""})
	a = m.(App)
	if a.cursor != 1 {
		t.Errorf("cursor = %d after unchanged tick, want 1", a.cursor)
	}
}

// Switching to a session before its agent exists must still hand the
// agent the highlight once it starts.
func TestAgentStartedAfterSwitchGetsHighlight(t *testing.T) {
	a := testApp(&fakeRunner{})
	snap := twoSessionSnap()
	snap.Sessions[1].Attached = true
	snap.Sessions[1].Agents = nil // switched here before claude started
	m, _ := a.Update(snapMsg{snap: snap, sel: ""})
	a = m.(App)
	if a.cursor != 1 {
		t.Fatalf("cursor = %d while alpha-2 has no agents, want 1", a.cursor)
	}

	snap = twoSessionSnap()
	snap.Sessions[1].Attached = true
	m, _ = a.Update(snapMsg{snap: snap, sel: ""})
	a = m.(App)
	if a.cursor != 3 {
		t.Errorf("cursor = %d after agent started in attached session, want 3", a.cursor)
	}
}

func TestActivatePublishesSelectionAndSignals(t *testing.T) {
	r := &fakeRunner{}
	a := testApp(r)
	a.cursor = 3 // alpha-2's agent
	m, _ := a.activate()
	a = m.(App)

	if a.lastSel != "%6" {
		t.Errorf("lastSel = %q, want %%6 (own write must not be re-adopted)", a.lastSel)
	}
	if len(r.calls) == 0 {
		t.Fatal("activate issued no tmux command")
	}
	jump := strings.Join(r.calls[len(r.calls)-1], " ")
	for _, want := range []string{
		"switch-client",
		"-t alpha-2",
		"select-window -t alpha-2:1",
		"select-pane -t %6",
		"set-option -g @sidebar_selected %6",
		"wait-for -S " + refreshChannel,
	} {
		if !strings.Contains(jump, want) {
			t.Errorf("jump command missing %q:\n%s", want, jump)
		}
	}
}

// Terminals eat the press of a focusing click but deliver the release,
// so the jump must fire on release and ignore the press.
func TestClickJumpsOnReleaseNotPress(t *testing.T) {
	r := &fakeRunner{}
	a := testApp(r)
	a.width, a.height = 30, 20

	// Body rows: 0-1 alpha-1 header, 2-3 agent %0, 4-5 alpha-2 header,
	// 6-7 agent %6; screen y = body row + 2 header lines, so %6 is at y=8.
	press := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: 8}
	m, _ := a.Update(press)
	a = m.(App)
	if len(r.calls) != 0 {
		t.Fatalf("press must not jump, got %v", r.calls)
	}

	release := tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Y: 8}
	m, _ = a.Update(release)
	a = m.(App)
	if a.cursor != 3 {
		t.Errorf("cursor = %d after release on %%6's row, want 3", a.cursor)
	}
	if len(r.calls) == 0 || !strings.Contains(strings.Join(r.calls[len(r.calls)-1], " "), "switch-client") {
		t.Errorf("release did not jump: calls %v", r.calls)
	}
}

// The session header is two lines now: a leading spacer then the name.
// Clicking the blank spacer row must still select and switch to that
// session — the enlarged target is the whole point.
func TestClickSessionSpacerLineSwitches(t *testing.T) {
	r := &fakeRunner{}
	a := testApp(r)
	a.width, a.height = 30, 20

	// Body rows: 0-1 alpha-1 header (spacer, name), 2-3 agent %0,
	// 4-5 alpha-2 header. alpha-2's spacer line is body row 4 -> screen y = 6.
	release := tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Y: 6}
	m, _ := a.Update(release)
	a = m.(App)
	if a.cursor != 2 {
		t.Fatalf("cursor = %d after clicking alpha-2's spacer line, want 2 (its header)", a.cursor)
	}
	if len(r.calls) == 0 || !strings.Contains(strings.Join(r.calls[len(r.calls)-1], " "), "-t alpha-2") {
		t.Errorf("spacer-line click did not switch to alpha-2: calls %v", r.calls)
	}
}

func TestSetSnapshotKeepsSelectionByPane(t *testing.T) {
	a := testApp(&fakeRunner{})
	a.cursor = 3
	// New snapshot with an extra agent shifting block indices.
	snap := twoSessionSnap()
	snap.Sessions[0].Agents = append(snap.Sessions[0].Agents,
		model.Agent{PaneID: "%2", WindowIndex: 2, State: model.StateWorking})
	a.setSnapshot(snap)
	b := a.blocks[a.cursor]
	if got := snap.Sessions[b.session].Agents[b.agent].PaneID; got != "%6" {
		t.Errorf("selection drifted to %s after snapshot refresh, want %%6", got)
	}
}

// A selected session header stays anchored by name across a refresh that
// shifts block indices, just as an agent selection anchors by pane.
func TestSetSnapshotKeepsSessionSelectionByName(t *testing.T) {
	a := testApp(&fakeRunner{})
	a.cursor = 2 // alpha-2's header
	// A new agent under alpha-1 shifts every later block index down by one.
	snap := twoSessionSnap()
	snap.Sessions[0].Agents = append(snap.Sessions[0].Agents,
		model.Agent{PaneID: "%2", WindowIndex: 2, State: model.StateWorking})
	a.setSnapshot(snap)
	b := a.blocks[a.cursor]
	if b.kind != blockSession || snap.Sessions[b.session].Name != "alpha-2" {
		t.Errorf("session selection drifted after refresh: block %+v", b)
	}
}

// Activating a session header switches the client to it and, unlike an
// agent jump, leaves window/pane selection alone.
func TestActivateSessionSwitchesClient(t *testing.T) {
	r := &fakeRunner{}
	a := testApp(r)
	a.cursor = 2 // alpha-2's header
	m, _ := a.activate()
	a = m.(App)

	if len(r.calls) == 0 {
		t.Fatal("activate issued no tmux command")
	}
	got := strings.Join(r.calls[len(r.calls)-1], " ")
	for _, want := range []string{"switch-client", "-t alpha-2", "wait-for -S " + refreshChannel} {
		if !strings.Contains(got, want) {
			t.Errorf("switch command missing %q:\n%s", want, got)
		}
	}
	for _, absent := range []string{"select-window", "select-pane", "@sidebar_selected"} {
		if strings.Contains(got, absent) {
			t.Errorf("session switch should not run %q:\n%s", absent, got)
		}
	}
}

// Clicking the current session is a no-op: no client is switched.
func TestActivateCurrentSessionIsNoop(t *testing.T) {
	r := &fakeRunner{}
	a := testApp(r)
	a.cursor = 0 // alpha-1's header (current)
	if _, _ = a.activate(); len(r.calls) != 0 {
		t.Errorf("activating the current session issued %v", r.calls)
	}
}
