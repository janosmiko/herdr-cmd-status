package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeClock struct {
	pending map[int]func()
	next    int
}

func (c *fakeClock) after(_ time.Duration, fn func()) func() bool {
	c.next++
	id := c.next
	c.pending[id] = fn
	return func() bool {
		_, ok := c.pending[id]
		delete(c.pending, id)
		return ok
	}
}

func (c *fakeClock) fire() {
	fns := c.pending
	c.pending = map[int]func(){}
	for _, fn := range fns {
		fn()
	}
}

type harness struct {
	tracker *Tracker
	clock   *fakeClock
	actions []string
}

func setup(t *testing.T, change func(*TrackerConfig)) *harness {
	t.Helper()
	h := &harness{clock: &fakeClock{pending: map[int]func(){}}}
	cfg := TrackerConfig{
		Delay:  5 * time.Second,
		Skip:   []string{"nvim", "ssh", "claude*"},
		Notify: true,
		After:  h.clock.after,
		Tokens: func(ws string, set, clear []string) {
			h.actions = append(h.actions, fmt.Sprintf("tokens %s set=%s clear=%s", ws, strings.Join(set, ","), strings.Join(clear, ",")))
		},
		Notification: func(title, body string) {
			h.actions = append(h.actions, fmt.Sprintf("notify %s: %s", title, body))
		},
	}
	if change != nil {
		change(&cfg)
	}
	h.tracker = NewTracker(cfg)
	return h
}

func (h *harness) take() []string {
	a := h.actions
	h.actions = nil
	return a
}

func (h *harness) expect(t *testing.T, want ...string) {
	t.Helper()
	if got := h.take(); !slices.Equal(got, want) {
		t.Errorf("actions:\n got %q\nwant %q", got, want)
	}
}

func run(cmd string) string { return RunMark + " " + cmd }
func done(code int) string  { return fmt.Sprintf("%s %d ~/project", DoneMark, code) }

func TestParseTitle(t *testing.T) {
	tests := []struct {
		title string
		want  parsedTitle
	}{
		{run("make build"), parsedTitle{kind: titleRun, command: "make build"}},
		{run("echo a\nb"), parsedTitle{kind: titleRun, command: "echo a\nb"}},
		{done(3), parsedTitle{kind: titleDone, code: 3}},
		{DoneMark + " 12abc", parsedTitle{kind: titleDone, code: 12}},
		{DoneMark + " +5", parsedTitle{kind: titleOther}},
		{DoneMark + " x", parsedTitle{kind: titleOther}},
		{"~/project", parsedTitle{kind: titleOther}},
		{"", parsedTitle{kind: titleOther}},
	}
	for _, tt := range tests {
		if got := parseTitle(tt.title); got != tt.want {
			t.Errorf("parseTitle(%q) = %+v, want %+v", tt.title, got, tt.want)
		}
	}
}

func TestShortCommandEmitsNothing(t *testing.T) {
	h := setup(t, nil)
	h.tracker.Title("w1:p1", "w1", run("ls -la"))
	h.tracker.Title("w1:p1", "w1", done(0))
	if len(h.clock.pending) != 0 {
		t.Errorf("timer still pending")
	}
	h.expect(t)
}

func TestLongCommandRunsThenFinishes(t *testing.T) {
	h := setup(t, nil)
	h.tracker.Focused("w9")
	h.take()
	h.tracker.Title("w1:p1", "w1", run("make build"))
	h.clock.fire()
	h.expect(t, "tokens w1 set=running clear=done")
	h.tracker.Title("w1:p1", "w1", done(0))
	h.expect(t, "tokens w1 set=done clear=running", "notify make done: make build")
}

func TestFailedCommandNotification(t *testing.T) {
	h := setup(t, nil)
	h.tracker.Title("w1:p1", "w1", run("npm test"))
	h.clock.fire()
	h.take()
	h.tracker.Title("w1:p1", "w1", done(2))
	h.expect(t, "tokens w1 set=done clear=running", "notify npm failed (exit 2): npm test")
}

func TestOtherTitleEndsRunWithUnknownCode(t *testing.T) {
	h := setup(t, nil)
	h.tracker.Title("w1:p1", "w1", run("make"))
	h.clock.fire()
	h.take()
	h.tracker.Title("w1:p1", "w1", "user@host: ~")
	h.expect(t, "tokens w1 set=done clear=running", "notify make finished: make")
}

func TestFocusedWorkspaceGetsNoDoneIcon(t *testing.T) {
	h := setup(t, nil)
	h.tracker.Focused("w1")
	h.tracker.Title("w1:p1", "w1", run("make"))
	h.clock.fire()
	h.take()
	h.tracker.Title("w1:p1", "w1", done(0))
	h.expect(t, "tokens w1 set= clear=running")
}

func TestFocusedFinishWithOtherPaneRunningEmitsNothing(t *testing.T) {
	h := setup(t, nil)
	h.tracker.Focused("w1")
	h.tracker.Title("w1:p1", "w1", run("make a"))
	h.tracker.Title("w1:p2", "w1", run("make b"))
	h.clock.fire()
	h.take()
	h.tracker.Title("w1:p1", "w1", done(0))
	h.expect(t)
}

func TestFocusClearsDoneIconOnce(t *testing.T) {
	h := setup(t, nil)
	h.tracker.Title("w1:p1", "w1", run("make"))
	h.clock.fire()
	h.tracker.Title("w1:p1", "w1", done(0))
	h.take()
	h.tracker.Focused("w1")
	h.tracker.Focused("w2")
	h.tracker.Focused("w1")
	h.expect(t, "tokens w1 set= clear=done")
}

func TestDoneIconFromEarlierWatcherClearsOnFocus(t *testing.T) {
	h := setup(t, func(c *TrackerConfig) { c.Done = []string{"w3"} })
	h.tracker.Focused("w3")
	h.expect(t, "tokens w3 set= clear=done")
}

func TestRunningIconStaysWhileAnotherPaneRuns(t *testing.T) {
	h := setup(t, nil)
	h.tracker.Title("w1:p1", "w1", run("make a"))
	h.tracker.Title("w1:p2", "w1", run("make b"))
	h.clock.fire()
	h.take()
	h.tracker.Title("w1:p1", "w1", done(0))
	h.expect(t, "tokens w1 set=done clear=", "notify make done: make a")
	h.tracker.Title("w1:p2", "w1", done(0))
	h.expect(t, "tokens w1 set=done clear=running", "notify make done: make b")
}

func TestSkippedCommandsAreNotTracked(t *testing.T) {
	h := setup(t, nil)
	h.tracker.Title("w1:p1", "w1", run("nvim foo"))
	h.tracker.Title("w1:p2", "w1", run("/usr/bin/ssh host"))
	h.tracker.Title("w1:p3", "w1", run("claude-work --resume"))
	if len(h.clock.pending) != 0 {
		t.Errorf("skipped command started a timer")
	}
	h.expect(t)
}

func TestSkippedCommandEndsRunInProgress(t *testing.T) {
	h := setup(t, nil)
	h.tracker.Title("w1:p1", "w1", run("make"))
	h.clock.fire()
	h.take()
	h.tracker.Title("w1:p1", "w1", run("ssh host"))
	h.expect(t, "tokens w1 set=done clear=running", "notify make finished: make")
}

func TestRepeatedRunTitleDoesNotRestart(t *testing.T) {
	h := setup(t, nil)
	h.tracker.Title("w1:p1", "w1", run("make"))
	h.clock.fire()
	h.take()
	h.tracker.Title("w1:p1", "w1", run("make"))
	if len(h.clock.pending) != 0 {
		t.Errorf("repeated title started a timer")
	}
	h.expect(t)
}

func TestClosedPaneDropsRunQuietly(t *testing.T) {
	h := setup(t, nil)
	h.tracker.Title("w1:p1", "w1", run("make"))
	h.clock.fire()
	h.take()
	h.tracker.PaneClosed("w1:p1")
	h.expect(t, "tokens w1 set= clear=running")
}

func TestNotifyOff(t *testing.T) {
	h := setup(t, func(c *TrackerConfig) { c.Notify = false })
	h.tracker.Title("w1:p1", "w1", run("make"))
	h.clock.fire()
	h.take()
	h.tracker.Title("w1:p1", "w1", done(0))
	h.expect(t, "tokens w1 set=done clear=running")
}

// A timer that fires after its run ended must not show the running icon.
func TestStaleTimerIsIgnored(t *testing.T) {
	h := setup(t, nil)
	h.tracker.Title("w1:p1", "w1", run("make"))
	var stale func()
	for _, fn := range h.clock.pending {
		stale = fn
	}
	h.tracker.Title("w1:p1", "w1", done(0))
	stale()
	h.expect(t)
}
