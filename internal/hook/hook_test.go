package hook

import (
	"strings"
	"testing"
	"time"

	"github.com/abhishekrana/tmux-agent-sidebar/internal/model"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		ev   Event
		want Effect
	}{
		{"session start", Event{Name: "SessionStart"}, Effect{Register: true, State: model.StateIdle}},
		{"prompt", Event{Name: "UserPromptSubmit"}, Effect{Register: true, State: model.StateWorking}},
		{"tool", Event{Name: "PreToolUse"}, Effect{Register: true, State: model.StateWorking}},
		{"permission", Event{Name: "PermissionRequest"}, Effect{State: model.StatePermission}},
		{"notif permission", Event{Name: "Notification", NotificationType: "permission_prompt"}, Effect{State: model.StatePermission}},
		{"notif question", Event{Name: "Notification", NotificationType: "agent_needs_input"}, Effect{State: model.StateQuestion}},
		{"notif elicitation", Event{Name: "Notification", NotificationType: "elicitation_dialog"}, Effect{State: model.StateQuestion}},
		{"notif idle nudge ignored", Event{Name: "Notification", NotificationType: "idle_prompt"}, Effect{}},
		{"notif other ignored", Event{Name: "Notification", NotificationType: "auth_success"}, Effect{}},
		{"stop", Event{Name: "Stop"}, Effect{State: model.StateDone}},
		{"subagent start", Event{Name: "SubagentStart"}, Effect{SubagentDelta: 1}},
		{"subagent stop", Event{Name: "SubagentStop"}, Effect{SubagentDelta: -1}},
		{"session end", Event{Name: "SessionEnd"}, Effect{ClearAll: true}},
		{"unknown ignored", Event{Name: "PostToolBatch"}, Effect{}},
	}
	for _, c := range cases {
		if got := Decide(c.ev); got != c.want {
			t.Errorf("%s: Decide(%+v) = %+v, want %+v", c.name, c.ev, got, c.want)
		}
	}
}

// fakeRunner records tmux invocations and serves canned option reads.
type fakeRunner struct {
	options map[string]string // option name -> value for show-options
	calls   []string
}

func (f *fakeRunner) Run(args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if len(args) > 0 && args[0] == "show-options" {
		return f.options[args[len(args)-1]], nil
	}
	return "", nil
}

var now = time.Unix(1700000000, 0)

func TestApplyStateChangeResetsClockAndSeen(t *testing.T) {
	r := &fakeRunner{options: map[string]string{"@agent_state": "working"}}
	ev := Event{Name: "Stop"}
	if err := Apply(r, "%1", ev, Decide(ev), now); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(r.calls, " | ")
	for _, want := range []string{
		"@agent_state done",
		"@agent_since 1700000000",
		"set-option -pqu -t %1 @agent_seen",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in calls: %s", want, got)
		}
	}
}

func TestApplySameStateKeepsClock(t *testing.T) {
	r := &fakeRunner{options: map[string]string{"@agent_state": "working"}}
	ev := Event{Name: "PreToolUse", SessionID: "sid1"}
	if err := Apply(r, "%1", ev, Decide(ev), now); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(r.calls, " | ")
	if strings.Contains(got, "@agent_since") {
		t.Errorf("same-state apply must not reset @agent_since: %s", got)
	}
	if !strings.Contains(got, "@agent_present 1") || !strings.Contains(got, "@agent_session_id sid1") {
		t.Errorf("register options missing: %s", got)
	}
}

func TestApplySubagentFloorZero(t *testing.T) {
	r := &fakeRunner{options: map[string]string{"@agent_subagents": "0"}}
	ev := Event{Name: "SubagentStop"}
	if err := Apply(r, "%1", ev, Decide(ev), now); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(r.calls, " | ")
	if !strings.Contains(got, "@agent_subagents 0") {
		t.Errorf("subagent count must floor at 0: %s", got)
	}
}

func TestApplyClearAllUnsetsEverything(t *testing.T) {
	r := &fakeRunner{}
	ev := Event{Name: "SessionEnd"}
	if err := Apply(r, "%1", ev, Decide(ev), now); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(r.calls, " ")
	for _, name := range allOptions {
		if !strings.Contains(got, "-pqu -t %1 "+name) {
			t.Errorf("ClearAll must unset %s: %s", name, got)
		}
	}
}
