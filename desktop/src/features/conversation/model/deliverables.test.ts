import test from 'node:test';
import assert from 'node:assert/strict';
import type { Deliverable, TurnBlock } from '../types.ts';
import { projectTurnsV2 } from './project.ts';
import { entry, final, narrate, snap, tool, user } from './testkit.ts';

const deliverables = (blocks: TurnBlock[]): Deliverable[] =>
  blocks.flatMap((b) => (b.kind === 'deliverable' ? [b.deliverable] : []));
const run = (entries: ReturnType<typeof user>[]) => projectTurnsV2(snap(entries)).turns[0];

const IMAGE_OUT = '/ws/.codeaf/images/harbour.png — 1024×1024 png, 1.4MB, generated on vendor/paint-5';

test('a generated image sits right before the final answer', () => {
  const turn = run([
    user('draw a harbour'),
    narrate('Drawing it.'),
    tool('generate_image', 'g1', { prompt: 'a harbour at dusk', size: '1024x1024' }, { Output: IMAGE_OUT }),
    final('Here it is.'),
  ]);
  assert.deepEqual(turn.blocks.map((b) => b.kind), ['work', 'deliverable', 'answer']);
  assert.deepEqual(deliverables(turn.blocks)[0], {
    kind: 'image',
    id: 'g1:image',
    path: '/ws/.codeaf/images/harbour.png',
    caption: 'a harbour at dusk',
    meta: '1024×1024 png',
  });
});

test('Args.path outranks the path in the result line', () => {
  const turn = run([
    user('go'),
    narrate(''),
    tool('generate_image', 'g1', { prompt: 'x', path: 'out/a.png' }, { Output: IMAGE_OUT }),
    final('ok'),
  ]);
  const image = deliverables(turn.blocks)[0];
  assert.equal(image.kind === 'image' && image.path, 'out/a.png');
});

test('a failed generation promotes nothing', () => {
  const turn = run([user('go'), narrate(''), tool('generate_image', 'g1', { prompt: 'x' }, { Failed: true }), final('sorry')]);
  assert.equal(deliverables(turn.blocks).length, 0);
});

test('several images stay in call order before the answer', () => {
  const turn = run([
    user('go'),
    narrate(''),
    tool('generate_image', 'g1', { prompt: 'one' }, { Output: IMAGE_OUT }),
    tool('generate_image', 'g2', { prompt: 'two' }, { Output: '/ws/b.png — 512×512 jpg, 20KB, generated on m' }),
    final('ok'),
  ]);
  const paths = deliverables(turn.blocks).map((d) => d.kind === 'image' && d.path);
  assert.deepEqual(paths, ['/ws/.codeaf/images/harbour.png', '/ws/b.png']);
});

test('speech and video become media deliverables', () => {
  const turn = run([
    user('go'),
    narrate(''),
    tool('speak', 's1', { text: 'hello there' }, { Output: '/ws/hello.mp3 — 3s mp3' }),
    tool('generate_video', 'v1', { prompt: 'waves' }, { Output: '/ws/waves.mp4 — 5s mp4' }),
    final('ok'),
  ]);
  const media = deliverables(turn.blocks).map((d) => d.kind === 'media' && [d.media, d.path, d.caption]);
  assert.deepEqual(media, [['audio', '/ws/hello.mp3', 'hello there'], ['video', '/ws/waves.mp4', 'waves']]);
});

test('written and edited files make one changes block after the last answer', () => {
  const turn = run([
    user('go'),
    narrate('Writing.'),
    tool('write', 'w1', { path: 'a.go', content: 'x\ny\nz\n' }),
    tool('edit', 'e1', { path: 'b.go', edits: [{ oldText: 'a\nb', newText: 'a\nc\nd' }] }),
    tool('edit', 'e2', { path: 'b.go', oldText: 'q', newText: 'r' }),
    final('Done.'),
  ]);
  assert.deepEqual(turn.blocks.map((b) => b.kind), ['work', 'answer', 'deliverable']);
  const changes = deliverables(turn.blocks)[0];
  assert.equal(changes.kind, 'changes');
  assert.deepEqual(changes.kind === 'changes' && changes.files, [
    { path: 'a.go', added: 3, removed: 0, capped: undefined, source: 'write' },
    { path: 'b.go', added: 3, removed: 2, capped: undefined, source: 'edit' },
  ]);
});

test('a capped write is flagged capped', () => {
  const turn = run([user('go'), narrate(''), tool('write', 'w1', { path: 'big.txt', content: 'a\nb… (9000 more bytes)' }), final('ok')]);
  const changes = deliverables(turn.blocks)[0];
  assert.equal(changes.kind === 'changes' && changes.files[0].capped, true);
});

test('failed edits are not changes', () => {
  const turn = run([user('go'), narrate(''), tool('edit', 'e1', { path: 'a.go', oldText: 'a', newText: 'b' }, { Failed: true }), final('no')]);
  assert.equal(deliverables(turn.blocks).length, 0);
});

test('attachments: structured fields win and the sentence stays in the text', () => {
  const sentence = 'look at this\n\nattached file: /ws/old.txt';
  const turn = run([
    user(sentence, {
      Attachments: [
        { Path: '/ws/report.pdf', Name: 'report.pdf', MIME: 'application/pdf', Kind: 'file' },
        { Path: '/ws/shot.png', Name: 'shot.png', MIME: 'image/png', Kind: 'image' },
      ],
      ImageRefs: ['/ws/pic.png'],
    }),
  ]);
  assert.equal(turn.user, sentence);
  assert.deepEqual(turn.attachments.map((a) => [a.path, a.source]), [
    ['/ws/pic.png', 'image'],
    ['/ws/report.pdf', 'attachment'],
    ['/ws/shot.png', 'image'],
  ]);
});

test('attachments: with no structured field, the sentence is parsed and removed', () => {
  const one = run([user('summarize\n\nattached file: /ws/a.md')]);
  assert.equal(one.user, 'summarize');
  assert.deepEqual(one.attachments, [{ path: '/ws/a.md', source: 'attachment' }]);
  const many = run([user('compare\n\nattached files:\n/ws/a.md\n/ws/b.md', { ImageRefs: ['/ws/p.png'] })]);
  assert.equal(many.user, 'compare');
  assert.deepEqual(many.attachments.map((a) => a.path), ['/ws/p.png', '/ws/a.md', '/ws/b.md']);
});

test('ordinary text mentioning attached files is left alone', () => {
  const turn = run([user('what does "attached file: x" mean in a mail?')]);
  assert.equal(turn.attachments.length, 0);
  assert.equal(turn.user, 'what does "attached file: x" mean in a mail?');
});

test('entry helper keeps Role so tests read plainly', () => {
  assert.equal(entry({ Role: 'note' }).Role, 'note');
});
