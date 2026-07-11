package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/abhishekrana/tmux-agent-sidebar/internal/model"
)

// spinnerFrames animates the working state (braille, like Claude's own UI).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type blockKind int

const (
	blockSession blockKind = iota // group header, one line, not selectable
	blockAgent                    // agent + branch + subagents: one selectable unit
)

// block is one navigation unit; an agent block renders as 1-3 lines that
// select, highlight, and click together.
type block struct {
	kind    blockKind
	session int // index into snapshot.Sessions
	agent   int // index into session.Agents (blockAgent only)
}

// Both row kinds are selectable: an agent block jumps to its pane, a
// session header switches to that session.
func (block) selectable() bool { return true }

// buildBlocks flattens the snapshot: session headers are pure group
// labels; agents form the flat, selectable list.
func buildBlocks(snap model.Snapshot) []block {
	var blocks []block
	for si, sess := range snap.Sessions {
		blocks = append(blocks, block{kind: blockSession, session: si})
		for ai := range sess.Agents {
			blocks = append(blocks, block{kind: blockAgent, session: si, agent: ai})
		}
	}
	return blocks
}

func stateIcon(s model.AgentState, frame int) string {
	switch s {
	case model.StateWorking:
		return spinnerFrames[frame%len(spinnerFrames)]
	case model.StatePermission:
		return "◔"
	case model.StateQuestion:
		return "?"
	case model.StateDone:
		return "✓"
	default:
		return "·"
	}
}

// elapsed renders a compact duration like 37s, 2m, 1h12m.
func elapsed(since time.Time, now time.Time) string {
	if since.IsZero() {
		return ""
	}
	d := now.Sub(since)
	switch {
	case d < 0:
		return ""
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// line lays out left and right fragments within width, truncating the left
// fragment first. Both fragments may contain ANSI styling.
func line(left, right string, width int) string {
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	if rw > 0 && lw+rw+1 > width {
		// Not enough room: drop the right fragment rather than corrupt it.
		if lw > width {
			left = truncate(left, width)
		}
		return left
	}
	if lw > width {
		return truncate(left, width)
	}
	pad := max(width-lw-rw, 0)
	return left + strings.Repeat(" ", pad) + right
}

// truncate cuts a styled string to width cells with an ellipsis.
func truncate(s string, width int) string {
	if width <= 1 {
		return "…"
	}
	return lipgloss.NewStyle().MaxWidth(width-1).Render(s) + "…"
}

// renderer turns a snapshot into the sidebar view.
type renderer struct {
	theme Theme
	width int
	nameW int // agent-name column width, derived from the snapshot
}

// labelW fits the longest state label ("permission").
const labelW = 10

// nameColumn returns the width of the agent-name column: the longest name
// in the snapshot, clamped so long names can't crowd out the state columns.
func nameColumn(snap model.Snapshot) int {
	w := 6 // len("claude")
	for _, sess := range snap.Sessions {
		for _, a := range sess.Agents {
			if n := len([]rune(a.Command)); n > w {
				w = n
			}
		}
	}
	return min(w, 12)
}

// padCol pads (or truncates) a plain string to exactly w cells.
func padCol(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		return string(r[:w-1]) + "…"
	}
	return s + strings.Repeat(" ", w-len(r))
}

func (r renderer) sep() string {
	return lipgloss.NewStyle().Foreground(r.theme.Muted).Render(strings.Repeat("─", r.width))
}

func (r renderer) header(snap model.Snapshot, frame int) string {
	title := lipgloss.NewStyle().Foreground(r.theme.Emphasis).Bold(true).Render(" tmux agents")
	count := fmt.Sprintf("%d/%d", snap.Working(), snap.Total())
	dot := " "
	if snap.Working() > 0 {
		dot = lipgloss.NewStyle().Foreground(r.theme.Working).Render(spinnerFrames[frame%len(spinnerFrames)])
	}
	right := lipgloss.NewStyle().Foreground(r.theme.Muted).Render(count) + " " + dot + " "
	return line(title, right, r.width)
}

// sessionMarker is the right-hand tag of a session header: "← here" for the
// current session, "no agents" for an empty one.
func (r renderer) sessionMarker(sess model.Session) (string, lipgloss.Color) {
	switch {
	case sess.Current:
		return "← here ", r.theme.Accent
	case len(sess.Agents) == 0:
		return "no agents ", r.theme.Muted
	}
	return "", r.theme.Accent
}

// sessionRow is the session's single name line. When lit (hovered or
// selected) it fills with the highlight background; otherwise it's the name
// plus its colored marker.
func (r renderer) sessionRow(sess model.Session, lit bool) string {
	marker, markerColor := r.sessionMarker(sess)
	if lit {
		gap := max(r.width-lipgloss.Width(sess.Name)-lipgloss.Width(marker), 0)
		plain := sess.Name + strings.Repeat(" ", gap) + marker
		return lipgloss.NewStyle().Foreground(r.theme.Emphasis).Bold(sess.Current).
			Background(r.theme.SelBg).Render(padCol(plain, r.width))
	}
	name := lipgloss.NewStyle().Foreground(r.theme.Emphasis).Bold(sess.Current).Render(sess.Name)
	right := ""
	if marker != "" {
		right = lipgloss.NewStyle().Foreground(markerColor).Render(marker)
	}
	return line(name, right, r.width)
}

// sessionBlock is a blank spacer (groups the sessions) above the name line.
// Both lines select the session; only the name lights.
func (r renderer) sessionBlock(sess model.Session, lit bool) []string {
	return []string{"", r.sessionRow(sess, lit)}
}

func (r renderer) agentRow(a model.Agent, lit bool, frame int, now time.Time) string {
	col := r.theme.StateColor(a.State)
	if a.State == model.StateDone && a.Seen {
		col = r.theme.Muted // acknowledged: stop shouting
	}
	// Each fragment carries its own style: an outer background would break at
	// the inner resets and leave the highlight half-painted.
	frag := func(c lipgloss.Color) lipgloss.Style {
		s := lipgloss.NewStyle().Foreground(c)
		if lit {
			s = s.Background(r.theme.SelBg)
		}
		return s
	}
	row := frag(col).Render("   "+stateIcon(a.State, frame)+" ") +
		frag(r.theme.Fg).Render(padCol(a.Command, r.nameW)) +
		frag(col).Render("  "+padCol(a.State.Label(), labelW)) +
		frag(r.theme.Muted).Render(fmt.Sprintf("%5s", elapsed(a.Since, now)))
	if pad := r.width - lipgloss.Width(row); pad > 0 && lit {
		row += frag(r.theme.Fg).Render(strings.Repeat(" ", pad))
	}
	return line(row, "", r.width)
}

// subRow renders a secondary line of an agent block (branch, subagents),
// carrying the block's fill edge to edge. Text is padded before styling so
// the whole line sits in one styled run with no bg gaps.
func (r renderer) subRow(text string, italic, lit bool) string {
	s := lipgloss.NewStyle().Foreground(r.theme.Muted).Italic(italic)
	if lit {
		s = s.Background(r.theme.SelBg)
	}
	return s.Render(padCol("     "+text, r.width))
}

// agentBlock renders an agent's full block: main row, branch (tapered
// with … when long), and subagent count.
func (r renderer) agentBlock(a model.Agent, lit bool, frame int, now time.Time) []string {
	lines := []string{r.agentRow(a, lit, frame, now)}
	if a.Branch != "" {
		lines = append(lines, r.subRow(a.Branch, true, lit))
	}
	if a.Subagents > 0 {
		plural := "s"
		if a.Subagents == 1 {
			plural = ""
		}
		lines = append(lines, r.subRow("⤷ "+strconv.Itoa(a.Subagents)+" subagent"+plural, false, lit))
	}
	return lines
}

// blockLineCount mirrors agentBlock's line count without rendering.
func blockLineCount(b block, snap model.Snapshot) int {
	if b.kind == blockSession {
		return 2 // name + summary line
	}
	a := snap.Sessions[b.session].Agents[b.agent]
	n := 1
	if a.Branch != "" {
		n++
	}
	if a.Subagents > 0 {
		n++
	}
	return n
}

func (r renderer) footer(snap model.Snapshot) string {
	var status string
	if att := snap.Attention(); att > 0 {
		status = lipgloss.NewStyle().Foreground(r.theme.Perm).Bold(true).
			Render(fmt.Sprintf(" ⚠ %d need attention", att))
	} else {
		status = lipgloss.NewStyle().Foreground(r.theme.Muted).Render(" all quiet")
	}
	hint := lipgloss.NewStyle().Foreground(r.theme.Muted).Render(" j/k · ⏎/click jump · q hide")
	return r.sep() + "\n" + status + "\n" + hint
}
