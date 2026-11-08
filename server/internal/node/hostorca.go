package node

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/nodeclient"
	"github.com/nwverse/persona/server/internal/orcart"
	"github.com/nwverse/persona/server/internal/proto"
)

// orcaHost is the Orca desktop app (docs/plans/plan-orca-support.md "Orca source"), through its runtime's unix socket. A pane
// is one of Orca's terminals, its id Orca's pane key "<tabId>:<leafId>" — what worktree.ps names agents by and what
// Orca puts in the terminal's processes as ORCA_PANE_KEY; the terminal's handle and incarnation (what the runtime's
// terminal.* calls take) are looked up from it. Spaces are worktrees, slots their tabs.
//
// Phase 1: listing, state (Orca's own hooks, polled), plain-text reads, typing, keys, prompts, process → session,
// closing. No colors, no history, no live stream, no starting or renaming. The socket pushes nothing: no Watcher, the
// node polls every second.
//
// Orca writes whatever it is sent into the terminal, and pastes a prompt only when it recognises an agent in front:
// so every write that carries text first makes sure the agent is in front (front), and refuses with nothing typed
// when it cannot tell.
type orcaHost struct {
	c *orcart.Client
	// scan lists the agent processes in front in Orca's terminals, by pane key (scanOrcaProcs).
	scan func() (map[string][]orcaProc, error)

	mu       sync.Mutex
	terms    map[string]orcart.Terminal // pane key -> terminal, from the last terminal.list
	procs    map[string][]orcaProc
	procsErr error
	procsAt  time.Time
	failedAt time.Time // when the last terminal.list failed (failErr), for Agents right after a failed Snapshot
	failErr  error
	// The layouts (tabs) are costly for Orca: asked for only when the terminals or a worktree's topology changed, or
	// every orcaLayoutMaxAge.
	tabs     []orcart.Tab
	tabsAt   time.Time
	topology string // the terminal set and topology revisions the tabs were read for
	// worktree.ps answers "unchanged" to its last snapshot id: the worktrees are kept from the last full answer.
	trees    []orcart.Worktree
	treesID  string
	fallback map[string]orcaFallback // pane key -> terminal.agentStatus, for agent panes without a row
	view     *orcaView               // the last look, reused by Agents right after a Snapshot
	viewAt   time.Time
	launches map[string]orcaLaunch // fresh panes whose shell may receive one launch command
}

var (
	_ host.Host        = (*orcaHost)(nil)
	_ host.PlainScreen = (*orcaHost)(nil)
	_ host.Organizer   = (*orcaHost)(nil)
	_ host.Launcher    = (*orcaHost)(nil)
)

func newOrcaHost(c *orcart.Client) *orcaHost { return &orcaHost{c: c, scan: orcaScan} }

// orcaScan is how a new Orca host lists agent processes (tests swap in a fake process table).
var orcaScan = scanOrcaProcs

// Orca's pane variables in the processes of its terminals: the pane key, and the alias Claude Code does not scrub.
const (
	orcaPaneEnv       = nodeclient.OrcaPaneEnv
	orcaLegacyPaneEnv = nodeclient.OrcaLegacyPaneEnv
)

const (
	// orcaProcsMaxAge: how long one scan of the processes serves the listing (it polls every second).
	orcaProcsMaxAge = 1500 * time.Millisecond
	// orcaLayoutMaxAge: the layouts are read again at least this often (a tab renamed changes no revision).
	orcaLayoutMaxAge = 30 * time.Second
	// orcaFallbackMaxAge: how long a terminal.agentStatus answer serves a pane without a worktree.ps row.
	orcaFallbackMaxAge = 3 * time.Second
	// orcaViewReuse: Agents right after a Snapshot reuses its look instead of asking Orca again.
	orcaViewReuse = 500 * time.Millisecond
	// orcaRowSlack: a worktree.ps row last updated this long before its pane's agent started is the previous agent's.
	orcaRowSlack = 2 * time.Second
)

func (h *orcaHost) Kind() string      { return host.Orca }
func (h *orcaHost) PaneEnv() string   { return orcaPaneEnv }
func (h *orcaHost) PlainScreen() bool { return true }

func (h *orcaHost) Version(ctx context.Context) string { return h.c.Version(ctx) }

// orcaErr marks Orca not running as host.ErrNotRunning: it then has no terminals.
func orcaErr(err error) error {
	if errors.Is(err, orcart.ErrNotRunning) {
		return host.Mark(err, host.ErrNotRunning)
	}
	return err
}

// terminals are this machine's terminals in Orca (it also lists terminals on SSH targets, which are not here), with
// the layout's tabs (read again only when something changed). A list that failed a moment ago fails again at once:
// the node asks for Agents right after a failed Snapshot, and a wedged Orca must not cost it twice.
func (h *orcaHost) terminals(ctx context.Context) ([]orcart.Terminal, []orcart.Tab, error) {
	h.mu.Lock()
	if h.failErr != nil && time.Since(h.failedAt) < time.Second {
		err := h.failErr
		h.mu.Unlock()
		return nil, nil, err
	}
	h.mu.Unlock()
	l, err := h.c.Terminals(ctx, false)
	if err == nil {
		key := orcaTopology(l)
		h.mu.Lock()
		stale := key != h.topology || time.Since(h.tabsAt) > orcaLayoutMaxAge
		h.mu.Unlock()
		if stale {
			if l, err = h.c.Terminals(ctx, true); err == nil {
				h.mu.Lock()
				h.tabs, h.tabsAt, h.topology = l.Tabs, time.Now(), orcaTopology(l)
				h.mu.Unlock()
			}
		}
	}
	if err != nil {
		err = orcaErr(err)
		h.mu.Lock()
		h.failedAt, h.failErr = time.Now(), err
		h.mu.Unlock()
		return nil, nil, err
	}
	var terms []orcart.Terminal
	byPane := map[string]orcart.Terminal{}
	for _, t := range l.Terminals {
		if t.IsLocal() && !t.Orphaned && t.TabID != "" && t.LeafID != "" {
			terms = append(terms, t)
			byPane[t.PaneKey()] = t
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failErr, h.terms = nil, byPane
	return terms, h.tabs, nil
}

// orcaTopology is what decides whether the layouts must be read again: the terminals (by pane) and each worktree's
// topology revision.
func orcaTopology(l *orcart.TerminalList) string {
	var parts []string
	for _, t := range l.Terminals {
		parts = append(parts, t.WorktreeID+"|"+t.PaneKey())
	}
	for _, w := range slices.Sorted(maps.Keys(l.TopologyRevisions)) {
		parts = append(parts, w+"#"+strconv.FormatInt(l.TopologyRevisions[w], 10))
	}
	slices.Sort(parts)
	return strings.Join(parts, "\n")
}

// worktrees is worktree.ps, the last full answer when Orca says nothing changed since. An error leaves the rows out:
// the panes are still listed, their states from the fallback.
func (h *orcaHost) worktrees(ctx context.Context) []orcart.Worktree {
	h.mu.Lock()
	after := h.treesID
	h.mu.Unlock()
	trees, id, unchanged, err := h.c.Worktrees(ctx, after)
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case err != nil:
		h.treesID = ""
		return nil
	case unchanged:
		h.treesID = id
	default:
		h.trees, h.treesID = trees, id
	}
	return h.trees
}

// orcaView is Orca's state at one moment: its terminals and the agents its hooks report, by pane key.
type orcaView struct {
	terms []orcart.Terminal
	tabs  []orcart.Tab
	trees []orcart.Worktree
	rows  map[string]orcart.AgentRow
	procs map[string][]orcaProc
	// procsKnown: the process scan worked (else the agent rows and Orca's agentIdentity are all there is).
	procsKnown bool
	// fallback: terminal.agentStatus of agent panes without a usable row.
	fallback map[string]string
}

func (h *orcaHost) look(ctx context.Context) (*orcaView, error) {
	terms, tabs, err := h.terminals(ctx)
	if err != nil {
		return nil, err
	}
	v := &orcaView{terms: terms, tabs: tabs, rows: map[string]orcart.AgentRow{}, fallback: map[string]string{}}
	v.trees = h.worktrees(ctx)
	for _, w := range v.trees {
		for _, a := range w.Agents {
			v.rows[a.PaneKey] = a
		}
	}
	procs, err := h.agentProcs(orcaProcsMaxAge)
	v.procs, v.procsKnown = procs, err == nil
	// Agent panes Orca's hooks have no (current) row for: Orca's own reading of the terminal.
	for _, t := range terms {
		if a := v.agent(t); a.Agent != "" && a.AgentStatus == proto.StatusUnknown {
			if s := h.fallbackStatus(ctx, t); s != "" {
				v.fallback[t.PaneKey()] = s
			}
		}
	}
	h.mu.Lock()
	h.view, h.viewAt = v, time.Now()
	h.mu.Unlock()
	return v, nil
}

// orcaFallback is one terminal.agentStatus answer, as the node's status.
type orcaFallback struct {
	status string
	at     time.Time
}

// orcaAgentStatus maps terminal.agentStatus's states to the node's.
var orcaAgentStatus = map[string]string{"working": proto.StatusWorking, "permission": proto.StatusBlocked, "idle": proto.StatusDone}

// fallbackStatus is a pane's state from terminal.agentStatus (cached a few seconds), "" when Orca does not know.
func (h *orcaHost) fallbackStatus(ctx context.Context, t orcart.Terminal) string {
	key := t.PaneKey()
	h.mu.Lock()
	f, ok := h.fallback[key]
	h.mu.Unlock()
	if ok && time.Since(f.at) < orcaFallbackMaxAge {
		return f.status
	}
	st, err := h.c.AgentStatus(ctx, t.Target())
	f = orcaFallback{at: time.Now()}
	if err == nil && st.IsRunningAgent {
		f.status = orcaAgentStatus[st.Status]
	}
	h.mu.Lock()
	if h.fallback == nil {
		h.fallback = map[string]orcaFallback{}
	}
	h.fallback[key] = f
	h.mu.Unlock()
	return f.status
}

func (h *orcaHost) Snapshot(ctx context.Context) (*host.Layout, error) {
	v, err := h.look(ctx)
	if err != nil {
		return nil, err
	}
	l := &host.Layout{}
	spaces := map[string]bool{}
	addSpace := func(id, path, branch, parent string) {
		if spaces[id] || id == "" {
			return
		}
		spaces[id] = true
		l.Spaces = append(l.Spaces, host.Space{ID: id, Label: orcaSpaceLabel(branch, path), Number: len(l.Spaces) + 1,
			Cwd: path, Branch: strings.TrimPrefix(branch, "refs/heads/"), ParentID: orcaParent(id, parent)})
	}
	// Orca's sidebar order (worktree.ps), then any worktree only the terminals name.
	for _, w := range v.trees {
		addSpace(w.WorktreeID, w.Path, w.Branch, w.ParentWorktreeID)
	}
	for _, t := range v.terms {
		addSpace(t.WorktreeID, t.WorktreePath, t.Branch, "")
	}
	perSpace := map[string]int{}
	seen := map[string]bool{}
	for _, tab := range v.tabs {
		if !spaces[tab.WorktreeID] || seen[tab.ID] {
			continue
		}
		seen[tab.ID] = true
		perSpace[tab.WorktreeID]++
		label := stripSpinner(tab.Title)
		if label == "" {
			label = strconv.Itoa(perSpace[tab.WorktreeID])
		}
		l.Slots = append(l.Slots, host.Slot{ID: tab.ID, SpaceID: tab.WorktreeID, Label: label, Number: perSpace[tab.WorktreeID]})
	}
	for _, t := range v.terms {
		a := v.agent(t)
		l.Panes = append(l.Panes, a)
		if a.Agent != "" {
			l.Agents = append(l.Agents, a)
		}
	}
	return l, nil
}

// Spaces reads the complete worktree catalog, including worktrees with no terminal.
// Unlike agent discovery's best-effort hook rows, catalog failures must reach the caller.
func (h *orcaHost) Spaces(ctx context.Context) ([]host.Space, error) {
	trees, _, _, err := h.c.Worktrees(ctx, "")
	if err != nil {
		return nil, orcaErr(err)
	}
	out := make([]host.Space, 0, len(trees))
	for _, w := range trees {
		if w.WorktreeID != "" {
			out = append(out, host.Space{ID: w.WorktreeID, Label: orcaSpaceLabel(w.Branch, w.Path),
				Number: len(out) + 1, Cwd: w.Path, Branch: strings.TrimPrefix(w.Branch, "refs/heads/"), ParentID: orcaParent(w.WorktreeID, w.ParentWorktreeID), DisplayName: w.DisplayName})
		}
	}
	return out, nil
}

// orcaParent is a worktree's parent space, never itself.
func orcaParent(id, parent string) string {
	if parent == id {
		return ""
	}
	return parent
}

// orcaSpaceLabel names a worktree the way Orca's sidebar does: its branch, else its folder.
func orcaSpaceLabel(branch, path string) string {
	if b := strings.TrimPrefix(branch, "refs/heads/"); b != "" {
		return b
	}
	return firstOf(filepath.Base(path))
}

// Agents is the agent panes: from the look a Snapshot just took, else a new one.
func (h *orcaHost) Agents(ctx context.Context) ([]host.Agent, error) {
	h.mu.Lock()
	v := h.view
	if time.Since(h.viewAt) > orcaViewReuse {
		v = nil
	}
	h.mu.Unlock()
	if v == nil {
		var err error
		if v, err = h.look(ctx); err != nil {
			return nil, err
		}
	}
	var out []host.Agent
	for _, t := range v.terms {
		if a := v.agent(t); a.Agent != "" {
			out = append(out, a)
		}
	}
	return out, nil
}

func (h *orcaHost) Agent(ctx context.Context, pane host.PaneRef) (*host.Agent, error) {
	v, err := h.look(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range v.terms {
		if t.PaneKey() == pane.ID {
			a := v.agent(t)
			return &a, nil
		}
	}
	return nil, fmt.Errorf("%w %s", errNoOrcaTerminal, pane.ID)
}

// orcaAgents names the agents Orca reports (agentType, agentIdentity) the way the node does.
var orcaAgents = map[string]string{"claude": "claude", "codex": "codex"}

// orcaStatus maps Orca's agent states to the node's: waiting is Orca's word for an agent waiting on the user, as
// blocked is.
var orcaStatus = map[string]string{
	"working": proto.StatusWorking, "blocked": proto.StatusBlocked, "waiting": proto.StatusBlocked, "done": proto.StatusDone,
}

// agent is a terminal as an agent pane. Its agent is the claude or codex process in front in it (by its
// ORCA_PANE_KEY; not suspended, not in the background, not inside a herdr nested in the terminal): Orca keeps an
// agent's row (and its identity on the terminal) after the agent exits, so neither alone says one runs there. Only
// when the processes could not be listed do the row and the identity decide (for listing; writes are refused then).
//
// Its state is the row's main agent's own (a claude whose turn ended while its background subagents run is done, as
// herdr would say), except that a row blocked by a subagent waiting on the user is blocked (AgentRow.PaneState), when
// the row is of that agent and not older than its process; else terminal.agentStatus's.
func (v *orcaView) agent(t orcart.Terminal) host.Agent {
	key := t.PaneKey()
	a := host.Agent{
		PaneID: host.PaneRef{Kind: host.Orca, ID: key}.String(), WorkspaceID: t.WorktreeID, TabID: t.TabID,
		Cwd: t.WorktreePath, Title: stripSpinner(t.Title), RawTitle: t.Title,
	}
	row, hasRow := v.rows[key]
	var started time.Time
	if v.procsKnown {
		if ps := v.procs[key]; len(ps) > 0 {
			a.Agent, a.ForegroundCwd, started = ps[0].agent, ps[0].cwd, ps[0].start
		}
	} else if k := orcaAgents[t.AgentIdentity]; k != "" && hasRow && row.HasLivePty && orcaAgents[row.AgentType] == k {
		a.Agent = k
	}
	if a.Agent == "" {
		return a
	}
	a.AgentStatus = proto.StatusUnknown
	current := hasRow && orcaAgents[row.AgentType] == a.Agent &&
		(started.IsZero() || row.UpdatedAt == 0 || time.UnixMilli(row.UpdatedAt).After(started.Add(-orcaRowSlack)))
	if s := orcaStatus[row.PaneState()]; current && s != "" {
		a.AgentStatus = s
	} else if s := v.fallback[key]; s != "" {
		a.AgentStatus = s
	}
	return a
}

// errNoOrcaTerminal: Orca lists no terminal with that pane key (closed, or Orca restarted).
var errNoOrcaTerminal = errors.New("orca: no terminal")

// terminal is the pane's terminal: from the last listing, else from a new one (fresh: always a new one).
func (h *orcaHost) terminal(ctx context.Context, paneKey string, fresh bool) (orcart.Terminal, error) {
	if !fresh {
		h.mu.Lock()
		t, ok := h.terms[paneKey]
		h.mu.Unlock()
		if ok {
			return t, nil
		}
	}
	if _, _, err := h.terminals(ctx); err != nil {
		return orcart.Terminal{}, err
	}
	h.mu.Lock()
	t, ok := h.terms[paneKey]
	h.mu.Unlock()
	if !ok {
		return orcart.Terminal{}, host.Mark(fmt.Errorf("%w %s", errNoOrcaTerminal, paneKey), host.ErrNotDelivered)
	}
	return t, nil
}

// onTerminal runs fn on the pane's terminal. A handle Orca no longer knows (the terminal came back under a new one) is
// looked up again once; for a write only when the terminal still runs the same process (incarnation), so a write
// never lands in a new process behind the pane — that is refused, nothing typed.
func (h *orcaHost) onTerminal(ctx context.Context, t orcart.Terminal, write bool, fn func(orcart.Target) error) error {
	err := fn(t.Target())
	if !orcart.IsCode(err, "terminal_handle_stale") {
		return err
	}
	t2, lerr := h.terminal(ctx, t.PaneKey(), true)
	if lerr != nil {
		return lerr
	}
	if write && (t.IncarnationID == "" || t2.IncarnationID != t.IncarnationID) {
		return host.Mark(fmt.Errorf("orca: the terminal in %s runs another process now", t.PaneKey()), host.ErrNotDelivered)
	}
	return fn(t2.Target())
}

// errNotInFront: no agent is in front in the pane (it exited, is suspended, runs in the background or inside a nested
// host): text would go to whatever is in front instead (a shell would run it).
var errNotInFront = errors.New("no agent is in front in the pane")

// front makes sure an agent is in front in the pane before text goes in, and returns its terminal: our own process
// scan finds a claude or codex in front with this pane key (not suspended, not in the background, not inside a nested
// host). Anything else — including not being able to tell — refuses with nothing typed. Orca's terminal.agentStatus
// is asked too, for a permission dialog (prompt: the text is a prompt, which the dialog would take as its answer); its
// isRunningAgent is not required on macOS and Linux: Orca says false while Claude Code has background tasks, though
// its input box takes text then (#5463), and the scan already tells what is in front of the pane's own pty (ptyOwner).
// On Windows, whose scan cannot tell, it still is.
func (h *orcaHost) front(ctx context.Context, paneKey string, prompt bool) (orcart.Terminal, error) {
	t, err := h.terminal(ctx, paneKey, false)
	if err != nil {
		return t, orcaInputErr(err)
	}
	procs, err := h.agentProcs(0)
	if err != nil {
		return t, host.Mark(fmt.Errorf("orca: cannot tell what runs in %s: %w", paneKey, err), host.ErrNotDelivered)
	}
	if len(procs[paneKey]) == 0 {
		return t, host.Mark(fmt.Errorf("orca: %w %s", errNotInFront, paneKey), host.ErrNotDelivered)
	}
	var st *orcart.AgentStatus
	err = h.onTerminal(ctx, t, false, func(tg orcart.Target) error {
		var e error
		st, e = h.c.AgentStatus(ctx, tg)
		return e
	})
	if err != nil {
		return t, host.Mark(fmt.Errorf("orca: cannot tell what runs in %s: %w", paneKey, err), host.ErrNotDelivered)
	}
	if !st.IsRunningAgent && runtime.GOOS == "windows" {
		// Windows' scan has no foreground or stopped state to tell by (scan_windows.go): Orca's answer stays the gate.
		return t, host.Mark(fmt.Errorf("orca: %w %s (Orca sees none)", errNotInFront, paneKey), host.ErrNotDelivered)
	}
	if prompt && st.Status == "permission" {
		return t, host.Mark(host.Mark(fmt.Errorf("orca: the agent in %s waits on a permission dialog", paneKey),
			host.ErrNotDelivered), host.ErrDialogOpen)
	}
	return t, nil
}

// Read is terminal.read of the visible screen as plain text: Orca reads no colors here, and the screen read is the one
// that never switches source, so "recent" is the screen too. Lines cuts it to its last rows.
func (h *orcaHost) Read(ctx context.Context, pane host.PaneRef, o host.ReadOpts) (string, error) {
	if o.ANSI {
		return "", fmt.Errorf("%w: Orca reads screens as plain text only", host.ErrUnsupported)
	}
	t, err := h.terminal(ctx, pane.ID, false)
	if err != nil {
		return "", orcaErr(err)
	}
	var text string
	err = h.onTerminal(ctx, t, false, func(tg orcart.Target) error {
		s, err := h.c.Read(ctx, tg)
		if err != nil {
			return err
		}
		text = s.Text()
		return nil
	})
	if err != nil {
		return "", orcaErr(err)
	}
	if o.Lines > 0 {
		if rows := strings.Split(strings.TrimRight(text, "\n"), "\n"); len(rows) > o.Lines {
			text = strings.Join(rows[len(rows)-o.Lines:], "\n") + "\n"
		}
	}
	return text, nil
}

// SendText types text as is (terminal.send without enter writes the bytes), once the agent is in front.
func (h *orcaHost) SendText(ctx context.Context, pane host.PaneRef, text string) error {
	t, err := h.front(ctx, pane.ID, false)
	if err != nil {
		return err
	}
	return orcaInputErr(h.onTerminal(ctx, t, true, func(tg orcart.Target) error { return h.c.Send(ctx, tg, text) }))
}

// orcaKeyGap: keys go one write each, a moment apart, so an escape is not read as the start of the next key's
// sequence (esc then shift+tab would arrive as alt+escape-sequence).
const orcaKeyGap = 40 * time.Millisecond

// SendKeys presses the keys one by one (their bytes, orcaKeyBytes); a key with no bytes fails before anything is
// pressed. Keys that type text (letters, digits, space) need the agent in front, as SendText does; the others (enter,
// esc, arrows, ctrl+c…) go to whatever is there, so a stuck pane can still be interrupted and closed.
func (h *orcaHost) SendKeys(ctx context.Context, pane host.PaneRef, keys ...string) error {
	seq := make([]string, 0, len(keys))
	typing := false
	for _, k := range keys {
		b, ok := orcaKeyBytes(k)
		if !ok {
			return host.Mark(fmt.Errorf("orca: unknown key %q", k), host.ErrNotDelivered)
		}
		typing = typing || orcaTypes(b)
		seq = append(seq, b)
	}
	var t orcart.Terminal
	var err error
	if typing {
		t, err = h.front(ctx, pane.ID, false)
	} else {
		t, err = h.terminal(ctx, pane.ID, false)
	}
	if err != nil {
		return orcaInputErr(err)
	}
	for i, b := range seq {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err() // some keys were pressed
			case <-time.After(orcaKeyGap):
			}
		}
		err := h.onTerminal(ctx, t, true, func(tg orcart.Target) error { return h.c.Send(ctx, tg, b) })
		if err != nil {
			if i > 0 {
				return orcaErr(err) // some keys were pressed: not safe to send again
			}
			return orcaInputErr(err)
		}
	}
	return nil
}

// Bracketed paste markers, and what an ESC inside the text becomes (Orca's own sanitizeAgentPromptText): an ESC in the
// text could end the paste early and turn the rest into keys.
const (
	pasteStart  = "\x1b[200~"
	pasteEnd    = "\x1b[201~"
	inertEscape = "<ESC>"
)

// orcaPasteGap is how long the agent gets to take a paste before the enter that submits it: a base, a little more per
// line, capped.
func orcaPasteGap(text string) time.Duration {
	return min(orcaSubmitGap+time.Duration(strings.Count(text, "\n"))*10*time.Millisecond, time.Second)
}

// orcaSubmitGap is the base of orcaPasteGap.
var orcaSubmitGap = 150 * time.Millisecond

// Prompt submits text to the agent the way herdr's agent.prompt does: a bracketed paste of the whole text (newlines
// stay in the input instead of each submitting), then a separate carriage return. Orca's own agentPrompt path is not
// used: Orca pastes only for an agent it has seen working, and writes a fresh agent's prompt raw, so a multi-line
// message would be submitted at its first newline.
//
// The agent must be in front (front) before each write: Orca writes whatever it is sent into whatever is there. A
// refusal before the paste typed nothing (host.ErrNotDelivered); anything failing after it leaves the text possibly
// sitting in the input (not marked: delivered unknown).
func (h *orcaHost) Prompt(ctx context.Context, pane host.PaneRef, text string) error {
	t, err := h.front(ctx, pane.ID, true)
	if err != nil {
		return err
	}
	paste := pasteStart + strings.ReplaceAll(text, "\x1b", inertEscape) + pasteEnd
	if err := h.onTerminal(ctx, t, true, func(tg orcart.Target) error { return h.c.Send(ctx, tg, paste) }); err != nil {
		return orcaInputErr(err)
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("orca: the text was pasted into %s but not submitted: %v", pane.ID, ctx.Err())
	case <-time.After(orcaPasteGap(text)):
	}
	afterPaste := func(err error) error {
		return fmt.Errorf("orca: the text was pasted into %s but not submitted: %v", pane.ID, err) // not marked: unknown
	}
	if t, err = h.front(ctx, pane.ID, false); err != nil {
		return afterPaste(err)
	}
	if err := h.onTerminal(ctx, t, true, func(tg orcart.Target) error { return h.c.Send(ctx, tg, "\r") }); err != nil {
		return afterPaste(err)
	}
	return nil
}

// orcaInputErr marks the refusals that typed nothing (host.ErrNotDelivered): Orca not running or incompatible, a
// terminal that is gone, exited, replaced or not writable, a request it rejected before writing, a send it turned
// down (accepted false; "permission": the agent waits on a dialog, also host.ErrDialogOpen).
func orcaInputErr(err error) error {
	var r *orcart.Refusal
	switch {
	case err == nil:
		return nil
	case errors.Is(err, host.ErrNotDelivered):
		return err
	case errors.As(err, &r):
		if r.Reason == "permission" {
			return host.Mark(host.Mark(err, host.ErrNotDelivered), host.ErrDialogOpen)
		}
		return host.Mark(err, host.ErrNotDelivered)
	case errors.Is(err, orcart.ErrNotRunning), errors.Is(err, orcart.ErrIncompatible), orcart.Refused(err):
		return host.Mark(orcaErr(err), host.ErrNotDelivered)
	}
	return err
}

// errOrcaRename: Orca owns its worktrees' and tabs' names; Persona does not rename them.
var errOrcaRename = fmt.Errorf("%w: Orca 里的工作区和标签页暂时不能从手机改名", host.ErrUnsupported)

func (h *orcaHost) RenameSpace(context.Context, string, string) error { return errOrcaRename }
func (h *orcaHost) RenameSlot(context.Context, string, string) error  { return errOrcaRename }

// Close is terminal.close: Orca closes the pane (its tab when it was the only one). A terminal already gone counts as
// closed.
func (h *orcaHost) Close(ctx context.Context, pane host.PaneRef, _, _ string) error {
	h.mu.Lock()
	delete(h.launches, pane.ID)
	h.mu.Unlock()
	t, err := h.terminal(ctx, pane.ID, false)
	if err == nil {
		err = h.onTerminal(ctx, t, true, func(tg orcart.Target) error { return h.c.Close(ctx, tg) })
	}
	if errors.Is(err, errNoOrcaTerminal) || orcart.IsCode(err, "terminal_handle_stale") ||
		orcart.IsCode(err, "terminal_gone") || orcart.IsCode(err, "terminal_exited") {
		return nil // no terminal with that pane key any more
	}
	return orcaErr(err)
}

// PaneProcesses are the agent processes in front whose ORCA_PANE_KEY is the pane, claude first.
func (h *orcaHost) PaneProcesses(ctx context.Context, pane host.PaneRef) ([]int, error) {
	procs, err := h.agentProcs(orcaProcsMaxAge)
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, p := range procs[pane.ID] {
		pids = append(pids, p.pid)
	}
	return pids, nil
}

// agentProcs is a scan of the agent processes no older than maxAge.
func (h *orcaHost) agentProcs(maxAge time.Duration) (map[string][]orcaProc, error) {
	h.mu.Lock()
	if h.procsAt.IsZero() || time.Since(h.procsAt) > maxAge {
		h.mu.Unlock()
		procs, err := h.scan()
		h.mu.Lock()
		h.procs, h.procsErr, h.procsAt = procs, err, time.Now()
	}
	defer h.mu.Unlock()
	return h.procs, h.procsErr
}

// orcaProc is an agent process in front in one of Orca's terminals.
type orcaProc struct {
	pid   int
	agent string // claude | codex
	cwd   string
	start time.Time // when it started (to the second; zero when unknown)
}

var (
	orcaPaneRe       = paneEnvRe(orcaPaneEnv)
	orcaLegacyPaneRe = paneEnvRe(orcaLegacyPaneEnv)
	// A process with a pane of a host nested in Orca's terminal (herdr, herdr-core) belongs to that host: it inherited
	// the Orca terminal's ORCA_PANE_KEY, but Orca's terminal shows the nested host, not it. Other nested ptys (tmux,
	// zellij, screen, script…) are told by their owner instead (ptyOwner), not by a variable: Orca's own Claude agent
	// teams set TMUX in a real pane.
	nestedPaneRes = []*regexp.Regexp{paneEnvRe(herdrPaneEnv), paneEnvRe(herdrCorePaneEnv)}
)

// orcaPaneOf is the Orca pane key in a process's environment listing ("" when none, or when the process runs in a
// host nested inside Orca's terminal): ORCA_PANE_KEY, else ORCA_AGENT_PANE. Nothing else is taken from it (it also
// holds Orca's hook token).
func orcaPaneOf(env string) string {
	for _, re := range nestedPaneRes {
		if re.MatchString(env) {
			return ""
		}
	}
	if m := orcaPaneRe.FindStringSubmatch(env); m != nil {
		return m[1]
	}
	if m := orcaLegacyPaneRe.FindStringSubmatch(env); m != nil {
		return m[1]
	}
	return ""
}

// agentOfCmd: the command line runs Claude Code or Codex.
func agentOfCmd(argv []string) string {
	if isClaudeCmd(argv) {
		return "claude"
	}
	if len(argv) == 0 {
		return ""
	}
	switch progName(argv[0]) {
	case "codex":
		return "codex"
	case "node", "bun":
		if len(argv) > 1 && (progName(argv[1]) == "codex" || strings.Contains(filepath.ToSlash(argv[1]), "/codex/")) {
			return "codex"
		}
	}
	return ""
}

// scanOrcaProcs lists this user's claude and codex processes in front in Orca's terminals, by pane key: not stopped,
// in their terminal's foreground process group (a suspended or background agent is not what the terminal shows), not
// inside a nested herdr / herdr-core. Claude first, lowest pid first within an agent (a wrapper before the program it
// starts). Linux reads /proc; macOS asks ps twice: the command lines of all, then the environments of the agents only;
// Windows reads the process table (scanOrcaProcsOS, which has its own idea of in front).
func scanOrcaProcs() (map[string][]orcaProc, error) {
	switch runtime.GOOS {
	case "linux":
		return scanOrcaProcsLinux()
	case "windows":
		return scanOrcaProcsOS()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "ps", "-ww", "-o", "pid=,stat=,etime=,args=", "-U", strconv.Itoa(os.Getuid())).Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	cands := parseOrcaPSArgs(string(b), time.Now())
	if len(cands) == 0 {
		return map[string][]orcaProc{}, nil
	}
	// Every process (not only the user's: /usr/bin/login sits between Orca's terminal host and the shell), for the
	// owner of each candidate's pty (ptyOwner).
	t, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=,tty=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps -A: %w", err)
	}
	tree := parsePSTree(string(t))
	owners := map[int]int{}
	pids := make([]string, 0, 2*len(cands))
	for pid := range cands {
		pids = append(pids, strconv.Itoa(pid))
		if q := ptyOwner(tree, pid); q > 0 {
			owners[pid] = q
			pids = append(pids, strconv.Itoa(q))
		}
	}
	slices.Sort(pids)
	pids = slices.Compact(pids)
	b, err = exec.CommandContext(ctx, "ps", "-E", "-ww", "-o", "pid=,command=", "-p", strings.Join(pids, ",")).Output()
	if err != nil && len(b) == 0 { // ps exits 1 when one of the pids is gone; the others are still listed
		return nil, fmt.Errorf("ps -E: %w", err)
	}
	return parseOrcaPSEnv(string(b), cands, owners), nil
}

// psNode is a process in `ps -A -o pid=,ppid=,tty=`: its parent and its controlling terminal ("??" for none).
type psNode struct {
	ppid int
	tty  string
}

func parsePSTree(s string) map[int]psNode {
	out := map[int]psNode{}
	for line := range strings.SplitSeq(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			out[pid] = psNode{ppid: ppid, tty: f[2]}
		}
	}
	return out
}

// ptyOwner is the process that owns pid's pty: the first ancestor on another terminal (or none). For an agent in an
// Orca pane that is Orca's terminal host; for one inside tmux, zellij, screen, script or any other pty a program in
// the pane made, it is that program, which inherited the pane's ORCA_PANE_KEY (#5463). 0 when the chain is broken.
func ptyOwner(tree map[int]psNode, pid int) int {
	tty := tree[pid].tty
	if tty == "" || tty == "??" {
		return 0
	}
	for cur, i := pid, 0; i < 64; i++ {
		p := tree[cur].ppid
		node, ok := tree[p]
		if !ok || p == cur {
			return 0
		}
		if node.tty != tty {
			return p
		}
		cur = p
	}
	return 0
}

// parseOrcaPSArgs reads `ps -o pid=,stat=,etime=,args=`: the claude / codex processes in front (stat holds "+", the
// foreground process group, and does not start with T, stopped), with their start times.
func parseOrcaPSArgs(s string, now time.Time) map[int]orcaProc {
	out := map[int]orcaProc{}
	for line := range strings.SplitSeq(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		stat := f[1]
		if !strings.Contains(stat, "+") || strings.HasPrefix(stat, "T") {
			continue
		}
		agent := agentOfCmd(f[3:])
		if agent == "" {
			continue
		}
		p := orcaProc{pid: pid, agent: agent}
		if d, ok := parseEtime(f[2]); ok {
			p.start = now.Add(-d).Truncate(time.Second)
		}
		out[pid] = p
	}
	return out
}

// parseEtime reads ps's elapsed time, [[dd-]hh:]mm:ss.
func parseEtime(s string) (time.Duration, bool) {
	var days int
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, false
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, false
		}
		total = total*60 + n
	}
	return time.Duration(days)*24*time.Hour + time.Duration(total)*time.Second, true
}

// parseOrcaPSEnv reads `ps -E -o pid=,command=` of the candidates and their pty owners (the command line followed by
// the environment): each candidate's Orca pane. owners (candidate → ptyOwner; nil: not checked) drops a candidate
// whose pty is not the pane's own: its owner carries the same pane key (a nested pty), its owner's environment cannot
// be read (ps -E shows none for Apple's own binaries, /usr/bin/script and screen among them: unknown is not "no
// key"), or it has no known owner.
func parseOrcaPSEnv(s string, cands map[int]orcaProc, owners map[int]int) map[string][]orcaProc {
	panes, readable := map[int]string{}, map[int]bool{}
	for line := range strings.SplitSeq(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if pid, err := strconv.Atoi(f[0]); err == nil {
			panes[pid] = orcaPaneOf(line)
			readable[pid] = strings.Contains(line, " HOME=") // every login environment has it
		}
	}
	out := map[string][]orcaProc{}
	for pid, p := range cands {
		pane := panes[pid]
		if pane == "" {
			continue
		}
		if owners != nil {
			if q := owners[pid]; q == 0 || !readable[q] || panes[q] == pane {
				continue
			}
		}
		out[pane] = append(out[pane], p)
	}
	sortOrcaProcs(out)
	return out
}

// scanOrcaProcsLinux is scanOrcaProcs from /proc: a process is in front when its process group is its terminal's
// foreground group (stat's pgrp = tpgid).
func scanOrcaProcsLinux() (map[string][]orcaProc, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	out := map[string][]orcaProc{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := "/proc/" + e.Name()
		cmd, err := os.ReadFile(dir + "/cmdline")
		if err != nil {
			continue
		}
		agent := agentOfCmd(strings.Split(strings.TrimRight(string(cmd), "\x00"), "\x00"))
		if agent == "" {
			continue
		}
		stat, err := os.ReadFile(dir + "/stat")
		if err != nil || !procInFront(string(stat)) {
			continue
		}
		env, _ := os.ReadFile(dir + "/environ")
		if pane := orcaPaneOf(string(env)); pane != "" {
			// A nested pty (tmux, zellij, script…) a program in the pane made: its owner carries the pane key too.
			q := ptyOwnerLinux(pid)
			if q == 0 {
				continue
			}
			// Unreadable (a setuid / setgid owner such as screen is not dumpable) is not "no key": not taken.
			if qenv, err := os.ReadFile("/proc/" + strconv.Itoa(q) + "/environ"); err != nil || len(qenv) == 0 ||
				orcaPaneOf(string(qenv)) == pane {
				continue
			}
			p := orcaProc{pid: pid, agent: agent}
			p.cwd, _ = os.Readlink(dir + "/cwd")
			if st, err := os.Stat(dir); err == nil {
				p.start = st.ModTime()
			}
			out[pane] = append(out[pane], p)
		}
	}
	sortOrcaProcs(out)
	return out, nil
}

// statPPIDTTY reads /proc/<pid>/stat's ppid and tty_nr.
func statPPIDTTY(pid int) (ppid int, tty string, ok bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, "", false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, "", false
	}
	f := strings.Fields(s[i+1:]) // state ppid pgrp session tty_nr …
	if len(f) < 5 {
		return 0, "", false
	}
	ppid, err = strconv.Atoi(f[1])
	return ppid, f[4], err == nil
}

// ptyOwnerLinux is ptyOwner from /proc: the first ancestor of pid on another terminal, 0 when the chain is broken.
func ptyOwnerLinux(pid int) int {
	_, tty, ok := statPPIDTTY(pid)
	if !ok || tty == "0" {
		return 0
	}
	for cur, i := pid, 0; i < 64; i++ {
		p, _, ok := statPPIDTTY(cur)
		if !ok || p <= 0 || p == cur {
			return 0
		}
		_, t, ok := statPPIDTTY(p)
		if !ok {
			return 0
		}
		if t != tty {
			return p
		}
		cur = p
	}
	return 0
}

// procInFront reads /proc/<pid>/stat: not stopped, and its process group is the terminal's foreground group.
func procInFront(stat string) bool {
	i := strings.LastIndexByte(stat, ')')
	if i < 0 {
		return false
	}
	f := strings.Fields(stat[i+1:]) // state ppid pgrp session tty_nr tpgid …
	if len(f) < 6 || f[0] == "T" || f[0] == "t" {
		return false
	}
	return f[2] == f[5]
}

func sortOrcaProcs(m map[string][]orcaProc) {
	for _, ps := range m {
		slices.SortFunc(ps, func(a, b orcaProc) int {
			if (a.agent == "claude") != (b.agent == "claude") {
				if a.agent == "claude" {
					return -1
				}
				return 1
			}
			return a.pid - b.pid
		})
	}
}
