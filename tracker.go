package main

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
)

// Shells write these at the start of the terminal title (shell/cmd-status.*).
const (
	RunMark  = "⏵"
	DoneMark = "⏹"
)

type titleKind int

const (
	titleOther titleKind = iota
	titleRun
	titleDone
)

type parsedTitle struct {
	kind    titleKind
	command string
	code    int
}

func parseTitle(title string) parsedTitle {
	if cmd, ok := strings.CutPrefix(title, RunMark+" "); ok {
		return parsedTitle{kind: titleRun, command: cmd}
	}
	if rest, ok := strings.CutPrefix(title, DoneMark+" "); ok {
		n := 0
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		if code, err := strconv.Atoi(rest[:n]); err == nil {
			return parsedTitle{kind: titleDone, code: code}
		}
	}
	return parsedTitle{kind: titleOther}
}

func commandName(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return path.Base(fields[0])
}

// noCode marks a run that ended without an exit status.
const noCode = -1

func finishTitle(name string, code int) string {
	switch code {
	case noCode:
		return name + " finished"
	case 0:
		return name + " done"
	default:
		return fmt.Sprintf("%s failed (exit %d)", name, code)
	}
}

// TrackerConfig holds the settings and callbacks of a Tracker.
type TrackerConfig struct {
	Delay  time.Duration
	Skip   []string
	Notify bool
	// After runs fn once d has passed and returns a function that cancels it.
	After        func(d time.Duration, fn func()) (stop func() bool)
	Tokens       func(workspace string, set, clear []string)
	Notification func(title, body string)
	// Done lists the workspaces that already show the finished icon.
	Done []string
}

type pane struct {
	workspace string
	command   string
	name      string
	stop      func() bool // nil once the delay has passed
}

// Tracker turns title changes into token and notification calls. A command
// shows as running only after Delay, so short commands never flash an icon.
// It is not safe for concurrent use. Call it and its timers from one goroutine.
type Tracker struct {
	cfg     TrackerConfig
	panes   map[string]*pane
	running map[string]map[string]bool // workspace -> pane IDs
	// Lets a workspace switch skip the CLI call when there is no icon to clear.
	done    map[string]bool
	focused string
}

// NewTracker returns a Tracker for cfg.
func NewTracker(cfg TrackerConfig) *Tracker {
	t := &Tracker{
		cfg:     cfg,
		panes:   map[string]*pane{},
		running: map[string]map[string]bool{},
		done:    map[string]bool{},
	}
	for _, ws := range cfg.Done {
		t.done[ws] = true
	}
	return t
}

func (t *Tracker) skipped(name string) bool {
	for _, pattern := range t.cfg.Skip {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

func (t *Tracker) start(paneID, workspace, command string) {
	name := commandName(command)
	if name == "" || t.skipped(name) {
		return
	}
	p := &pane{workspace: workspace, command: command, name: name}
	p.stop = t.cfg.After(t.cfg.Delay, func() {
		// The timer can fire after its run ended, when stop came too late.
		if t.panes[paneID] != p || p.stop == nil {
			return
		}
		p.stop = nil
		if t.running[workspace] == nil {
			t.running[workspace] = map[string]bool{}
		}
		t.running[workspace][paneID] = true
		delete(t.done, workspace)
		t.cfg.Tokens(workspace, []string{"running"}, []string{"done"})
	})
	t.panes[paneID] = p
}

// finish ends the run of a pane. quiet means the pane is gone, so it only tidies up.
func (t *Tracker) finish(paneID string, code int, quiet bool) {
	p := t.panes[paneID]
	if p == nil {
		return
	}
	delete(t.panes, paneID)
	if p.stop != nil {
		p.stop()
		return
	}
	set := t.running[p.workspace]
	delete(set, paneID)
	var clear []string
	if len(set) == 0 {
		delete(t.running, p.workspace)
		clear = []string{"running"}
	}
	showDone := !quiet && p.workspace != t.focused
	var tokens []string
	if showDone {
		t.done[p.workspace] = true
		tokens = []string{"done"}
	}
	if showDone || len(clear) > 0 {
		t.cfg.Tokens(p.workspace, tokens, clear)
	}
	if showDone && t.cfg.Notify {
		t.cfg.Notification(finishTitle(p.name, code), p.command)
	}
}

// Title handles a new terminal title of a pane.
func (t *Tracker) Title(paneID, workspace, title string) {
	parsed := parseTitle(title)
	switch parsed.kind {
	case titleRun:
		if p := t.panes[paneID]; p != nil && p.command == parsed.command {
			return
		}
		t.finish(paneID, noCode, false)
		t.start(paneID, workspace, parsed.command)
	case titleDone:
		t.finish(paneID, parsed.code, false)
	default:
		t.finish(paneID, noCode, false)
	}
}

// PaneClosed drops the run of a closed pane.
func (t *Tracker) PaneClosed(paneID string) {
	t.finish(paneID, noCode, true)
}

// Focused handles a workspace switch.
func (t *Tracker) Focused(workspace string) {
	t.focused = workspace
	if t.done[workspace] {
		delete(t.done, workspace)
		t.cfg.Tokens(workspace, nil, []string{"done"})
	}
}
