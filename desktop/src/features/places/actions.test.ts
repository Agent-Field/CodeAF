import test from 'node:test';
import assert from 'node:assert/strict';
import { PlacesError, type Mutation } from './client.ts';
import { PLACE_DELETE_UNDO_MS, createPlaceActions, cycleWords, type PlaceMutationClient } from './actions.ts';

const names = {
  place: (id: string) => ({ pl_soft: 'Software', pl_rel: 'Release', pl_cfg: 'Config parser' }[id]),
  chat: (id: string) => ({ 'sess-a': 'Q3 report', 'sess-b': 'Generics' }[id]),
};

function committed(action: string, extra: Partial<Mutation> = {}): Mutation {
  return {
    revision: 4,
    receipts: [{ id: 'rc_1', action, beforeRevision: 3, afterRevision: 4, at: '2026-10-10T00:00:00Z' }],
    noop: false,
    undo: ['rc_1'],
    ...extra,
  };
}

const quiet: Mutation = { revision: 4, receipts: [], noop: true, undo: [] };

type Call = { method: string; args: unknown[] };

function fake(answer: (method: string, args: unknown[]) => Mutation | Promise<Mutation> = () => committed('place.create')) {
  const calls: Call[] = [];
  const method = (name: string) => async (...args: unknown[]) => {
    calls.push({ method: name, args });
    return answer(name, args);
  };
  const client = {
    createPlace: method('createPlace'),
    updatePlace: method('updatePlace'),
    setParents: method('setParents'),
    pinPlace: method('pinPlace'),
    unpinPlace: method('unpinPlace'),
    railOp: method('railOp'),
    archivePlace: method('archivePlace'),
    deletePlace: method('deletePlace'),
    addChats: method('addChats'),
    removeChats: method('removeChats'),
  } as PlaceMutationClient;
  return { calls, client, actions: createPlaceActions(client, names) };
}

test('create, rename and setTint each return the engine receipt as the inverse', async () => {
  const { calls, actions } = fake(method => committed(method));
  const created = await actions.create({ name: 'Reports', tint: 'iris', parent: 'pl_soft' }, { generation: 3 });
  assert.equal(created.inverse?.kind, 'create');
  assert.equal(created.inverse?.text, 'Created “Reports”');
  assert.equal(created.inverse?.subject, 'Reports');
  assert.deepEqual(created.inverse?.receipts, ['rc_1']);
  assert.deepEqual(calls[0].args, [{ name: 'Reports', tint: 'iris', parent: 'pl_soft', ifGeneration: 3 }]);

  const renamed = await actions.rename('pl_cfg', 'Parser', { generation: 4 });
  assert.equal(renamed.inverse?.text, 'Renamed “Config parser” to “Parser”');
  assert.equal(renamed.inverse?.subject, 'Parser');
  assert.deepEqual(calls[1].args, ['pl_cfg', { name: 'Parser', ifGeneration: 4 }]);

  const tinted = await actions.setTint('pl_soft', 'sage');
  assert.equal(tinted.inverse?.kind, 'setTint');
  assert.equal(tinted.inverse?.text, 'Changed the tint of “Software”');
  assert.equal(tinted.inverse?.durationMs, undefined);
});

test('a write the engine did not change has no inverse', async () => {
  const { actions } = fake(() => quiet);
  const renamed = await actions.rename('pl_soft', 'Software');
  assert.equal(renamed.inverse, undefined);
  assert.equal(renamed.revision, 4);
});

test('addParent keeps the other parents, and option-move replaces them with the one target', async () => {
  const { calls, actions } = fake(() => committed('place.reparent'));
  const added = await actions.addParent('pl_rel', 'pl_soft', { generation: 9 });
  assert.equal(added.inverse?.kind, 'addParent');
  assert.equal(added.inverse?.text, 'Added “Release” into “Software”');
  assert.deepEqual(calls[0].args, ['pl_rel', { add: 'pl_soft', ifGeneration: 9 }]);

  const moved = await actions.move('pl_cfg', 'pl_rel');
  assert.equal(moved.inverse?.kind, 'move');
  assert.equal(moved.inverse?.text, 'Moved “Config parser” into “Release”');
  assert.equal(moved.inverse?.subject, 'Config parser');
  assert.deepEqual(calls[1].args, ['pl_cfg', { set: ['pl_rel'], ifGeneration: undefined }]);
});

test('putting a place inside itself is said in words and the client is not asked', async () => {
  const { calls, actions } = fake();
  await assert.rejects(actions.addParent('pl_soft', 'pl_soft'), (error: unknown) =>
    error instanceof PlacesError && error.code === 'cycle' && error.message === "A place can't be inside itself.");
  await assert.rejects(actions.move('pl_soft', 'pl_soft'), (error: unknown) =>
    error instanceof PlacesError && error.code === 'cycle' && error.message === "A place can't be inside itself.");
  assert.equal(calls.length, 0);
});

test('a cycle the engine names as a chain is returned as a sentence, and any other refusal is left alone', async () => {
  assert.equal(cycleWords('placegraph: that would make a place its own ancestor: Software -> Release -> Software'),
    'That would put “Software” inside “Release”, which is already inside it.');
  assert.equal(cycleWords('Software -> Software'), "A place can't be inside itself.");
  assert.equal(cycleWords('That would put “Software” inside “Release”, which is already inside it.'),
    'That would put “Software” inside “Release”, which is already inside it.');
  assert.equal(cycleWords('placegraph: that would make a place its own ancestor'), 'That would put a place inside itself.');

  const chain = new PlacesError('placegraph: that would make a place its own ancestor: Software -> Release -> Config parser -> Software', 409, 'cycle', [{ id: 'rc_kept', action: 'place.reparent', beforeRevision: 1, afterRevision: 1, at: '2026-10-10T00:00:00Z' }]);
  const { actions } = fake(() => { throw chain; });
  await assert.rejects(actions.addParent('pl_soft', 'pl_rel'), (error: unknown) =>
    error instanceof PlacesError && error.code === 'cycle' && error.message === 'That would put “Software” inside “Config parser”, which is already inside it.' && error.applied.length === 1 && error !== chain);

  const sentence = new PlacesError('That would put “Software” inside “Release”, which is already inside it.', 409, 'cycle');
  const kept = fake(() => { throw sentence; });
  await assert.rejects(kept.actions.move('pl_soft', 'pl_rel'), (error: unknown) => error === sentence);

  const taken = new PlacesError('Another place here already has that name.', 409, 'name_taken');
  const other = fake(() => { throw taken; });
  await assert.rejects(other.actions.rename('pl_soft', 'Release'), (error: unknown) => error === taken);

  const raw = new Error('placegraph: that would make a place its own ancestor: Release -> Software -> Release');
  const store = fake(() => { throw raw; });
  await assert.rejects(store.actions.move('pl_rel', 'pl_soft'), (error: unknown) =>
    error instanceof PlacesError && error.code === 'cycle' && error.message === 'That would put “Release” inside “Software”, which is already inside it.');
});

test('pin, unpin and reorder return inverses, and a pin index of zero is sent', async () => {
  const { calls, actions } = fake(method => committed(method));
  const pinned = await actions.pin('pl_cfg', { index: 0, generation: 2 });
  assert.equal(pinned.inverse?.text, 'Pinned “Config parser” to the rail');
  assert.deepEqual(calls[0].args, ['pl_cfg', 0, 2]);
  const unpinned = await actions.unpin('pl_soft');
  assert.equal(unpinned.inverse?.kind, 'unpin');
  assert.equal(unpinned.inverse?.text, 'Unpinned “Software”');
  const order = ['pl_rel', 'pl_soft'];
  const reordered = await actions.reorder(order, { generation: 8 });
  assert.equal(reordered.inverse?.kind, 'reorder');
  assert.equal(reordered.inverse?.text, 'Reordered the pinned places.');
  assert.equal(reordered.inverse?.subject, undefined);
  assert.deepEqual(calls[2].args, [{ op: 'reorder', order: ['pl_rel', 'pl_soft'], ifGeneration: 8 }]);
  order.push('pl_cfg');
  assert.deepEqual((calls[2].args[0] as { order: string[] }).order, ['pl_rel', 'pl_soft'], 'the request keeps the order it was given');
});

test('archive returns the engine place and an inverse, and a second archive of the same place does not', async () => {
  const archivedPlace = { id: 'pl_cfg', name: 'Config parser', archived: true, pinned: false } as Mutation['place'];
  let archives = 0;
  const { calls, actions } = fake(() => {
    archives += 1;
    return archives === 1 ? committed('place.archive', { place: archivedPlace }) : quiet;
  });
  const archived = await actions.archive('pl_cfg', { generation: 5 });
  assert.equal(archived.archived, true);
  assert.equal(archived.place?.pinned, false);
  assert.equal(archived.inverse?.kind, 'archive');
  assert.equal(archived.inverse?.text, 'Archived “Config parser”');
  assert.deepEqual(archived.inverse?.receipts, ['rc_1']);
  const again = await actions.archive('pl_cfg');
  assert.equal(again.inverse, undefined);
  assert.equal(again.archived, undefined);
  assert.deepEqual(calls.map(call => call.method), ['archivePlace', 'archivePlace']);
});

test('delete returns the engine counts and keeps Undo for 10 seconds', async () => {
  const { actions } = fake(() => committed('place.delete', {
    chats: 2, children: 1, result: { unfiled: 2, childrenMoved: 1, nowUnplaced: ['sess-a'] },
  }));
  const deleted = await actions.delete('pl_cfg');
  assert.equal(deleted.inverse?.kind, 'delete');
  assert.equal(deleted.inverse?.text, 'Deleted “Config parser”. No chat was deleted.');
  assert.equal(deleted.inverse?.durationMs, PLACE_DELETE_UNDO_MS);
  assert.equal(PLACE_DELETE_UNDO_MS, 10_000);
  assert.deepEqual(deleted.counts, { chats: 2, children: 1, nowUnplaced: ['sess-a'] });
});

test('delete counts come from the result when the envelope omitted them, and a missing count stays missing', async () => {
  const fromResult = fake(() => committed('place.delete', { result: { unfiled: 0, childrenMoved: 3, nowUnplaced: [] } }));
  const counted = await fromResult.actions.delete('pl_rel');
  assert.deepEqual(counted.counts, { chats: 0, children: 3, nowUnplaced: [] });

  const silent = fake(() => committed('place.delete'));
  const bare = await silent.actions.delete('pl_soft');
  assert.deepEqual(bare.counts, {});
  assert.equal(bare.inverse?.text, 'Deleted “Software”. No chat was deleted.');
});

test('filing adds, option-move leaves the previous place, and remove takes a chat out', async () => {
  const { calls, actions } = fake(() => committed('chat.file', { undo: ['rc_1', 'rc_2'] }));
  const added = await actions.addMembership('pl_soft', ['sess-a'], { generation: 6, addedBy: 'you' });
  assert.equal(added.inverse?.kind, 'addMembership');
  assert.equal(added.inverse?.text, 'Added “Q3 report” to “Software”');
  assert.deepEqual(added.inverse?.receipts, ['rc_1', 'rc_2']);
  assert.deepEqual(calls[0].args, ['pl_soft', ['sess-a'], { addedBy: 'you', ifGeneration: 6 }]);

  const moved = await actions.addMembership('pl_rel', ['sess-a', 'sess-b'], { moveFrom: 'pl_soft' });
  assert.equal(moved.inverse?.kind, 'move');
  assert.equal(moved.inverse?.text, 'Moved 2 chats to “Release”');
  assert.deepEqual(calls[1].args, ['pl_rel', ['sess-a', 'sess-b'], { moveFrom: 'pl_soft', ifGeneration: undefined }]);

  const same = await actions.addMembership('pl_soft', ['sess-a'], { moveFrom: 'pl_soft' });
  assert.equal(same.inverse?.kind, 'addMembership');
  assert.equal(same.inverse?.text, 'Added “Q3 report” to “Software”');
  assert.equal((calls[2].args[2] as { moveFrom?: string }).moveFrom, undefined);

  const removed = await actions.removeMembership('pl_cfg', ['sess-missing']);
  assert.equal(removed.inverse?.kind, 'removeMembership');
  assert.equal(removed.inverse?.text, 'Removed the chat from “Config parser”');
  assert.equal(removed.inverse?.subject, 'Config parser');
});

test('an unknown place is not given a name', async () => {
  const { actions } = fake(() => committed('place.archive'));
  const archived = await actions.archive('pl_gone');
  assert.equal(archived.inverse?.text, 'Archived the place');
  assert.equal(archived.inverse?.subject, undefined);
});

test('the inverse keeps its own copy of the receipt ids', async () => {
  const mutation = committed('place.pin');
  const { actions } = fake(() => mutation);
  const pinned = await actions.pin('pl_soft');
  mutation.undo.push('rc_later');
  assert.deepEqual(pinned.inverse?.receipts, ['rc_1']);
});
