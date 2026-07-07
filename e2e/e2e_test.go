// Package e2e exercises the plugin against a real, throwaway tmux server.
//
// Every test starts its own server on a private socket (tmux -L), so runs
// never touch the developer's live tmux. Bare `tmux` calls made by the
// scripts and the binary are routed to that server through a PATH shim,
// and agents are simulated with a copy of sleep(1) named "claude" (the
// snapshot filters on #{pane_current_command}) plus real `hook` events.
//
// Skipped with -short and when tmux is not installed.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	repoRoot string
	binPath  string
	shimDir  string // holds the tmux shim and the fake `claude`
)

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("tmux"); err != nil {
		fmt.Println("e2e: tmux not installed, skipping")
		os.Exit(0)
	}
	var err error
	repoRoot, err = filepath.Abs("..")
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(repoRoot, "bin", "tmux-agent-sidebar")

	build := exec.Command("go", "build", "-o", binPath, "./cmd/tmux-agent-sidebar")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Printf("e2e: build failed: %v\n%s", err, out)
		os.Exit(1)
	}

	shimDir, err = os.MkdirTemp("", "tas-e2e-shim")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(shimDir)

	realTmux, _ := exec.LookPath("tmux")
	shim := "#!/bin/sh\nexec " + realTmux + " -L \"$TAS_TEST_SOCKET\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(shimDir, "tmux"), []byte(shim), 0o755); err != nil {
		panic(err)
	}

	// A fake agent: sleep(1) renamed so #{pane_current_command} == "claude".
	realSleep, _ := exec.LookPath("sleep")
	data, err := os.ReadFile(realSleep)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(shimDir, "claude"), data, 0o755); err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}

// server is one isolated tmux server on a private socket.
type server struct {
	t   *testing.T
	env []string
}

func start(t *testing.T) *server {
	if testing.Short() {
		t.Skip("e2e skipped with -short")
	}
	sock := fmt.Sprintf("tas-e2e-%d-%s", os.Getpid(),
		strings.NewReplacer("/", "-", " ", "-").Replace(t.Name()))
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "TMUX", "TMUX_PANE", "PATH", "TAS_TEST_SOCKET", "TERM":
		default:
			env = append(env, kv)
		}
	}
	env = append(env,
		"PATH="+shimDir+":"+os.Getenv("PATH"),
		"TAS_TEST_SOCKET="+sock,
		"TERM=xterm-256color",
	)
	s := &server{t: t, env: env}
	t.Cleanup(func() { _, _ = s.tmuxErr("kill-server") })
	return s
}

func (s *server) tmuxErr(args ...string) (string, error) {
	cmd := exec.Command(filepath.Join(shimDir, "tmux"), args...)
	cmd.Env = s.env
	out, err := cmd.CombinedOutput()
	return strings.TrimRight(string(out), "\n"), err
}

func (s *server) tmux(args ...string) string {
	s.t.Helper()
	out, err := s.tmuxErr(args...)
	if err != nil {
		s.t.Fatalf("tmux %v: %v\n%s", args, err, out)
	}
	return out
}

// newSession creates a detached session and returns its first pane id.
func (s *server) newSession(name string) string {
	s.t.Helper()
	return s.tmux("new-session", "-d", "-s", name, "-x", "220", "-y", "50",
		"-P", "-F", "#{pane_id}")
}

// agentPane opens a pane running the fake claude and registers it via a
// real hook event, exactly as Claude Code would.
func (s *server) agentPane(session string) string {
	s.t.Helper()
	pane := s.tmux("split-window", "-d", "-t", session, "-P", "-F", "#{pane_id}",
		"claude 600")
	s.hook(pane, `{"hook_event_name":"SessionStart","session_id":"e2e"}`)
	return pane
}

// hook feeds one event to `tmux-agent-sidebar hook` for the given pane.
func (s *server) hook(pane, eventJSON string) {
	s.t.Helper()
	cmd := exec.Command(binPath, "hook")
	cmd.Env = append(append([]string{}, s.env...), "TMUX_PANE="+pane)
	cmd.Stdin = strings.NewReader(eventJSON)
	if out, err := cmd.CombinedOutput(); err != nil {
		s.t.Fatalf("hook %s: %v\n%s", eventJSON, err, out)
	}
}

// script runs one of the plugin's shell scripts against this server.
func (s *server) script(name string, args ...string) {
	s.t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(repoRoot, "scripts", name)}, args...)...)
	cmd.Env = s.env
	if out, err := cmd.CombinedOutput(); err != nil {
		s.t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func (s *server) paneOption(pane, name string) string {
	out, _ := s.tmuxErr("show-option", "-pqv", "-t", pane, name)
	return out
}

func (s *server) sidebarPane(session string) string {
	out, _ := s.tmuxErr("show-option", "-t", session, "-qv", "@sidebar_pane")
	return out
}

// sidebarAlive reports whether the session has a live sidebar pane.
func (s *server) sidebarAlive(session string) bool {
	pane := s.sidebarPane(session)
	if pane == "" {
		return false
	}
	panes, err := s.tmuxErr("list-panes", "-s", "-t", session,
		"-F", "#{pane_id} #{pane_current_command}")
	if err != nil {
		return false
	}
	return strings.Contains(panes, pane+" tmux-agent-sidebar")
}

// capture returns the sidebar pane content with escape sequences.
func (s *server) capture(pane string) string {
	out, _ := s.tmuxErr("capture-pane", "-p", "-e", "-t", pane)
	return out
}

// captureText returns the pane content without escape sequences.
func (s *server) captureText(pane string) string {
	out, _ := s.tmuxErr("capture-pane", "-p", "-t", pane)
	return out
}

// click injects a left mouse press+release at 1-based (col, row) into the
// pane's input as raw SGR sequences — exactly the bytes a terminal sends,
// so the TUI's real mouse path runs.
func (s *server) click(pane string, col, row int) {
	s.t.Helper()
	for _, suffix := range []string{"M", "m"} { // press, then release
		seq := fmt.Sprintf("\x1b[<0;%d;%d%s", col, row, suffix)
		args := []string{"send-keys", "-H", "-t", pane}
		for _, b := range []byte(seq) {
			args = append(args, fmt.Sprintf("%02x", b))
		}
		s.tmux(args...)
	}
}

func waitFor(t *testing.T, desc string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

// selBG is the Solarized Light selection background every theme test uses.
const selBG = "48;2;238;232;213"

// highlightedAgentLine returns the text of the line carrying the selection
// background and the word claude, "" if none.
func highlightedAgentLine(capture string) (line string, lineNo int) {
	for i, l := range strings.Split(capture, "\n") {
		if strings.Contains(l, selBG) && strings.Contains(l, "claude") {
			return l, i
		}
	}
	return "", -1
}

// --- tests ---

// TestHookStateMachineLive drives the full Claude Code event sequence
// against a real pane and watches @agent_* options transition.
func TestHookStateMachineLive(t *testing.T) {
	s := start(t)
	s.newSession("work")
	pane := s.agentPane("work")

	if got := s.paneOption(pane, "@agent_state"); got != "idle" {
		t.Fatalf("after SessionStart: state=%q, want idle", got)
	}
	if got := s.paneOption(pane, "@agent_present"); got != "1" {
		t.Fatalf("after SessionStart: present=%q, want 1", got)
	}

	s.hook(pane, `{"hook_event_name":"UserPromptSubmit","session_id":"e2e"}`)
	if got := s.paneOption(pane, "@agent_state"); got != "working" {
		t.Errorf("after UserPromptSubmit: state=%q, want working", got)
	}
	since := s.paneOption(pane, "@agent_since")

	// PreToolUse while already working must not reset the clock.
	s.hook(pane, `{"hook_event_name":"PreToolUse","session_id":"e2e"}`)
	if got := s.paneOption(pane, "@agent_since"); got != since {
		t.Errorf("PreToolUse reset @agent_since: %q -> %q", since, got)
	}

	s.hook(pane, `{"hook_event_name":"Notification","notification_type":"permission_prompt"}`)
	if got := s.paneOption(pane, "@agent_state"); got != "permission" {
		t.Errorf("after permission_prompt: state=%q, want permission", got)
	}

	// idle_prompt nudges are deliberately ignored.
	s.hook(pane, `{"hook_event_name":"Notification","notification_type":"idle_prompt"}`)
	if got := s.paneOption(pane, "@agent_state"); got != "permission" {
		t.Errorf("idle_prompt changed state to %q", got)
	}

	s.hook(pane, `{"hook_event_name":"SubagentStart"}`)
	s.hook(pane, `{"hook_event_name":"SubagentStart"}`)
	s.hook(pane, `{"hook_event_name":"SubagentStop"}`)
	if got := s.paneOption(pane, "@agent_subagents"); got != "1" {
		t.Errorf("subagents=%q, want 1", got)
	}

	s.hook(pane, `{"hook_event_name":"Stop"}`)
	if got := s.paneOption(pane, "@agent_state"); got != "done" {
		t.Errorf("after Stop: state=%q, want done", got)
	}

	s.hook(pane, `{"hook_event_name":"SessionEnd"}`)
	if got := s.paneOption(pane, "@agent_present"); got != "" {
		t.Errorf("after SessionEnd: present=%q, want unset", got)
	}
	if got := s.paneOption(pane, "@agent_state"); got != "" {
		t.Errorf("after SessionEnd: state=%q, want unset", got)
	}
}

// TestGlobalToggleLifecycle: one toggle opens a sidebar in every session,
// sessions born while on get one automatically, next toggle closes all.
func TestGlobalToggleLifecycle(t *testing.T) {
	s := start(t)
	for _, name := range []string{"aaa", "bbb", "ccc"} {
		s.newSession(name)
	}

	s.script("toggle.sh")
	for _, name := range []string{"aaa", "bbb", "ccc"} {
		waitFor(t, "sidebar in "+name, 5*time.Second, func() bool {
			return s.sidebarAlive(name)
		})
	}
	if hook := s.tmux("show-hooks", "-g"); !strings.Contains(hook, "session-created") {
		t.Error("global session-created hook not installed")
	}

	// A session created while globally on gets a sidebar automatically.
	s.newSession("ddd")
	waitFor(t, "auto sidebar in new session", 5*time.Second, func() bool {
		return s.sidebarAlive("ddd")
	})

	s.script("toggle.sh")
	for _, name := range []string{"aaa", "bbb", "ccc", "ddd"} {
		waitFor(t, "sidebar gone in "+name, 5*time.Second, func() bool {
			return !s.sidebarAlive(name)
		})
		if got := s.sidebarPane(name); got != "" {
			t.Errorf("%s: @sidebar_pane=%q after toggle off, want unset", name, got)
		}
	}
	// tmux 3.7 keeps an empty "session-created" entry after set-hook -gu,
	// so assert on the command being gone, then behaviorally: a session
	// born after toggle-off must NOT get a sidebar.
	if hook := s.tmux("show-hooks", "-g"); strings.Contains(hook, "open.sh") {
		t.Errorf("global session-created hook survived toggle off:\n%s", hook)
	}
	s.newSession("eee")
	time.Sleep(1 * time.Second)
	if s.sidebarAlive("eee") {
		t.Error("session created after toggle-off got a sidebar")
	}
}

// TestSidebarRendersAgentState: the TUI picks up hook-driven state changes.
func TestSidebarRendersAgentState(t *testing.T) {
	s := start(t)
	s.newSession("work")
	agent := s.agentPane("work")
	s.hook(agent, `{"hook_event_name":"UserPromptSubmit","session_id":"e2e"}`)

	s.script("open.sh", "work")
	side := s.sidebarPane("work")
	waitFor(t, "sidebar shows working agent", 5*time.Second, func() bool {
		c := s.capture(side)
		return strings.Contains(c, "claude") && strings.Contains(c, "working")
	})

	s.hook(agent, `{"hook_event_name":"Stop"}`)
	waitFor(t, "sidebar shows done", 5*time.Second, func() bool {
		return strings.Contains(s.capture(side), "done")
	})

	// Killing the agent pane removes its entry within a tick.
	s.tmux("kill-pane", "-t", agent)
	waitFor(t, "dead agent dropped", 5*time.Second, func() bool {
		return !strings.Contains(s.capture(side), "claude ")
	})
}

// TestSelectionSyncAcrossSidebars is the regression test for the click
// bug: each session's sidebar is its own process, so a jump published in
// one must move the highlight in all of them — immediately (wait-for
// signal), not on the next 1s tick.
func TestSelectionSyncAcrossSidebars(t *testing.T) {
	s := start(t)
	s.newSession("aaa")
	s.newSession("bbb")
	s.agentPane("aaa")
	agentB := s.agentPane("bbb")

	s.script("toggle.sh")
	sideA, sideB := s.sidebarPane("aaa"), s.sidebarPane("bbb")
	waitFor(t, "both sidebars list both agents", 5*time.Second, func() bool {
		return strings.Contains(s.capture(sideA), "bbb") &&
			strings.Contains(s.capture(sideB), "bbb")
	})

	// Publish a selection the way activate() does: option + signal.
	s.tmux("set-option", "-g", "@sidebar_selected", agentB, ";",
		"wait-for", "-S", "tmux-agent-sidebar-refresh")

	// Both sidebars must adopt it well under the 1s snapshot tick.
	waitFor(t, "highlight on bbb's agent in both sidebars", 700*time.Millisecond, func() bool {
		for _, side := range []string{sideA, sideB} {
			capture := s.capture(side)
			_, lineNo := highlightedAgentLine(capture)
			if lineNo < 0 {
				return false
			}
			bbbLine := -1
			for i, l := range strings.Split(capture, "\n") {
				if strings.Contains(l, "bbb") {
					bbbLine = i
				}
			}
			if lineNo < bbbLine { // highlight must sit under the bbb header
				return false
			}
		}
		return true
	})
}

// TestJumpViaEnter walks the full user story with a real attached client:
// select the other session's agent in the sidebar, press Enter, and land
// there with that sidebar already highlighting the agent.
func TestJumpViaEnter(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script(1) not available for pty client")
	}
	s := start(t)
	s.newSession("aaa")
	s.newSession("bbb")
	s.agentPane("aaa")
	s.agentPane("bbb")
	// Keep windows at their detached size when the small pty attaches.
	s.tmux("set-option", "-g", "window-size", "manual")

	s.script("toggle.sh")
	sideA, sideB := s.sidebarPane("aaa"), s.sidebarPane("bbb")
	waitFor(t, "sidebars ready", 5*time.Second, func() bool {
		return strings.Contains(s.capture(sideA), "bbb") &&
			strings.Contains(s.capture(sideB), "bbb")
	})

	// Attach a real client (pty via script) to aaa.
	client := exec.Command("script", "-qfc", "tmux attach-session -t aaa", "/dev/null")
	client.Env = s.env
	if err := client.Start(); err != nil {
		t.Fatalf("attach client: %v", err)
	}
	t.Cleanup(func() { _ = client.Process.Kill(); _, _ = client.Process.Wait() })
	waitFor(t, "client attached", 5*time.Second, func() bool {
		out, _ := s.tmuxErr("list-clients", "-F", "#{client_session}")
		return strings.Contains(out, "aaa")
	})

	// In aaa's sidebar: G selects the last agent (bbb's), Enter jumps.
	s.tmux("send-keys", "-t", sideA, "G", "")
	s.tmux("send-keys", "-t", sideA, "Enter", "")

	waitFor(t, "client switched to bbb", 5*time.Second, func() bool {
		out, _ := s.tmuxErr("list-clients", "-F", "#{client_session}")
		return strings.Contains(out, "bbb")
	})

	// The bug: bbb's own sidebar (a different process) must show the
	// highlight on the jumped-to agent without any further clicks.
	waitFor(t, "bbb sidebar highlights jumped-to agent", 700*time.Millisecond, func() bool {
		_, lineNo := highlightedAgentLine(s.capture(sideB))
		return lineNo >= 0
	})
}

// TestClickJump is the full reproduction of the reported bug, mouse and
// all: single-click the other session's agent in the sidebar, land in
// that session, and see its own sidebar highlight the clicked agent —
// with no second click and no tick-latency.
func TestClickJump(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script(1) not available for pty client")
	}
	s := start(t)
	s.newSession("aaa")
	s.newSession("bbb")
	s.agentPane("aaa")
	s.agentPane("bbb")
	s.tmux("set-option", "-g", "window-size", "manual")

	s.script("toggle.sh")
	sideA, sideB := s.sidebarPane("aaa"), s.sidebarPane("bbb")
	waitFor(t, "sidebars ready", 5*time.Second, func() bool {
		return strings.Contains(s.capture(sideA), "bbb") &&
			strings.Contains(s.capture(sideB), "bbb")
	})

	client := exec.Command("script", "-qfc", "tmux attach-session -t aaa", "/dev/null")
	client.Env = s.env
	if err := client.Start(); err != nil {
		t.Fatalf("attach client: %v", err)
	}
	t.Cleanup(func() { _ = client.Process.Kill(); _, _ = client.Process.Wait() })
	waitFor(t, "client attached", 5*time.Second, func() bool {
		out, _ := s.tmuxErr("list-clients", "-F", "#{client_session}")
		return strings.Contains(out, "aaa")
	})

	// Find bbb's agent row in aaa's sidebar: the first claude line after
	// the bbb session header (rows are 0-based, SGR is 1-based).
	lines := strings.Split(s.captureText(sideA), "\n")
	row := -1
	for i, l := range lines {
		if strings.Contains(l, "bbb") {
			for j := i + 1; j < len(lines); j++ {
				if strings.Contains(lines[j], "claude") {
					row = j + 1
					break
				}
			}
			break
		}
	}
	if row < 0 {
		t.Fatalf("bbb's agent row not found in sidebar:\n%s", strings.Join(lines, "\n"))
	}

	s.click(sideA, 5, row)

	waitFor(t, "client switched to bbb after single click", 5*time.Second, func() bool {
		out, _ := s.tmuxErr("list-clients", "-F", "#{client_session}")
		return strings.Contains(out, "bbb")
	})
	// Both sidebars — including bbb's, a separate process that never saw
	// the click — must highlight the clicked agent, faster than the tick.
	waitFor(t, "clicked agent highlighted in both sidebars", 700*time.Millisecond, func() bool {
		for _, side := range []string{sideA, sideB} {
			if _, lineNo := highlightedAgentLine(s.capture(side)); lineNo != row-1 {
				return false
			}
		}
		return true
	})
}

// TestFollowWindowAndSelfHeal: the sidebar pane follows the active window,
// and stale state self-heals when the sidebar process is gone.
func TestFollowWindowAndSelfHeal(t *testing.T) {
	s := start(t)
	s.newSession("work")
	s.script("open.sh", "work")
	side := s.sidebarPane("work")
	waitFor(t, "sidebar open", 5*time.Second, func() bool { return s.sidebarAlive("work") })

	s.tmux("new-window", "-t", "work")
	waitFor(t, "sidebar followed to new window", 5*time.Second, func() bool {
		cur, _ := s.tmuxErr("display-message", "-t", "work", "-p", "#{window_id}")
		sidewin, _ := s.tmuxErr("display-message", "-t", side, "-p", "#{window_id}")
		return cur != "" && cur == sidewin
	})

	// Kill the sidebar process behind tmux's back; the next window change
	// must clean up the stale options and hook.
	s.tmux("kill-pane", "-t", side)
	s.tmux("new-window", "-t", "work")
	waitFor(t, "stale sidebar state cleaned", 5*time.Second, func() bool {
		return s.sidebarPane("work") == ""
	})
	if on, _ := s.tmuxErr("show-option", "-t", "work", "-qv", "@sidebar_on"); on != "" {
		t.Errorf("@sidebar_on=%q after self-heal, want unset", on)
	}
}

// TestStatusSegment: the status subcommand counts attention + working.
func TestStatusSegment(t *testing.T) {
	s := start(t)
	s.newSession("work")
	a := s.agentPane("work")
	b := s.agentPane("work")
	s.hook(a, `{"hook_event_name":"UserPromptSubmit","session_id":"e2e"}`)
	s.hook(b, `{"hook_event_name":"Notification","notification_type":"permission_prompt"}`)

	cmd := exec.Command(binPath, "status")
	cmd.Env = s.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "⚠1") || !strings.Contains(string(out), "●1") {
		t.Errorf("status segment = %q, want ⚠1 and ●1", out)
	}
}
