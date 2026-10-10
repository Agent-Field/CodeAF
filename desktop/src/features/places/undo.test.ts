import test from 'node:test';
import assert from 'node:assert/strict';
import { createPlaceUndo } from './undo.ts';
import { PlacesError } from './client.ts';
import { createStructuralUndo, undoLimit } from '../tabs/undo/structuralUndo.ts';
import { workspaceReducer, type WorkspaceState } from '../tabs/model.ts';

const workspace = (): WorkspaceState => ({
  tabs: ['a', 'b'].map(id => ({ id, title: id, kind: 'conversation', draft: '', pinned: false })),
  groups: [], closed: [], activeId: 'a', nextNumber: 3, recentIds: ['a', 'b'],
});
const answer = { revision: 1, undone: 1 };

function rig() {
  const stack = createStructuralUndo();
  const calls: string[][] = [];
  const places = createPlaceUndo({ undo: async receipts => { calls.push(receipts); return answer; } }, stack);
  return { stack, places, calls };
}

async function undo(stack: ReturnType<typeof createStructuralUndo>) {
  const plan = stack.take(workspace());
  assert.equal(plan.kind, 'external');
  if (plan.kind === 'external') await plan.undo();
}

test('close, archive, move, delete and filing inverses use exact engine receipts newest first', async () => {
  const { stack, places, calls } = rig();
  for (const kind of ['close', 'archive', 'move', 'delete', 'filing']) places.register({ receipts: [kind] });
  while (stack.size) await undo(stack);
  assert.deepEqual(calls, [['filing'], ['delete'], ['move'], ['archive'], ['close']]);
});

test('tab and place steps share one chronology and one cap of twenty', async () => {
  const { stack, places, calls } = rig();
  const before = workspace();
  const action = { type: 'pin', id: 'a' } as const;
  stack.record(before, action, workspaceReducer(before, action));
  for (let index = 0; index < undoLimit; index++) places.register({ receipts: [`receipt-${index}`] });
  assert.equal(stack.size, undoLimit);
  while (stack.size) await undo(stack);
  assert.equal(calls.length, undoLimit);
  assert.deepEqual(calls[0], ['receipt-19']);
  assert.equal(stack.take(before).kind, 'empty', 'the oldest tab inverse was evicted by the shared cap');
});

test('interleaved tab and place inverses pop in action order', async () => {
  const { stack, places, calls } = rig();
  places.register({ receipts: ['archive'] });
  const before = workspace();
  const action = { type: 'pin', id: 'a' } as const;
  const after = workspaceReducer(before, action);
  stack.record(before, action, after);
  places.register({ receipts: ['filing'] });
  await undo(stack);
  assert.equal(await stack.undoExternal(), false, 'the app fallback never skips a newer tab action');
  const plan = stack.take(after);
  assert.equal(plan.kind, 'apply');
  if (plan.kind === 'apply') assert.equal(workspaceReducer(after, plan.actions[0]).tabs[0].pinned, false);
  await undo(stack);
  assert.deepEqual(calls, [['filing'], ['archive']]);
});

test('windows keep their own steps and clearing one window leaves the other intact', async () => {
  const left = rig(); const right = rig();
  left.places.register({ receipts: ['left'] });
  right.places.register({ receipts: ['right'] });
  left.stack.clear();
  assert.equal(left.stack.size, 0);
  await undo(right.stack);
  assert.deepEqual(left.calls, []);
  assert.deepEqual(right.calls, [['right']]);
});

test('no-op receipts do not register and input receipts cannot mutate the inverse', async () => {
  const { stack, places, calls } = rig();
  assert.equal(places.register({ receipts: [] }), undefined);
  assert.equal(stack.size, 0);
  const receipts = ['first', 'second'];
  places.register({ receipts });
  receipts.reverse();
  await undo(stack);
  assert.deepEqual(calls, [['first', 'second']]);
});

test('toast Undo consumes only its step; an evicted or already undone toast cannot run again', async () => {
  const { stack, places, calls } = rig();
  const first = places.register({ receipts: ['first'] })!;
  places.register({ receipts: ['second'] });
  await first.undo(); await first.undo();
  assert.equal(stack.size, 1);
  await undo(stack);
  const evicted = places.register({ receipts: ['evicted'] })!;
  for (let index = 0; index < undoLimit; index++) places.register({ receipts: [`new-${index}`] });
  await evicted.undo();
  assert.deepEqual(calls, [['first'], ['second']]);
});

test('keyboard and toast racing share one in-flight inverse', async () => {
  const stack = createStructuralUndo();
  let finish!: () => void; let calls = 0;
  const places = createPlaceUndo({ undo: async () => { calls++; await new Promise<void>(resolve => { finish = resolve; }); return answer; } }, stack);
  const entry = places.register({ receipts: ['one'] })!;
  const first = entry.undo(); const second = stack.undoExternal();
  await Promise.resolve();
  assert.equal(calls, 1);
  finish(); await Promise.all([first, second]);
  assert.equal(stack.size, 0);
});

test('transport failures retain authority for retry; a canonical cannot_undo refusal drops the step', async () => {
  const stack = createStructuralUndo(); let calls = 0;
  const places = createPlaceUndo({ undo: async () => {
    if (++calls === 1) throw new PlacesError('Engine unavailable');
    throw new PlacesError('This place changed since.', 409, 'cannot_undo');
  } }, stack);
  const entry = places.register({ receipts: ['one'] })!;
  await assert.rejects(entry.undo(), /Engine unavailable/);
  assert.equal(stack.size, 1);
  await assert.rejects(entry.undo(), /changed since/);
  assert.equal(stack.size, 0);
  await entry.undo();
  assert.equal(calls, 2);
});

test('a refresh failure after successful undo never sends the receipt twice', async () => {
  const { stack, places, calls } = rig();
  const entry = places.register({ receipts: ['one'] }, async () => { throw new Error('Refresh failed'); })!;
  await assert.rejects(entry.undo(), /Refresh failed/);
  await entry.undo();
  assert.equal(stack.size, 0);
  assert.deepEqual(calls, [['one']]);
});


test('a rail close registers its canonical reopen without requiring a graph receipt', async () => {
  const { stack, places, calls } = rig();
  let reopened = 0;
  places.registerClose(async () => { reopened++; });
  await undo(stack);
  assert.equal(reopened, 1);
  assert.deepEqual(calls, []);
});

test('a partial undo retry only submits receipts the engine has not already undone', async () => {
  const stack = createStructuralUndo(); const calls: string[][] = [];
  const places = createPlaceUndo({ undo: async receipts => {
    calls.push(receipts);
    if (calls.length === 1) throw new PlacesError('Interrupted', 503, '', [], 1);
    return answer;
  } }, stack);
  const entry = places.register({ receipts: ['old', 'new'] })!;
  await assert.rejects(entry.undo(), /Interrupted/);
  await entry.undo();
  assert.deepEqual(calls, [['old', 'new'], ['old']]);
});
