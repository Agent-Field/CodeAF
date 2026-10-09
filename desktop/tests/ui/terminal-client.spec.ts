import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

// The typed terminal client against the mock engine, through the real module.
const scenario = () => ({
  ...plainReply(),
  terminals: [
    { id: 'job-1', command: 'nightly-bench', title: 'nightly-bench', output: 'goos: darwin\r\n\x1b[32mok\x1b[0m codeaf/parse 4.2s\r\n' },
    { id: 'done-1', command: 'make test', title: 'make test', state: 'exited' as const, exitCode: 0, endedAt: new Date(Date.now() - 120_000).toISOString(), output: 'all green\r\n' },
  ],
});

test.beforeEach(async ({ page }) => { await page.goto('/'); });

test('lists jobs, replays the kept log and reports exit', async ({ page }) => {
  await installMockEngine(page, scenario());
  const result = await page.evaluate(async () => {
    const client = await import(/* @vite-ignore */ '/src/features/chat/engine-client.ts');
    const snap = await client.connectEngine();
    const list = await client.listTerminals(snap.id);
    const chunks: string[] = []; let exit: unknown;
    await client.watchTerminal(snap.id, 'done-1', 0, (bytes: Uint8Array) => chunks.push(new TextDecoder().decode(bytes)), (info: unknown) => { exit = info; }, new AbortController().signal);
    return { ids: list.map((t: { id: string }) => t.id), chunks, exit, words: client.terminalStateWords(list[1]) };
  });
  expect(result.ids).toEqual(['job-1', 'done-1']);
  expect(result.chunks.join('')).toBe('all green\r\n');
  expect(result.exit).toMatchObject({ state: 'exited', exitCode: 0 });
  expect(result.words).toMatch(/^exit 0 · 2m 0s ago$/);
});

test('starts, types into, resizes and closes a terminal', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  const result = await page.evaluate(async () => {
    const client = await import(/* @vite-ignore */ '/src/features/chat/engine-client.ts');
    const snap = await client.connectEngine();
    const t = await client.startTerminal(snap.id, { cols: 80, rows: 24 });
    await client.writeTerminal(snap.id, t.id, 'ls é\n');
    const resized = await client.resizeTerminal(snap.id, t.id, 120, 40);
    const out = await client.readTerminalOutput(snap.id, t.id);
    await client.closeTerminal(snap.id, t.id);
    const after = await client.listTerminals(snap.id);
    return { kind: t.kind, resized: [resized.cols, resized.rows], text: out.text, left: after.map((x: { id: string }) => x.id) };
  });
  expect(result).toMatchObject({ kind: 'terminal', resized: [120, 40] });
  expect(result.text).toBe('mock$ ls é');
  expect(result.left).toEqual(['job-1', 'done-1']);
  expect(engine.calls.filter(c => c.path.endsWith('/input'))).toHaveLength(1);
});

test('ask codeaf attaches the selection or the recent plain output through the attachment path', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  const result = await page.evaluate(async () => {
    const client = await import(/* @vite-ignore */ '/src/features/chat/engine-client.ts');
    const snap = await client.connectEngine();
    await client.askAboutTerminalOutput({ sessionId: snap.id, id: 'job-1' }, 'Why is parse slow?', { conversationId: snap.id });
    await client.askAboutTerminalOutput({ sessionId: snap.id, id: 'job-1' }, '', { conversationId: snap.id, selection: ' ok codeaf/parse ' });
    return null;
  });
  expect(result).toBeNull();
  const turns = engine.calls.filter(c => c.path.endsWith('/turn'));
  expect(turns).toHaveLength(2);
  const first = turns[0].body as { text: string; files: { name: string; mime: string; dataBase64: string }[] };
  expect(first.text).toBe('Why is parse slow?');
  expect(first.files[0]).toMatchObject({ name: 'nightly-bench-output.txt', mime: 'text/plain' });
  expect(Buffer.from(first.files[0].dataBase64, 'base64').toString()).toBe('goos: darwin\nok codeaf/parse 4.2s');
  const second = turns[1].body as { text: string; files: { name: string; dataBase64: string }[] };
  expect(second.text).toBe('What is going on in this output?');
  expect(second.files[0].name).toBe('nightly-bench-selection.txt');
  expect(Buffer.from(second.files[0].dataBase64, 'base64').toString()).toBe('ok codeaf/parse');
});

test('state words for a running job, a closed job and a missing session', async ({ page }) => {
  await installMockEngine(page, scenario());
  const words = await page.evaluate(async () => {
    const client = await import(/* @vite-ignore */ '/src/features/chat/engine-client.ts');
    const base = { id: 'x', kind: 'job', title: 't', cwd: '/', shell: 'sh', cols: 1, rows: 1, bytes: 0, durationMs: 0 };
    const now = Date.parse('2026-01-01T00:10:00Z');
    return [
      client.terminalStateWords({ ...base, state: 'running', startedAt: '2026-01-01T00:07:46Z' }, now),
      client.terminalStateWords({ ...base, state: 'closed', exitCode: 129, startedAt: '2026-01-01T00:00:00Z', endedAt: '2026-01-01T00:08:00Z' }, now),
    ];
  });
  expect(words).toEqual(['Running · 2m 14s', 'closed · 2m 0s ago']);
});
