'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const { createTracker, parseTitle, RUN_MARK, DONE_MARK } = require('../lib/tracker');

function setup(options = {}) {
  const actions = [];
  const timers = new Map();
  let nextTimer = 1;
  const tracker = createTracker({
    delayMs: 5000,
    skip: ['nvim', 'ssh', 'claude*'],
    notify: true,
    setTimer: (fn) => {
      const id = nextTimer++;
      timers.set(id, fn);
      return id;
    },
    clearTimer: (id) => timers.delete(id),
    emit: (action) => actions.push(action),
    ...options,
  });
  const fire = () => {
    const pending = [...timers.values()];
    timers.clear();
    for (const fn of pending) fn();
  };
  return { tracker, actions, timers, fire };
}

const run = (cmd) => `${RUN_MARK} ${cmd}`;
const done = (code) => `${DONE_MARK} ${code} ~/project`;

test('parseTitle reads run and done marks and ignores other titles', () => {
  assert.deepEqual(parseTitle(run('make build')), { kind: 'run', command: 'make build' });
  assert.deepEqual(parseTitle(done(3)), { kind: 'done', code: 3 });
  assert.deepEqual(parseTitle('~/project'), { kind: 'other' });
  assert.deepEqual(parseTitle(undefined), { kind: 'other' });
});

test('a command that ends before the delay emits nothing', () => {
  const { tracker, actions, timers } = setup();
  tracker.title('w1:p1', 'w1', run('ls -la'));
  tracker.title('w1:p1', 'w1', done(0));
  assert.equal(timers.size, 0);
  assert.deepEqual(actions, []);
});

test('a long command shows running, then finished with a notification', () => {
  const { tracker, actions, fire } = setup();
  tracker.focused('w9');
  actions.length = 0;
  tracker.title('w1:p1', 'w1', run('make build'));
  fire();
  assert.deepEqual(actions, [{ type: 'tokens', workspace: 'w1', set: { running: true }, clear: ['done'] }]);
  actions.length = 0;
  tracker.title('w1:p1', 'w1', done(0));
  assert.deepEqual(actions, [
    { type: 'tokens', workspace: 'w1', set: { done: true }, clear: ['running'] },
    { type: 'notify', title: 'make done', body: 'make build' },
  ]);
});

test('a failed command says so in the notification', () => {
  const { tracker, actions, fire } = setup();
  tracker.title('w1:p1', 'w1', run('npm test'));
  fire();
  tracker.title('w1:p1', 'w1', done(2));
  assert.deepEqual(actions.at(-1), { type: 'notify', title: 'npm failed (exit 2)', body: 'npm test' });
});

test('any other title ends a run with an unknown exit code', () => {
  const { tracker, actions, fire } = setup();
  tracker.title('w1:p1', 'w1', run('make'));
  fire();
  tracker.title('w1:p1', 'w1', 'user@host: ~');
  assert.deepEqual(actions.at(-1), { type: 'notify', title: 'make finished', body: 'make' });
});

test('the focused workspace gets no finished icon and no notification', () => {
  const { tracker, actions, fire } = setup();
  tracker.focused('w1');
  actions.length = 0;
  tracker.title('w1:p1', 'w1', run('make'));
  fire();
  actions.length = 0;
  tracker.title('w1:p1', 'w1', done(0));
  assert.deepEqual(actions, [{ type: 'tokens', workspace: 'w1', set: {}, clear: ['running'] }]);
});

test('a finish in the focused workspace with another pane still running emits nothing', () => {
  const { tracker, actions, fire } = setup();
  tracker.focused('w1');
  tracker.title('w1:p1', 'w1', run('make a'));
  tracker.title('w1:p2', 'w1', run('make b'));
  fire();
  actions.length = 0;
  tracker.title('w1:p1', 'w1', done(0));
  assert.deepEqual(actions, []);
});

test('focusing a workspace clears its finished icon once', () => {
  const { tracker, actions, fire } = setup();
  tracker.title('w1:p1', 'w1', run('make'));
  fire();
  tracker.title('w1:p1', 'w1', done(0));
  actions.length = 0;
  tracker.focused('w1');
  tracker.focused('w2');
  tracker.focused('w1');
  assert.deepEqual(actions, [{ type: 'tokens', workspace: 'w1', set: {}, clear: ['done'] }]);
});

test('a finished icon left from an earlier watcher clears on focus', () => {
  const { tracker, actions } = setup({ done: ['w3'] });
  tracker.focused('w3');
  assert.deepEqual(actions, [{ type: 'tokens', workspace: 'w3', set: {}, clear: ['done'] }]);
});

test('the running icon stays while another pane in the workspace still runs', () => {
  const { tracker, actions, fire } = setup();
  tracker.title('w1:p1', 'w1', run('make a'));
  tracker.title('w1:p2', 'w1', run('make b'));
  fire();
  actions.length = 0;
  tracker.title('w1:p1', 'w1', done(0));
  assert.deepEqual(actions[0], { type: 'tokens', workspace: 'w1', set: { done: true }, clear: [] });
  tracker.title('w1:p2', 'w1', done(0));
  assert.deepEqual(actions[2], { type: 'tokens', workspace: 'w1', set: { done: true }, clear: ['running'] });
});

test('skipped commands, plain or by pattern, are not tracked', () => {
  const { tracker, actions, timers } = setup();
  tracker.title('w1:p1', 'w1', run('nvim foo'));
  tracker.title('w1:p2', 'w1', run('/usr/bin/ssh host'));
  tracker.title('w1:p3', 'w1', run('claude-work --resume'));
  assert.equal(timers.size, 0);
  assert.deepEqual(actions, []);
});

test('a skipped command still ends a run that was in progress', () => {
  const { tracker, actions, fire } = setup();
  tracker.title('w1:p1', 'w1', run('make'));
  fire();
  tracker.title('w1:p1', 'w1', run('ssh host'));
  assert.deepEqual(actions.at(-1), { type: 'notify', title: 'make finished', body: 'make' });
});

test('a repeated run title for the same command does not restart the run', () => {
  const { tracker, actions, fire, timers } = setup();
  tracker.title('w1:p1', 'w1', run('make'));
  fire();
  tracker.title('w1:p1', 'w1', run('make'));
  assert.equal(timers.size, 0);
  assert.equal(actions.length, 1);
});

test('a closed pane drops its run quietly', () => {
  const { tracker, actions, fire } = setup();
  tracker.title('w1:p1', 'w1', run('make'));
  fire();
  actions.length = 0;
  tracker.paneClosed('w1:p1');
  assert.deepEqual(actions, [{ type: 'tokens', workspace: 'w1', set: {}, clear: ['running'] }]);
});

test('notify: false turns notifications off', () => {
  const { tracker, actions, fire } = setup({ notify: false });
  tracker.title('w1:p1', 'w1', run('make'));
  fire();
  tracker.title('w1:p1', 'w1', done(0));
  assert.equal(actions.filter((a) => a.type === 'notify').length, 0);
});
