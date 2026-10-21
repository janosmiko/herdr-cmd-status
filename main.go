// Command herdr-cmd-status watches pane title changes on Herdr's event stream
// and shows a running and a finished icon in the Spaces row. `--ensure` starts
// a detached watcher unless one is already alive, and exits.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const source = "herdr-cmd-status"

// Nerd Font terminal and check-circle glyphs, as escapes because private-use characters are invisible in source.
var tokens = map[string]struct{ name, value string }{
	"running": {"space_cmd_running", "\uf489"},
	"done":    {"space_cmd_done", "\uf058"},
}

type config struct {
	DelaySeconds float64  `json:"delay_seconds"`
	Notify       bool     `json:"notify"`
	Skip         []string `json:"skip"`
}

var defaults = config{
	DelaySeconds: 5,
	Notify:       true,
	// Interactive programs wait for input, so "running" would be wrong. Agents and
	// remote shells report on their own.
	Skip: []string{
		"claude*", "codex", "gemini", "opencode",
		"nvim", "vim", "vi", "nano", "less", "more", "man",
		"ssh", "mosh", "limactl", "lima", "lazygit", "tig", "htop", "btop", "top", "fzf", "herdr",
	},
}

type env struct {
	herdrBin   string
	stateDir   string
	pidFile    string
	socketPath string
}

func mustEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is not set; run this from Herdr", name)
	}
	return value
}

func loadConfig() config {
	file := filepath.Join(os.Getenv("HERDR_PLUGIN_CONFIG_DIR"), "config.json")
	data, err := os.ReadFile(file)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("ignoring %s: %v", file, err)
		}
		return defaults
	}
	cfg := defaults
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("ignoring %s: %v", file, err)
		return defaults
	}
	for _, pattern := range cfg.Skip {
		if _, err := path.Match(pattern, ""); err != nil {
			log.Printf("skip pattern %q in %s never matches: %v", pattern, file, err)
		}
	}
	return cfg
}

func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// The pidfile survives a reboot, and the OS can give its PID to another process.
func isWatcher(pid int) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	return err == nil && strings.Contains(string(out), exe)
}

func (e env) runningPid() int {
	data, err := os.ReadFile(e.pidFile)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 || !alive(pid) || !isWatcher(pid) {
		return 0
	}
	return pid
}

// claimPidFile takes the pidfile atomically, so two watchers that start together cannot both run.
// The link makes the pidfile appear with the PID already in it. A watcher that
// read an empty pidfile would take it as stale.
func (e env) claimPidFile() bool {
	tmp := e.pidFile + "." + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		log.Print(err)
		return false
	}
	defer func() { _ = os.Remove(tmp) }()
	for range 2 {
		err := os.Link(tmp, e.pidFile)
		if err == nil {
			return true
		}
		if !errors.Is(err, fs.ErrExist) || e.runningPid() != 0 {
			return false
		}
		_ = os.Remove(e.pidFile)
	}
	return false
}

func (e env) releasePidFile() {
	data, err := os.ReadFile(e.pidFile)
	if err == nil && strings.TrimSpace(string(data)) == strconv.Itoa(os.Getpid()) {
		_ = os.Remove(e.pidFile)
	}
}

func (e env) herdr(args ...string) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, e.herdrBin, args...).Output()
	if err != nil {
		log.Printf("herdr %s failed: %v", strings.Join(args[:min(2, len(args))], " "), err)
		return nil
	}
	return out
}

type workspace struct {
	ID      string            `json:"workspace_id"`
	Focused bool              `json:"focused"`
	Tokens  map[string]string `json:"tokens"`
}

type event struct {
	Error any `json:"error"`
	Data  *struct {
		Type        string `json:"type"`
		WorkspaceID string `json:"workspace_id"`
		PaneID      string `json:"pane_id"`
		Pane        *struct {
			PaneID        string `json:"pane_id"`
			WorkspaceID   string `json:"workspace_id"`
			TerminalTitle string `json:"terminal_title"`
		} `json:"pane"`
	} `json:"data"`
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// textArg keeps a value from the terminal title from being read as a herdr
// flag. Any program in a pane can set that title. herdr accepts neither `--`
// nor `--flag=value`.
func textArg(s string) string {
	if strings.HasPrefix(s, "-") {
		return " " + s
	}
	return s
}

// watch runs the watcher until the Herdr server goes away, and returns the exit code.
func (e env) watch() int {
	if err := os.MkdirAll(e.stateDir, 0o755); err != nil {
		log.Print(err)
		return 1
	}
	if !e.claimPidFile() {
		return 0
	}
	defer e.releasePidFile()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	// One queue for every CLI call, so a workspace's token changes land in order.
	calls := make(chan []string, 256)
	go func() {
		for args := range calls {
			e.herdr(args...)
		}
	}()
	setTokens := func(ws string, set, clear []string) {
		args := []string{"workspace", "report-metadata", ws, "--source", source}
		for _, key := range set {
			args = append(args, "--token", tokens[key].name+"="+tokens[key].value)
		}
		for _, key := range clear {
			args = append(args, "--clear-token", tokens[key].name)
		}
		calls <- args
	}

	var workspaces []workspace
	var list struct {
		Result struct {
			Workspaces []workspace `json:"workspaces"`
		} `json:"result"`
	}
	if err := json.Unmarshal(e.herdr("workspace", "list"), &list); err != nil {
		log.Printf("cannot read workspace list: %v", err)
	} else {
		workspaces = list.Result.Workspaces
	}
	var focused string
	var done []string
	for _, ws := range workspaces {
		// A watcher that died mid-run left its running icons behind.
		if ws.Tokens[tokens["running"].name] != "" {
			setTokens(ws.ID, nil, []string{"running"})
		}
		if ws.Tokens[tokens["done"].name] != "" {
			done = append(done, ws.ID)
		}
		if ws.Focused {
			focused = ws.ID
		}
	}

	cfg := loadConfig()
	fired := make(chan func())
	tracker := NewTracker(TrackerConfig{
		Delay:  time.Duration(cfg.DelaySeconds * float64(time.Second)),
		Skip:   cfg.Skip,
		Notify: cfg.Notify,
		After: func(d time.Duration, fn func()) func() bool {
			return time.AfterFunc(d, func() { fired <- fn }).Stop
		},
		Tokens: setTokens,
		Notification: func(title, body string) {
			calls <- []string{"notification", "show", textArg(title), "--body", textArg(truncate(body, 120)), "--sound", "done"}
		},
		Done: done,
	})
	if focused != "" {
		tracker.Focused(focused)
	}

	conn, err := net.Dial("unix", e.socketPath)
	if err != nil {
		log.Printf("socket error: %v", err)
		return 1
	}
	defer func() { _ = conn.Close() }()
	subscribe, _ := json.Marshal(map[string]any{
		"id":     "watch",
		"method": "events.subscribe",
		"params": map[string]any{"subscriptions": []map[string]string{
			{"type": "pane.updated"}, {"type": "pane.closed"}, {"type": "workspace.focused"},
		}},
	})
	if _, err := conn.Write(append(subscribe, '\n')); err != nil {
		log.Printf("socket error: %v", err)
		return 1
	}

	events := make(chan event)
	readErr := make(chan error, 1)
	go func() {
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 1 {
				var ev event
				if jerr := json.Unmarshal(line, &ev); jerr != nil {
					log.Printf("bad event: %v", jerr)
				} else {
					events <- ev
				}
			}
			if err != nil {
				readErr <- err
				return
			}
		}
	}()

	for {
		select {
		case fn := <-fired:
			fn()
		case ev := <-events:
			if ev.Error != nil {
				log.Printf("subscription error: %v", ev.Error)
				return 1
			}
			handle(tracker, ev)
		case err := <-readErr:
			// The server is gone. The next server's startup hook starts a new watcher.
			if errors.Is(err, io.EOF) {
				return 0
			}
			log.Printf("socket error: %v", err)
			return 1
		case <-signals:
			return 0
		}
	}
}

func handle(tracker *Tracker, ev event) {
	d := ev.Data
	if d == nil {
		return
	}
	switch d.Type {
	case "pane_updated":
		if d.Pane != nil {
			tracker.Title(d.Pane.PaneID, d.Pane.WorkspaceID, d.Pane.TerminalTitle)
		}
	case "pane_closed":
		id := d.PaneID
		if id == "" && d.Pane != nil {
			id = d.Pane.PaneID
		}
		tracker.PaneClosed(id)
	case "workspace_focused":
		tracker.Focused(d.WorkspaceID)
	}
}

func (e env) ensure() error {
	if e.runningPid() != 0 {
		return nil
	}
	if err := os.MkdirAll(e.stateDir, 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(e.stateDir, "watch.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	// The watcher holds its own copy of the file descriptor.
	defer func() { _ = logFile.Close() }()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func (e env) restart() error {
	if pid := e.runningPid(); pid != 0 {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return err
		}
		// The new watcher can take the pidfile only after the old one exits.
		for i := 0; i < 20 && alive(pid); i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return e.ensure()
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	stateDir := mustEnv("HERDR_PLUGIN_STATE_DIR")
	e := env{
		herdrBin:   mustEnv("HERDR_BIN_PATH"),
		stateDir:   stateDir,
		pidFile:    filepath.Join(stateDir, "watch.pid"),
		socketPath: mustEnv("HERDR_SOCKET_PATH"),
	}
	mode := ""
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	var err error
	switch mode {
	case "--ensure":
		err = e.ensure()
	case "--restart":
		err = e.restart()
	case "":
		os.Exit(e.watch())
	default:
		err = fmt.Errorf("unknown argument %q", mode)
	}
	if err != nil {
		log.Fatal(err)
	}
}
