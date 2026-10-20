#!/usr/bin/env node
'use strict';

// Watches pane title changes on Herdr's event stream and shows a running and a
// finished icon in the Spaces row. `--ensure` starts a detached watcher unless
// one is already alive, and exits.

const { execFile, execFileSync, spawn } = require('node:child_process');
const fs = require('node:fs');
const net = require('node:net');
const path = require('node:path');

const { createTracker } = require('../lib/tracker');

const SOURCE = 'herdr-cmd-status';
// Nerd Font terminal and check-circle glyphs, as escapes because private-use characters are invisible in source.
const TOKENS = {
  running: { name: 'space_cmd_running', value: '\uf489' },
  done: { name: 'space_cmd_done', value: '\uf058' },
};
const DEFAULTS = {
  delay_seconds: 5,
  notify: true,
  // Interactive programs wait for input, so "running" would be wrong. Agents and
  // remote shells report on their own.
  skip: [
    'claude*', 'codex', 'gemini', 'opencode',
    'nvim', 'vim', 'vi', 'nano', 'less', 'more', 'man',
    'ssh', 'mosh', 'limactl', 'lima', 'lazygit', 'tig', 'htop', 'btop', 'top', 'fzf', 'herdr',
  ],
};

const env = (name) => {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is not set; run this from Herdr`);
  return value;
};
const herdrBin = env('HERDR_BIN_PATH');
const stateDir = env('HERDR_PLUGIN_STATE_DIR');
const pidFile = path.join(stateDir, 'watch.pid');

function log(...args) {
  console.error(new Date().toISOString(), ...args);
}

function loadConfig() {
  const file = path.join(process.env.HERDR_PLUGIN_CONFIG_DIR ?? '', 'config.json');
  try {
    return { ...DEFAULTS, ...JSON.parse(fs.readFileSync(file, 'utf8')) };
  } catch (error) {
    if (error.code !== 'ENOENT') log(`ignoring ${file}:`, error.message);
    return DEFAULTS;
  }
}

function alive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

// The pidfile survives a reboot, and the OS can give its PID to another process.
function isWatcher(pid) {
  try {
    return execFileSync('ps', ['-o', 'command=', '-p', String(pid)], { encoding: 'utf8' }).includes(__filename);
  } catch {
    return false;
  }
}

function runningPid() {
  try {
    const pid = Number(fs.readFileSync(pidFile, 'utf8'));
    return pid && alive(pid) && isWatcher(pid) ? pid : null;
  } catch {
    return null;
  }
}

// Takes the pidfile atomically, so two watchers that start together cannot both run.
function claimPidFile() {
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      fs.writeFileSync(pidFile, String(process.pid), { flag: 'wx' });
      return true;
    } catch (error) {
      if (error.code !== 'EEXIST' || runningPid()) return false;
      fs.rmSync(pidFile, { force: true });
    }
  }
  return false;
}

function herdr(args) {
  return new Promise((resolve) => {
    execFile(herdrBin, args, { timeout: 5000 }, (error, stdout) => {
      if (error) log(`herdr ${args.slice(0, 2).join(' ')} failed:`, error.message);
      resolve(error ? null : stdout);
    });
  });
}

// One queue for every CLI call, so a workspace's token changes land in order.
let queue = Promise.resolve();
function enqueue(args) {
  queue = queue.then(() => herdr(args));
}

function apply(action) {
  if (action.type === 'notify') {
    enqueue(['notification', 'show', action.title, '--body', action.body.slice(0, 120), '--sound', 'done']);
    return;
  }
  const args = ['workspace', 'report-metadata', action.workspace, '--source', SOURCE];
  for (const key of Object.keys(action.set)) args.push('--token', `${TOKENS[key].name}=${TOKENS[key].value}`);
  for (const key of action.clear) args.push('--clear-token', TOKENS[key].name);
  enqueue(args);
}

async function initialState() {
  const out = await herdr(['workspace', 'list']);
  let workspaces = [];
  try {
    workspaces = JSON.parse(out).result.workspaces ?? [];
  } catch (error) {
    log('cannot read workspace list:', error.message);
  }
  // A watcher that died mid-run left its running icons behind.
  for (const ws of workspaces) {
    if (ws.tokens?.[TOKENS.running.name]) {
      apply({ type: 'tokens', workspace: ws.workspace_id, set: {}, clear: ['running'] });
    }
  }
  return {
    focused: workspaces.find((ws) => ws.focused)?.workspace_id ?? null,
    done: workspaces.filter((ws) => ws.tokens?.[TOKENS.done.name]).map((ws) => ws.workspace_id),
  };
}

function handle(tracker, message) {
  if (message.error) {
    log('subscription error:', JSON.stringify(message.error));
    process.exit(1);
  }
  const data = message.data;
  switch (data?.type) {
    case 'pane_updated':
      tracker.title(data.pane.pane_id, data.pane.workspace_id, data.pane.terminal_title);
      break;
    case 'pane_closed':
      tracker.paneClosed(data.pane_id ?? data.pane?.pane_id);
      break;
    case 'workspace_focused':
      tracker.focused(data.workspace_id);
      break;
  }
}

async function watch() {
  fs.mkdirSync(stateDir, { recursive: true });
  if (!claimPidFile()) return;
  const cleanup = () => {
    try {
      if (Number(fs.readFileSync(pidFile, 'utf8')) === process.pid) fs.rmSync(pidFile, { force: true });
    } catch {}
  };
  process.on('exit', cleanup);
  for (const signal of ['SIGTERM', 'SIGINT', 'SIGHUP']) process.on(signal, () => process.exit(0));

  const config = loadConfig();
  const { focused, done } = await initialState();
  const tracker = createTracker({
    delayMs: config.delay_seconds * 1000,
    skip: config.skip,
    notify: config.notify,
    setTimer: setTimeout,
    clearTimer: clearTimeout,
    emit: apply,
    done,
  });
  if (focused) tracker.focused(focused);

  const socket = net.createConnection(env('HERDR_SOCKET_PATH'));
  socket.setEncoding('utf8');
  let buffer = '';
  socket.on('data', (chunk) => {
    buffer += chunk;
    let end;
    while ((end = buffer.indexOf('\n')) >= 0) {
      const line = buffer.slice(0, end);
      buffer = buffer.slice(end + 1);
      if (!line) continue;
      try {
        handle(tracker, JSON.parse(line));
      } catch (error) {
        log('bad event:', error.message);
      }
    }
  });
  // The server is gone. The next server's startup hook starts a new watcher.
  socket.on('close', () => process.exit(0));
  socket.on('error', (error) => {
    log('socket error:', error.message);
    process.exit(1);
  });
  const subscriptions = ['pane.updated', 'pane.closed', 'workspace.focused'].map((type) => ({ type }));
  socket.write(`${JSON.stringify({ id: 'watch', method: 'events.subscribe', params: { subscriptions } })}\n`);
}

function ensure() {
  if (runningPid()) return;
  fs.mkdirSync(stateDir, { recursive: true });
  const logFd = fs.openSync(path.join(stateDir, 'watch.log'), 'a');
  spawn(process.execPath, [__filename], { detached: true, stdio: ['ignore', logFd, logFd] }).unref();
}

async function restart() {
  const pid = runningPid();
  if (pid) {
    process.kill(pid);
    // The new watcher can take the pidfile only after the old one exits.
    for (let i = 0; i < 20 && alive(pid); i++) await new Promise((resolve) => setTimeout(resolve, 100));
  }
  ensure();
}

if (process.argv.includes('--ensure')) ensure();
else if (process.argv.includes('--restart')) restart();
else watch().catch((error) => {
  log(error.stack);
  process.exit(1);
});
