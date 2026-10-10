import { test, expect, type Page } from '@playwright/test';
import type { EngineEntry, EngineSnapshot } from '../../src/features/chat/engine-client';
import { installMockEngine, OUTPUT_CAP_BYTES, type MockEngine } from './support/mock-engine';
import { expectNoHorizontalOverflow, openApp } from './support/conversation';
import { expectAccessible } from './contracts';

// CV-300. After the first snapshot, a later read asks for the tail (`since` is how many
// entries the window already holds) and the painted transcript matches a full read.
// A tool body over the cap stays out of that snapshot; the row fetches it once, when
// the call is opened. A tail that does not meet the held transcript costs one full read.

const EARLIER = 'Earlier words';
const ASK = 'Find needle';
const ANSWER = 'Found two thousand matches.';
const STEP = 'Searching the logs';
const CALL_ID = 'g1';
const SESSION = 'mock-session-1.jsonl';
const THEMES = ['light', 'dark'] as const;
const WIDTHS = [320, 600, 1200] as const;

const LINE = 'match 1: needle in a haystack';
const BIG = `${LINE}\n${'line of output\n'.repeat(1200)}`;

const reply: EngineEntry[] = [
  { Role: 'user', Text: ASK },
  { Role: 'assistant', Text: `${STEP}.`, Answer: false },
  { Role: 'tool', Text: '', Tool: 'probe', Hint: 'probe', CallID: CALL_ID, Answered: true, Args: JSON.stringify({ pattern: 'needle' }), Output: BIG },
  { Role: 'assistant', Text: ANSWER, Answer: true },
];

type Shown = { Role: string; Text: string; CallID?: string; Output: string; OutputOmitted: boolean };

function shown(entries: { Role: string; Text: string; CallID?: string; Output?: string; OutputOmitted?: boolean }[]): Shown[] {
  return entries.map((entry) => ({
    Role: entry.Role,
    Text: entry.Text,
    CallID: entry.CallID,
    Output: entry.Output ?? '',
    OutputOmitted: entry.OutputOmitted ?? false,
  }));
}

/** GETs of the session document itself. Streams, tool bodies and file reads are not snapshot reads. */
function trackSnapshotReads(page: Page): string[] {
  const seen: string[] = [];
  page.on('request', (request) => {
    if (request.method() !== 'GET') return;
    const url = new URL(request.url());
    if (!url.pathname.endsWith('/sessions/mock-1')) return;
    seen.push(url.search);
  });
  return seen;
}

function seedChat(page: Page) {
  const state = {
    tabs: [{ id: 'search', title: 'Search', titleSource: 'manual', kind: 'conversation', pinned: false, draft: '', sessionFile: SESSION }],
    groups: [], closed: [], activeId: 'search', nextNumber: 2, recentIds: ['search'],
  };
  return page.addInitScript((value) => {
    if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', value);
  }, JSON.stringify(state));
}

async function boot(page: Page, theme: (typeof THEMES)[number], width: number) {
  await page.addInitScript((value) => localStorage.setItem('codeaf-theme', value), theme);
  await seedChat(page);
  await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
  await page.setViewportSize({ width, height: 800 });
  const engine = await installMockEngine(page, {
    initial: { title: 'Search', sessionFile: SESSION, entries: [{ Role: 'user', Text: EARLIER }] },
    tools: { [CALL_ID]: { output: BIG, full: true } },
  });
  const reads = trackSnapshotReads(page);
  await openApp(page);
  await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
  await expect(page.getByText(EARLIER, { exact: true })).toBeVisible();
  return { engine, reads };
}

/** The tail the stream delivered, folded onto the one entry the window already held. */
async function readTail(page: Page, held: EngineSnapshot, since: number): Promise<EngineSnapshot> {
  return page.evaluate(async ({ held, since }) => {
    const { readEngine } = await import(/* @vite-ignore */ '/src/features/chat/engine-client.ts');
    return readEngine(held.id, since, held);
  }, { held, since });
}

async function readWhole(page: Page, id: string): Promise<EngineSnapshot> {
  return page.evaluate(async (id) => {
    const { readEngine } = await import(/* @vite-ignore */ '/src/features/chat/engine-client.ts');
    return readEngine(id);
  }, id);
}

const toolGets = (engine: MockEngine) => engine.calls.filter((call) => call.method === 'GET' && call.path.includes(`/tools/${CALL_ID}`));

for (const theme of THEMES) {
  for (const width of WIDTHS) {
    test(`CV-300: a tail paints the same transcript as a full read, and the tool body loads on expand (${theme}, ${width}px)`, async ({ page }) => {
      test.setTimeout(60_000);
      expect(Buffer.byteLength(BIG)).toBeGreaterThan(OUTPUT_CAP_BYTES);
      const { engine, reads } = await boot(page, theme, width);
      const held = engine.snapshot();
      engine.update({
        title: 'Search',
        entries: [...held.entries, ...reply],
      });
      await expect(page.getByText(ASK, { exact: true })).toBeVisible();
      await expect(page.getByText(ANSWER, { exact: true })).toBeVisible();
      await expect(page.getByText(EARLIER, { exact: true })).toBeVisible();
      // The body is not in the snapshot the window painted, so it cannot already be on screen.
      await expect(page.getByText(LINE)).toHaveCount(0);
      expect(toolGets(engine)).toHaveLength(0);
      // Painting the tail did not take another snapshot read. The first read was the attach.
      expect(reads).toEqual([]);

      // Open the call before the closed stream's reconnect can replace the tail with a whole snapshot.
      const work = page.getByRole('button', { name: /^Worked / });
      await work.focus();
      await page.keyboard.press('Enter');
      await expect(work).toHaveAttribute('aria-expanded', 'true');
      const step = page.getByRole('button', { name: STEP });
      await step.focus();
      await page.keyboard.press('Enter');
      await expect(step).toHaveAttribute('aria-expanded', 'true');
      expect(toolGets(engine)).toHaveLength(0);
      const call = page.getByRole('button', { name: 'probe probe' });
      await call.focus();
      await page.keyboard.press('Enter');
      await expect(call).toHaveAttribute('aria-expanded', 'true');
      await expect(page.getByLabel('Output')).toContainText(LINE);
      // StrictMode replays the effect, so the count is "a fetch happened because the call opened", not a single request.
      expect(toolGets(engine).length).toBeGreaterThan(0);

      await expectNoHorizontalOverflow(page);
      await expectAccessible(page);

      const prefix = { ...held, entries: held.entries.slice(0, 1) };
      const merged = await readTail(page, prefix, prefix.entries.length);
      const whole = await readWhole(page, held.id);
      // The tail read names how many entries were already held. The comparison read is the one full document.
      expect(reads).toEqual([`?since=${prefix.entries.length}`, '']);
      expect(shown(merged.entries)).toEqual(shown(whole.entries));
      expect(shown(merged.entries).map((entry) => entry.Text)).toEqual([EARLIER, ASK, `${STEP}.`, '', ANSWER]);
      const tool = merged.entries.find((entry) => entry.CallID === CALL_ID);
      expect(tool).toMatchObject({ Output: '', OutputOmitted: true, OutputBytes: Buffer.byteLength(BIG) });
    });
  }
}

for (const theme of THEMES) {
  test(`CV-300: a tail that misses the held transcript costs one full read (${theme})`, async ({ page }) => {
    await page.addInitScript((value) => localStorage.setItem('codeaf-theme', value), theme);
    await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
    const engine = await installMockEngine(page, {
      initial: {
        entries: [
          { Role: 'user', Text: EARLIER },
          { Role: 'assistant', Text: 'Middle line', Answer: true },
          { Role: 'assistant', Text: 'Latest line', Answer: true },
        ],
      },
    });
    const reads = trackSnapshotReads(page);
    await openApp(page);
    const live = engine.snapshot();
    // since=2 is a real index in the engine, but the window only holds the first entry, so the tail cannot be joined.
    const held = { ...live, entries: live.entries.slice(0, 1) };
    const merged = await readTail(page, held, 2);
    expect(reads.map((search) => new URLSearchParams(search.startsWith('?') ? search.slice(1) : search).get('since'))).toEqual(['2', null]);
    expect(merged.entries.map((entry) => entry.Text)).toEqual(live.entries.map((entry) => entry.Text));
    expect(reads).toHaveLength(2);
  });
}

test('stream tails merge in order, a header-only record keeps the messages, and a reset replaces them', async ({ page }) => {
  const engine = await installMockEngine(page, { initial: { entries: [{ Role: 'user', Text: EARLIER }] } });
  await openApp(page);
  const held = engine.snapshot();
  const { entries: _entries, ...header } = held;
  const records = [
    { seq: 1, type: 'snapshot', snapshot: { header: { ...header, seq: 1, entryCount: 2 }, from: 1, entries: [{ Role: 'assistant', Text: 'New reply', Answer: true }] } },
    { seq: 2, type: 'snapshot', snapshot: { header: { ...header, seq: 2, title: 'New title', entryCount: 2 }, from: 2, entries: [] } },
    { seq: 3, type: 'snapshot', snapshot: { header: { ...header, seq: 3, entryCount: 1 }, from: 0, reset: true, entries: [{ Role: 'user', Text: 'Compacted' }] } },
    { seq: 4, type: 'snapshot', snapshot: { ...held, seq: 4, entries: [{ Role: 'user', Text: 'Current full transcript' }] } },
  ];
  await page.route('**/api/engine/sessions/*/events?*', (route) => route.fulfill({ contentType: 'text/event-stream', body: records.map((record) => `id: ${record.seq}\ndata: ${JSON.stringify(record)}\n\n`).join('') }));
  const result = await page.evaluate(async (held) => {
    const { watchEngine } = await import(/* @vite-ignore */ '/src/features/chat/engine-client.ts');
    const snapshots: EngineSnapshot[] = [];
    let error = '';
    try {
      await watchEngine(held, (next: EngineSnapshot) => snapshots.push(next), () => {}, new AbortController().signal);
    } catch (caught) {
      error = (caught as Error).message;
    }
    return { snapshots, error };
  }, held);
  expect(result.error).toContain('connection closed');
  expect(result.snapshots.map((snapshot) => snapshot.entries.map((entry) => entry.Text))).toEqual([
    [EARLIER, 'New reply'], [EARLIER, 'New reply'], ['Compacted'], ['Current full transcript'],
  ]);
  expect(result.snapshots[1].title).toBe('New title');
});
