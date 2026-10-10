import test from 'node:test';
import assert from 'node:assert/strict';
import { acceptsPlaceDrag, placeDndTypes, placeDragFromPointer, readPlaceDrag, resolvePlaceDrop, wouldCreatePlaceCycle, type PlaceDropContext, type PlaceTransfer } from './placeDnd.ts';

const places = [
  { id: 'root', parents: [] },
  { id: 'other', parents: [] },
  { id: 'child', parents: ['root', 'other'] },
  { id: 'grandchild', parents: ['child'] },
  { id: 'target', parents: [] },
];
const context = (altKey = false): PlaceDropContext => ({ targetPlaceId: 'target', altKey, fromPlaceId: 'root', places, chatForTab: id => id === 'tab' ? 'saved-chat' : undefined });
const transfer = (type: string, data: string): PlaceTransfer => ({ types: [type], getData: requested => requested === type ? data : '' });

test('a pointer payload is the same drag a tile already understands', () => {
  assert.deepEqual(placeDragFromPointer({ kind: 'chat', id: 'chat', ids: ['chat'] }), { kind: 'chat', ids: ['chat'] });
  assert.deepEqual(placeDragFromPointer({ kind: 'tab', id: 'tab' }), { kind: 'tab', id: 'tab' });
  assert.equal(placeDragFromPointer({ kind: 'queue-row', id: 'q' }), undefined);
});

for (const kind of ['chat', 'place', 'tab'] as const) {
  for (const alt of [false, true]) {
    test(`${kind} MIME with Option ${alt ? 'held moves' : 'released adds'}`, () => {
      const data = kind === 'tab' ? 'tab' : JSON.stringify([kind === 'chat' ? 'chat' : 'child']);
      const result = resolvePlaceDrop(readPlaceDrag(transfer(placeDndTypes[kind], data)), context(alt));
      assert.equal(result.status, 'action');
      if (result.status !== 'action') return;
      assert.deepEqual(result, {
        status: 'action', dropEffect: alt ? 'move' : 'copy',
        action: kind === 'place'
          ? { kind: alt ? 'place-move' : 'place-add-parent', placeIds: ['child'], targetPlaceId: 'target', ...(alt ? { fromPlaceId: 'root' } : {}) }
          : { kind: alt ? 'chat-move' : 'chat-add', chatIds: [kind === 'tab' ? 'saved-chat' : 'chat'], targetPlaceId: 'target', ...(alt ? { fromPlaceId: 'root' } : {}) },
      });
    });
  }
}

for (const alt of [false, true]) {
  test(`Files stay additive with Option ${alt}`, () => {
    const file = new File(['source'], 'notes.md');
    const files: PlaceTransfer = { types: ['Files', 'text/uri-list'], files: [file], getData: () => 'https://example.com' };
    assert.deepEqual(resolvePlaceDrop(readPlaceDrag(files), context(alt)), {
      status: 'action', dropEffect: 'copy', action: { kind: 'sources-add', targetPlaceId: 'target', sources: { kind: 'files', files: [file] } },
    });
  });
  test(`URI lists stay additive with Option ${alt}`, () => {
    const urls = readPlaceDrag(transfer(placeDndTypes.urls, '# comment\r\nhttps://example.com/a\r\n\r\nhttp://example.com/b\nhttps://example.com/a'));
    assert.deepEqual(resolvePlaceDrop(urls, context(alt)), {
      status: 'action', dropEffect: 'copy', action: { kind: 'sources-add', targetPlaceId: 'target', sources: { kind: 'urls', urls: ['https://example.com/a', 'http://example.com/b'] } },
    });
  });
}

for (const alt of [false, true]) {
  for (const target of ['root', 'child', 'grandchild']) {
    test(`self or descendant ${target} refuses Option ${alt} with screen-reader words`, () => {
      const result = resolvePlaceDrop({ kind: 'place', ids: ['root'] }, { ...context(alt), targetPlaceId: target });
      assert.deepEqual(result, { status: 'refused', reason: 'cycle', announcement: 'A place cannot be put inside itself or one of its children. Nothing was changed.' });
      assert.ok(!('action' in result));
    });
  }
}

test('a second-parent ancestor is also a cycle; a sibling is safe', () => {
  assert.equal(wouldCreatePlaceCycle(['other'], 'grandchild', places), true);
  assert.equal(wouldCreatePlaceCycle(['target'], 'grandchild', places), false);
  assert.equal(wouldCreatePlaceCycle(['child'], 'root', places), false);
});

test('a batch with one cyclic place is refused entirely', () => {
  assert.equal(resolvePlaceDrop({ kind: 'place', ids: ['target', 'root'] }, { ...context(), targetPlaceId: 'child' }).status, 'refused');
});

test('cycle traversal terminates even if the supplied graph already contains a loop', () => {
  assert.equal(wouldCreatePlaceCycle(['target'], 'a', [{ id: 'a', parents: ['b'] }, { id: 'b', parents: ['a'] }]), false);
});

test('Option needs a real source and removes only that relationship', () => {
  for (const fromPlaceId of [undefined, '', 'gone', 'target']) {
    assert.equal(resolvePlaceDrop({ kind: 'chat', ids: ['chat'] }, { ...context(true), fromPlaceId }).status, 'refused');
  }
  assert.equal(resolvePlaceDrop({ kind: 'place', ids: ['child'] }, { ...context(true), fromPlaceId: 'target' }).status, 'refused');
  assert.equal(resolvePlaceDrop({ kind: 'place', ids: ['child'] }, { ...context(true), fromPlaceId: 'grandchild' }).status, 'refused');
  // Resolving a move never edits the snapshot or removes the child's second parent.
  resolvePlaceDrop({ kind: 'place', ids: ['child'] }, context(true));
  assert.deepEqual(places[2].parents, ['root', 'other']);
});

test('unknown places and unsaved or non-chat tabs do not manufacture identities', () => {
  for (const id of ['file-tab', 'unsent', 'gone']) {
    const result = resolvePlaceDrop({ kind: 'tab', id }, context());
    assert.equal(result.status, 'refused');
    if (result.status === 'refused') assert.equal(result.reason, 'not-chat');
  }
  assert.equal(resolvePlaceDrop({ kind: 'chat', ids: ['chat'] }, { ...context(), targetPlaceId: 'gone' }).status, 'refused');
  assert.equal(resolvePlaceDrop({ kind: 'place', ids: ['gone'] }, context()).status, 'refused');
});

test('protected drag-over reads only types, never payloads', () => {
  for (const type of Object.values(placeDndTypes)) assert.equal(acceptsPlaceDrag([type]), true);
  assert.equal(acceptsPlaceDrag(['text/plain', 'application/codeaf-group']), false);
});

test('invalid internal payloads are not reinterpreted as external drops', () => {
  for (const raw of ['', '{', 'null', '"chat"', '[]', '[""]', '[" "]', '[1]', '["chat", null]']) {
    const input = transfer(placeDndTypes.chat, raw);
    input.types = [placeDndTypes.chat, placeDndTypes.urls];
    assert.equal(readPlaceDrag(input), undefined);
  }
  assert.deepEqual(readPlaceDrag(transfer(placeDndTypes.place, '["child","child"]')), { kind: 'place', ids: ['child'] });
});

test('plain text, empty files, and unsafe or mixed URI lists are refused', () => {
  assert.equal(readPlaceDrag(transfer('text/plain', 'chat')), undefined);
  assert.equal(readPlaceDrag({ types: ['Files'], getData: () => '' }), undefined);
  for (const uri of ['', '# comment', 'javascript:alert(1)', 'file:///private/key', 'data:text/plain,hello', 'not a url', 'https://example.com\njavascript:alert(1)']) {
    assert.equal(readPlaceDrag(transfer(placeDndTypes.urls, uri)), undefined);
  }
  assert.equal(resolvePlaceDrop(undefined, context()).status, 'refused');
});

test('direct callers cannot submit empty sources or unsafe links', () => {
  for (const payload of [{ kind: 'files', files: [] }, { kind: 'urls', urls: [] }, { kind: 'urls', urls: ['javascript:alert(1)'] }] as const) {
    assert.equal(resolvePlaceDrop(payload, context()).status, 'refused');
  }
});
