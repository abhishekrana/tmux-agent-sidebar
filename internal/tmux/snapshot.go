package tmux

import (
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abhishekrana/tmux-agent-sidebar/internal/model"
)

// snapFormat: one line per pane across the whole server. Field order
// must match parseLine.
const snapFormat = "#{session_name}\t#{session_attached}\t#{window_index}\t#{window_name}\t#{window_active}\t#{pane_id}\t#{pane_active}\t#{pane_current_command}\t#{pane_current_path}\t#{@agent_present}\t#{@agent_state}\t#{@agent_since}\t#{@agent_seen}\t#{@agent_subagents}"

// agentCommands guards against zombies: a pane whose Claude died without
// SessionEnd keeps its options, but its foreground command changes.
// Claude Code runs as `claude` (native) or `node` (npm install).
var agentCommands = map[string]bool{"claude": true, "node": true}

// CurrentSession names the session the sidebar pane lives in.
func CurrentSession(r Runner) string {
	out, _ := r.Run("display-message", "-p", "#S")
	return out
}

// Snapshot builds the full model: every session on the server (sessions
// without agents included), agents from panes stamped by the hooks.
// Sessions are sorted alphabetically, agents by window then pane.
//
// As a side effect, a done agent whose pane the user is looking at is
// marked seen (@agent_seen=1) so it renders dimmed from then on.
func Snapshot(r Runner, bc *BranchCache, currentSession string) model.Snapshot {
	out, err := r.Run("list-panes", "-a", "-F", snapFormat)
	if err != nil || out == "" {
		return model.Snapshot{}
	}

	bySession := map[string]*model.Session{}
	for ln := range strings.SplitSeq(out, "\n") {
		f := strings.Split(ln, "\t")
		if len(f) < 14 {
			continue
		}
		name := f[0]
		sess := bySession[name]
		if sess == nil {
			sess = &model.Session{Name: name, Current: name == currentSession}
			bySession[name] = sess
		}
		if f[9] != "1" || !agentCommands[f[7]] {
			continue // not an agent pane
		}
		windowIdx, _ := strconv.Atoi(f[2])
		since, _ := strconv.ParseInt(f[11], 10, 64)
		subagents, _ := strconv.Atoi(f[13])
		state := model.AgentState(f[10])
		if state == "" {
			state = model.StateIdle
		}
		seen := f[12] == "1"
		attached, paneActive, windowActive := f[1] != "0" && f[1] != "", f[6] == "1", f[4] == "1"
		if state == model.StateDone && !seen && attached && paneActive && windowActive {
			// User is looking at the finished agent right now.
			_, _ = r.Run("set-option", "-pq", "-t", f[5], "@agent_seen", "1")
			seen = true
		}
		sess.Agents = append(sess.Agents, model.Agent{
			PaneID:      f[5],
			WindowIndex: windowIdx,
			// Row label is the agent command (user preference: not the
			// window name — the branch line disambiguates instead).
			WindowName: f[7],
			Branch:     bc.Get(f[8]),
			State:      state,
			Seen:       seen,
			Since:      time.Unix(since, 0),
			Subagents:  subagents,
		})
	}

	names := make([]string, 0, len(bySession))
	for name := range bySession {
		names = append(names, name)
	}
	sort.Strings(names)
	snap := model.Snapshot{}
	for _, name := range names {
		sess := bySession[name]
		sort.Slice(sess.Agents, func(i, j int) bool {
			a, b := sess.Agents[i], sess.Agents[j]
			if a.WindowIndex != b.WindowIndex {
				return a.WindowIndex < b.WindowIndex
			}
			return a.PaneID < b.PaneID
		})
		snap.Sessions = append(snap.Sessions, *sess)
	}
	return snap
}

// BranchCache memoizes git branch lookups per directory so the 1s
// snapshot tick doesn't fork git for every agent every time.
type BranchCache struct {
	entries map[string]branchEntry
}

type branchEntry struct {
	branch string
	at     time.Time
}

const branchTTL = 5 * time.Second

func NewBranchCache() *BranchCache {
	return &BranchCache{entries: map[string]branchEntry{}}
}

func (c *BranchCache) Get(dir string) string {
	if dir == "" {
		return ""
	}
	if e, ok := c.entries[dir]; ok && time.Since(e.at) < branchTTL {
		return e.branch
	}
	out, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	branch := strings.TrimSpace(string(out))
	c.entries[dir] = branchEntry{branch: branch, at: time.Now()}
	return branch
}
