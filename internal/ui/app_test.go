package ui

import (
	"strings"
	"testing"

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
	a.moveCursor(-1) // user pressed k: local movement to %0's block
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

func TestRefreshMsgAdoptsSelectionImmediately(t *testing.T) {
	a := testApp(&fakeRunner{})
	m, cmd := a.Update(refreshMsg("%6"))
	a = m.(App)
	if a.cursor != 3 {
		t.Errorf("cursor = %d after refresh, want 3", a.cursor)
	}
	if cmd == nil {
		t.Error("refreshMsg must re-arm waitRefresh, got nil cmd")
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
