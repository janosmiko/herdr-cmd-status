'use strict';

// Shells write these at the start of the terminal title (shell/cmd-status.*).
const RUN_MARK = '⏵';
const DONE_MARK = '⏹';

const RUN_RE = new RegExp(`^${RUN_MARK} (.*)$`, 's');
const DONE_RE = new RegExp(`^${DONE_MARK} (\\d+)`);

function parseTitle(title) {
  if (typeof title !== 'string') return { kind: 'other' };
  const run = RUN_RE.exec(title);
  if (run) return { kind: 'run', command: run[1] };
  const done = DONE_RE.exec(title);
  if (done) return { kind: 'done', code: Number(done[1]) };
  return { kind: 'other' };
}

function commandName(command) {
  const first = command.trim().split(/\s+/)[0] ?? '';
  return first.slice(first.lastIndexOf('/') + 1);
}

function globToRegExp(glob) {
  const escaped = glob.replace(/[.+^${}()|[\]\\?]/g, '\\$&').replace(/\*/g, '.*');
  return new RegExp(`^${escaped}$`);
}

function finishTitle(name, code) {
  if (code === null) return `${name} finished`;
  return code === 0 ? `${name} done` : `${name} failed (exit ${code})`;
}

// Turns title changes into token and notification actions. A command shows as
// running only after delayMs, so short commands never flash an icon.
// done: workspaces that already show the finished icon.
function createTracker({ delayMs, skip, notify, setTimer, clearTimer, emit, done = [] }) {
  const skipRes = skip.map(globToRegExp);
  const panes = new Map(); // paneId -> { workspace, command, name, timer, running }
  const runningByWorkspace = new Map(); // workspaceId -> Set of paneIds
  // Lets a workspace switch skip the CLI call when there is no icon to clear.
  const doneWorkspaces = new Set(done);
  let focusedWorkspace = null;

  function start(paneId, workspace, command) {
    const name = commandName(command);
    if (!name || skipRes.some((re) => re.test(name))) return;
    const pane = { workspace, command, name, timer: null, running: false };
    pane.timer = setTimer(() => {
      pane.timer = null;
      pane.running = true;
      const set = runningByWorkspace.get(workspace) ?? new Set();
      set.add(paneId);
      runningByWorkspace.set(workspace, set);
      doneWorkspaces.delete(workspace);
      emit({ type: 'tokens', workspace, set: { running: true }, clear: ['done'] });
    }, delayMs);
    panes.set(paneId, pane);
  }

  // code: exit status, or null when unknown. quiet: the pane is gone, so only tidy up.
  function finish(paneId, code, quiet = false) {
    const pane = panes.get(paneId);
    if (!pane) return;
    panes.delete(paneId);
    if (pane.timer !== null) {
      clearTimer(pane.timer);
      return;
    }
    const set = runningByWorkspace.get(pane.workspace);
    set.delete(paneId);
    if (set.size === 0) runningByWorkspace.delete(pane.workspace);
    const clear = set.size === 0 ? ['running'] : [];
    const showDone = !quiet && pane.workspace !== focusedWorkspace;
    if (showDone) doneWorkspaces.add(pane.workspace);
    if (showDone || clear.length) {
      emit({ type: 'tokens', workspace: pane.workspace, set: showDone ? { done: true } : {}, clear });
    }
    if (showDone && notify) {
      emit({ type: 'notify', title: finishTitle(pane.name, code), body: pane.command });
    }
  }

  return {
    title(paneId, workspace, title) {
      const parsed = parseTitle(title);
      const current = panes.get(paneId);
      if (parsed.kind === 'run') {
        if (current?.command === parsed.command) return;
        finish(paneId, null);
        start(paneId, workspace, parsed.command);
        return;
      }
      finish(paneId, parsed.kind === 'done' ? parsed.code : null);
    },
    paneClosed(paneId) {
      finish(paneId, null, true);
    },
    focused(workspace) {
      focusedWorkspace = workspace;
      if (doneWorkspaces.delete(workspace)) emit({ type: 'tokens', workspace, set: {}, clear: ['done'] });
    },
  };
}

module.exports = { createTracker, parseTitle, RUN_MARK, DONE_MARK };
