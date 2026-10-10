import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createFocusWire, type FocusPosition } from './useFocusHistory.ts';
import type { FocusEntry } from './model.ts';

/** A window over an in-memory store; `reopen` is the same window after a relaunch. */
function window(store: { saved: string | null } = { saved: null }) {
  const navigated: FocusEntry[] = [];
  const wire = createFocusWire({
    load: () => JSON.parse(store.saved ?? 'null'),
    save: history => { store.saved = JSON.stringify(history); },
    navigate: entry => navigated.push(entry),
  });
  return { wire, store, navigated };
}
const at = (windowPlace: string, tabId: string, drillPath: string[] = []): FocusPosition => ({ windowPlace, tabId, drillPath });
const ids = (wire: ReturnType<typeof createFocusWire>) => wire.getSnapshot().history.entries.map(e => `${e.windowPlace}/${e.tabId}${e.drillPath.length ? '/' + e.drillPath.join('.') : ''}`);

test('tab changes, place switches and drill-ins are steps', () => {
  const { wire } = window();
  wire.observe(at('now', 't1'));
  wire.observe(at('now', 't2'));
  wire.observe(at('p1', 'h1'));
  wire.observe(at('p1', 'h1', ['job']));
  assert.deepEqual(ids(wire), ['now/t1', 'now/t2', 'p1/h1', 'p1/h1/job']);
});

test('a repeat of the current position, and a background open, add nothing', () => {
  const { wire } = window();
  wire.observe(at('now', 't1'));
  wire.observe(at('now', 't1'));
  wire.setTabs('now', ['t1', 't2']);
  wire.observeBackground(at('now', 't2'));
  assert.deepEqual(ids(wire), ['now/t1']);
});

test('back restores the previous step through navigate and does not record the arrival as a new step', () => {
  const { wire, navigated } = window();
  wire.observe(at('now', 't1'));
  wire.observe(at('p1', 'h1'));
  assert.equal(wire.back(), true);
  assert.deepEqual(navigated.map(e => `${e.windowPlace}/${e.tabId}`), ['now/t1']);
  wire.observe(at('now', 't1'));
  assert.deepEqual(ids(wire), ['now/t1', 'p1/h1']);
  assert.equal(wire.getSnapshot().canForward, true);
  assert.equal(wire.forward(), true);
  assert.equal(navigated.at(-1)?.tabId, 'h1');
});

test('the place the shell passes through on the way to a step is not recorded', () => {
  const { wire } = window();
  wire.observe(at('p1', 'h1'));
  wire.observe(at('p2', 'x1'));
  wire.back();
  wire.observe(at('p2', 'x0')); // still in p2 until the switch lands
  wire.observe(at('p1', 'h2')); // the place's own saved tab, not the target
  wire.observe(at('p1', 'h1')); // the shell selects the target
  assert.deepEqual(ids(wire), ['p1/h1', 'p2/x1']);
  assert.equal(wire.getSnapshot().canForward, true);
});

test('a person moving on after back drops the forward tail', () => {
  const { wire } = window();
  wire.observe(at('now', 't1'));
  wire.observe(at('now', 't2'));
  wire.back();
  wire.observe(at('now', 't1'));
  wire.observe(at('now', 't3'));
  assert.deepEqual(ids(wire), ['now/t1', 'now/t3']);
});

test('a closed tab is skipped and its step stays in the stack', () => {
  const { wire, navigated } = window();
  wire.observe(at('now', 't1'));
  wire.observe(at('now', 't2'));
  wire.observe(at('now', 't3'));
  wire.setTabs('now', ['t1', 't3']);
  wire.back();
  assert.equal(navigated[0].tabId, 't1');
  assert.equal(ids(wire).length, 3);
});

test('the back chip appears only after a move the person did not start, and goes when they act', () => {
  const { wire } = window();
  wire.observe(at('now', 't1'));
  assert.equal(wire.getSnapshot().chip, undefined);
  wire.markCause('next-up');
  wire.observe(at('now', 't2'));
  assert.equal(wire.getSnapshot().chip?.tabId, 't1');
  wire.dismissChip();
  assert.equal(wire.getSnapshot().chip, undefined);
  wire.observe(at('now', 't3'));
  assert.equal(wire.getSnapshot().chip, undefined);
});

test('entries survive a relaunch, and the relaunch restore is not a step', () => {
  const first = window();
  first.wire.observe(at('now', 't1'));
  first.wire.observe(at('p1', 'h1', ['job']));
  const second = window(first.store);
  second.wire.setTabs('now', ['t1']);
  second.wire.observe(at('p1', 'h1', ['job']));
  assert.deepEqual(ids(second.wire), ['now/t1', 'p1/h1/job']);
  assert.equal(second.wire.getSnapshot().canBack, true);
  second.wire.back();
  assert.equal(second.navigated[0].tabId, 't1');
});

test('a late-loaded store keeps the live step and installs the saved stack', () => {
  const first = window();
  first.wire.observe(at('now', 't1'));
  first.wire.observe(at('now', 't2'));
  const late = window();
  late.wire.observe(at('now', 't2'));
  late.wire.hydrate(JSON.parse(first.store.saved!));
  assert.deepEqual(ids(late.wire), ['now/t1', 'now/t2']);
});

test('garbage in the window store is no history', () => {
  const { wire } = window({ saved: '{"entries":"nope"}' });
  assert.deepEqual(ids(wire), []);
});

test('scroll and draft ride along on the step they were noted on', () => {
  const { wire } = window();
  wire.observe({ ...at('now', 't1'), draftKey: 't1' });
  wire.observe({ ...at('now', 't1'), scroll: [{ key: 'conversation', top: 40, left: 0, end: false }] });
  const entry = wire.getSnapshot().history.entries[0];
  assert.equal(entry.draftKey, 't1');
  assert.equal(entry.scroll[0].top, 40);
});
