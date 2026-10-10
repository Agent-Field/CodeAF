import { test, expect } from '@playwright/test';
import { installMockEngine, OUTPUT_CAP_BYTES } from './support/mock-engine';
import { openApp } from './support/conversation';
import type { EngineSnapshot } from '../../src/features/chat/engine-client';

test('incremental reads retain earlier messages and flag large outputs', async ({ page }) => {
  const engine = await installMockEngine(page, { initial: { entries: [{ Role: 'user', Text: 'Earlier words' }] } });
  await openApp(page);
  const held = engine.snapshot();
  engine.update({ entries: [...held.entries, { Role: 'tool', Text: '', CallID: 'large', Output: 'x'.repeat(OUTPUT_CAP_BYTES + 1) }] });
  const next = await page.evaluate(async held => {
    const path = '/src/features/chat/engine-client.ts';
    const { readEngine } = await import(/* @vite-ignore */ path);
    return readEngine(held.id, held.entries.length, held);
  }, held);
  expect(next.entries).toEqual([held.entries[0], { Role: 'tool', Text: '', CallID: 'large', Output: '', OutputOmitted: true, OutputBytes: OUTPUT_CAP_BYTES + 1 }]);
});

test('stream tails merge in order, header-only changes preserve messages, and a gap replaces them', async ({ page }) => {
  const engine = await installMockEngine(page, { initial: { entries: [{ Role: 'user', Text: 'Earlier words' }] } });
  await openApp(page);
  const held = engine.snapshot();
  const { entries: _entries, ...header } = held;
  const records = [
    { seq: 1, type: 'snapshot', snapshot: { header: { ...header, seq: 1, entryCount: 2 }, from: 1, entries: [{ Role: 'assistant', Text: 'New reply', Answer: true }] } },
    { seq: 2, type: 'snapshot', snapshot: { header: { ...header, seq: 2, title: 'New title', entryCount: 2 }, from: 2, entries: [] } },
    { seq: 3, type: 'snapshot', snapshot: { header: { ...header, seq: 3, entryCount: 1 }, from: 0, reset: true, entries: [{ Role: 'user', Text: 'Compacted' }] } },
    { seq: 4, type: 'snapshot', snapshot: { ...held, seq: 4, entries: [{ Role: 'user', Text: 'Current full transcript' }] } },
  ];
  await page.route('**/api/engine/sessions/*/events?*', route => route.fulfill({ contentType: 'text/event-stream', body: records.map(r => `id: ${r.seq}\ndata: ${JSON.stringify(r)}\n\n`).join('') }));
  const result = await page.evaluate(async held => {
    const path = '/src/features/chat/engine-client.ts';
    const { watchEngine } = await import(/* @vite-ignore */ path);
    const snapshots: EngineSnapshot[] = [];
    let error = '';
    try { await watchEngine(held, (next: EngineSnapshot) => snapshots.push(next), () => {}, new AbortController().signal); }
    catch (caught) { error = (caught as Error).message; }
    return { snapshots, error };
  }, held);
  expect(result.error).toContain('connection closed');
  expect(result.snapshots.map(s => s.entries.map(e => e.Text))).toEqual([
    ['Earlier words', 'New reply'], ['Earlier words', 'New reply'], ['Compacted'], ['Current full transcript'],
  ]);
  expect(result.snapshots[1].title).toBe('New title');
});
