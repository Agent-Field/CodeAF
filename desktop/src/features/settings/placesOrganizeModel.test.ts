import test from 'node:test';
import assert from 'node:assert/strict';
import {
  initialIncluded, lastRunLine, liveProgress, oneReceiptId, opLine, organizeFixtures, parseOrganizeSnapshot,
  progressLine, screenOf, skipOf, type OrganizeOp, type OrganizeProgress,
} from './placesOrganizeModel.ts';

const banned = /auditor|verdict|cluster|centroid|embedding|leiden|consensus|fallback/i;
const progress = (stage: string, done = 0, total = 0, run = 'run-1'): OrganizeProgress => ({ run, stage, done, total, cancellable: true });

test('the progress line uses the person-facing words and omits an unknown stage', () => {
  assert.equal(progressLine(progress('vectors', 100, 1240)), 'Reading 1,240 chats');
  assert.equal(progressLine(progress('dedup', 0, 0)), 'Reading');
  assert.equal(progressLine(progress('communities')), 'Grouping');
  assert.equal(progressLine(progress('park')), 'Grouping');
  assert.equal(progressLine(progress('name', 12, 30)), 'Naming (12 of 30)');
  assert.equal(progressLine(progress('review', 0, 0)), 'Naming');
  assert.equal(progressLine(progress('check', 1, 1)), 'Checking');
  assert.equal(progressLine(progress('later', 1, 2)), '');
  assert.equal(progressLine(undefined), '');
  for (const stage of ['dedup', 'kind', 'anchors', 'vectors', 'neighbours', 'communities', 'nest', 'audit', 'park', 'name', 'review', 'link', 'check']) {
    assert.equal(banned.test(progressLine(progress(stage, 3, 9))), false, stage);
  }
});

test('a feed from the same run replaces the snapshot line, and a different run does not', () => {
  const snapshot = progress('vectors', 0, 10);
  assert.equal(progressLine(liveProgress(true, snapshot, progress('name', 12, 30))), 'Naming (12 of 30)');
  assert.equal(progressLine(liveProgress(true, snapshot, progress('name', 12, 30, 'run-2'))), 'Reading 10 chats');
  assert.equal(liveProgress(false, snapshot, progress('name', 12, 30)), undefined);
});

test('the last run is its summary, and a cancelled run with none says Cancelled', () => {
  assert.equal(lastRunLine(undefined), '');
  assert.equal(lastRunLine({ summary: '12 new places', outcome: 'offered' }), '12 new places');
  assert.equal(lastRunLine({ summary: '', outcome: 'cancelled' }), 'Cancelled');
  assert.equal(lastRunLine({ summary: '  ', outcome: 'applied' }), '');
});

test('an op uses the engine sentence, and a kind with none does not show an id', () => {
  const why: OrganizeOp = { kind: 'rename', why: 'Rename to Desk', name: 'Other', proposed: true };
  assert.equal(opLine(why), 'Rename to Desk');
  assert.equal(opLine({ kind: 'rename', why: '', name: 'Desk', proposed: true }), 'Rename to “Desk”');
  assert.equal(opLine({ kind: 'merge', why: '', name: '', proposed: true }), 'Merge');
  assert.equal(opLine({ kind: 'nope', why: '', name: 'secret', proposed: true }), '');
  assert.equal(banned.test(opLine({ kind: 'dissolve', why: '', name: '', proposed: true })), false);
});

test('unchecked indexes are the skip list, and a hidden op stays included', () => {
  const ops: OrganizeOp[] = [
    { kind: 'dissolve', why: 'This place would go', name: '', proposed: true },
    { kind: 'nope', why: '', name: '', proposed: true },
    { kind: 'rename', why: '', name: 'Desk', proposed: true },
  ];
  const included = initialIncluded(ops);
  included.delete(2);
  assert.deepEqual(skipOf(ops, included), [2]);
  assert.equal(screenOf(organizeFixtures.plan).plan?.rows.map(row => row.line).join('|'), 'This place would go|Rename to “Desk”|File a chat');
});

test('every fixture screen says only what that state has', () => {
  assert.equal(screenOf(organizeFixtures['idle-empty']).progress, '');
  assert.equal(screenOf(organizeFixtures['idle-empty']).last, '');
  assert.equal(screenOf(organizeFixtures['idle-empty']).plan, null);
  assert.equal(screenOf(organizeFixtures['idle-summary']).last, '12 new places, 340 chats filed');
  assert.equal(screenOf(organizeFixtures['running-reading']).progress, 'Reading 1,240 chats');
  assert.equal(screenOf(organizeFixtures['running-reading']).cancel, true);
  assert.equal(screenOf(organizeFixtures['running-grouping']).progress, 'Grouping');
  assert.equal(screenOf(organizeFixtures['running-naming']).progress, 'Naming (12 of 30)');
  assert.equal(screenOf(organizeFixtures['running-checking']).progress, 'Checking');
  assert.equal(screenOf(organizeFixtures['running-quiet']).progress, '');
  assert.equal(screenOf(organizeFixtures['running-quiet']).cancel, true);
  assert.equal(screenOf(organizeFixtures['running-reading']).plan, null);
  assert.equal(screenOf(organizeFixtures.plan).plan?.summary, '3 places would go, 2 would be renamed, 410 chats would move');
  assert.equal(screenOf(organizeFixtures.cancelled).last, 'Cancelled');
  assert.equal(screenOf(organizeFixtures.unavailable).unavailable, 'Organizing places is not available from this engine yet.');
  assert.equal(screenOf(organizeFixtures.unavailable).cancel, false);
});

test('a snapshot parses, a zero run is no progress, and a bad plan is refused', () => {
  const snap = parseOrganizeSnapshot({
    running: true,
    progress: { run: 'run-1', stage: 'vectors', done: 1, total: 4, cancellable: false, spent: { Calls: 0, USD: 0, Fallbacks: 0 } },
    last: null,
    plan: null,
  });
  assert.equal(snap.running, true);
  assert.equal(snap.progress?.cancellable, false);
  assert.equal(parseOrganizeSnapshot({ running: false, progress: { run: '', stage: '', done: 0, total: 0 } }).progress, undefined);
  assert.throws(() => parseOrganizeSnapshot({ running: 'yes' }), /invalid organizing status/);
  assert.throws(() => parseOrganizeSnapshot({ running: false, plan: { id: '', ops: [] } }), /invalid organizing plan/);
});

test('accept must name exactly one receipt', () => {
  assert.equal(oneReceiptId({ receipt: { id: 'rc_1', action: 'plan' } }), 'rc_1');
  assert.equal(oneReceiptId({ receipts: [{ id: 'rc_1' }], undo: ['rc_1'] }), 'rc_1');
  assert.equal(oneReceiptId({ undo: ['rc_1'] }), 'rc_1');
  assert.throws(() => oneReceiptId({ receipts: [{ id: 'a' }, { id: 'b' }] }), /invalid receipt/);
  assert.throws(() => oneReceiptId({ receipt: { id: 'a' }, undo: ['b'] }), /invalid receipt/);
  assert.throws(() => oneReceiptId({}), /invalid receipt/);
});
