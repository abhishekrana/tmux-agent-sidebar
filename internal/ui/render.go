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

// sessionRow is the unselected name line of a session header (the selected
// band is built in sessionBlock). Its right marker is "← here" for the
// current session or "no agents" for an empty one.
func (r renderer) sessionRow(sess model.Session) string {
	marker, markerColor := "", r.theme.Accent
	switch {
	case sess.Current:
		marker = "← here "
	case len(sess.Agents) == 0:
		marker, markerColor = "no agents ", r.theme.Muted
	}
	name := lipgloss.NewStyle().Foreground(r.theme.Emphasis).Bold(sess.Current).Render(sess.Name)
	right := ""
	if marker != "" {
		right = lipgloss.NewStyle().Foreground(markerColor).Render(marker)
	}
	return line(name, right, r.width)
}

// sessionBlock renders a session header as two lines: a leading spacer
// that separates groups, then the name. When selected both lines form one
// highlighted band (a 2-row click target); otherwise the spacer is blank.
func (r renderer) sessionBlock(sess model.Session, selected bool) []string {
	if !selected {
		return []string{"", r.sessionRow(sess)}
	}
	// A 2-row highlight band: blank spacer above, then the name. Both rows
	// carry the selection bg so the whole target reads as one block.
	marker := "→ switch "
	if sess.Current {
		marker = "← here "
	}
	nameW := min(lipgloss.Width(sess.Name), max(r.width-lipgloss.Width(marker), 0))
	name := string([]rune(sess.Name)[:nameW]) +
		strings.Repeat(" ", max(r.width-nameW-lipgloss.Width(marker), 0)) + marker
	sel := lipgloss.NewStyle().Foreground(r.theme.Emphasis).Bold(true).Background(r.theme.SelBg)
	// The spacer leaves its last column unpainted: a full-width all-blank bg
	// line makes tmux drop the highlight on the row below it.
	spacer := sel.Render(strings.Repeat(" ", max(r.width-1, 0)))
	return []string{spacer, sel.Render(padCol(name, r.width))}
}

func (r renderer) agentRow(a model.Agent, selected bool, frame int, now time.Time) string {
	col := r.theme.StateColor(a.State)
	if a.State == model.StateDone && a.Seen {
		col = r.theme.Muted // acknowledged: stop shouting
	}
	// Every fragment (including spacing) carries the full style itself:
	// wrapping pre-styled text in an outer background breaks at the inner
	// resets, leaving the highlight half-painted.
	frag := func(c lipgloss.Color, bold bool) lipgloss.Style {
		s := lipgloss.NewStyle().Foreground(c).Bold(bold || selected)
		if selected {
			s = s.Background(r.theme.SelBg)
		}
		return s
	}
	// Fixed columns: icon · name · state · time (right-aligned) · warn.
	row := frag(col, false).Render("   "+stateIcon(a.State, frame)+" ") +
		frag(r.theme.Fg, false).Render(padCol(a.Command, r.nameW)) +
		frag(col, false).Render("  "+padCol(a.State.Label(), labelW)) +
		frag(r.theme.Muted, false).Render(fmt.Sprintf("%5s", elapsed(a.Since, now)))
	if pad := r.width - lipgloss.Width(row); pad > 0 && selected {
		row += frag(r.theme.Fg, false).Render(strings.Repeat(" ", pad))
	}
	return line(row, "", r.width)
}

// subRow renders a secondary line of an agent block (branch, subagents),
// carrying the block's selection highlight edge to edge. The plain text
// is truncated/padded BEFORE styling so the whole line — including the …
// and trailing padding — sits inside one styled run with no bg gaps.
func (r renderer) subRow(text string, italic, selected bool) string {
	s := lipgloss.NewStyle().Foreground(r.theme.Muted).Italic(italic)
	if selected {
		s = s.Background(r.theme.SelBg)
	}
	plain := padCol("     "+text, r.width)
	return s.Render(plain)
}

// agentBlock renders an agent's full block: main row, branch (tapered
// with … when long), and subagent count.
func (r renderer) agentBlock(a model.Agent, selected bool, frame int, now time.Time) []string {
	lines := []string{r.agentRow(a, selected, frame, now)}
	if a.Branch != "" {
		lines = append(lines, r.subRow(a.Branch, true, selected))
	}
	if a.Subagents > 0 {
		plural := "s"
		if a.Subagents == 1 {
			plural = ""
		}
		lines = append(lines, r.subRow("⤷ "+strconv.Itoa(a.Subagents)+" subagent"+plural, false, selected))
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
