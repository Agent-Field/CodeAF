import { openPage } from './support/shell-navigation';
import { test, expect, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { plainReply } from './support/scenarios';

// Terminal and job tabs (design 3c) against the mock engine: output stream, input, resize, state words,
// Ask codeaf about this output, Remove job, and the limit line.
const SESSION = 'mock-session-1.jsonl';
const scenario = (): Scenario => ({
  ...plainReply(),
  terminals: [
    { id: 'shell-1', title: 'zsh', output: '$ ' },
    { id: 'job-1', command: 'nightly-bench', title: 'nightly-bench', output: 'goos: darwin\r\npkg: codeaf/internal/parse\r\n\x1b[32mok\x1b[0m codeaf/parse 4.2s\r\n\x1b[31mFAIL\x1b[0m codeaf/load\r\nBenchmarkLoadAll-10\r\n' },
    { id: 'failed-1', command: 'make lint', title: 'make lint', state: 'exited', exitCode: 2, endedAt: new Date(Date.now() - 60_000).toISOString(), output: '\x1b[31mlint failed\x1b[0m\r\n' },
    { id: 'done-1', command: 'make test', title: 'make test', state: 'exited', exitCode: 0, endedAt: new Date(Date.now() - 120_000).toISOString(), output: 'all green\r\n' },
  ],
});

/** Opens the app on a workspace whose only tab shows an engine terminal that already exists. */
async function openOn(page: Page, terminalId: string, title: string) {
  await page.addInitScript(([id, name, terminal, session]) => {
    if (localStorage.getItem('codeaf.desktop.workspace.v1')) return;
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [{ id, kind: 'terminal', title: name, draft: '', pinned: false }], groups: [], activeId: id, closed: [], nextNumber: 2, recentIds: [id] }));
    localStorage.setItem('codeaf.desktop.terminals.v1', JSON.stringify({ [id]: { sessionFile: session, terminalId: terminal } }));
  }, ['term-tab', title, terminalId, SESSION]);
  await page.goto('/');
  await expect(page.locator('.terminal-header')).toBeVisible();
}
const screenText = (page: Page) => page.locator('.terminal-pane .xterm-rows');
const header = (page: Page) => page.locator('.terminal-header');
const tabs = (page: Page) => page.getByRole('tab');

test.beforeEach(async ({ page }) => { await page.addInitScript(() => { try { localStorage.removeItem('codeaf-theme'); } catch { /* none */ } }); });

test('a job tab shows its header, replays the log on the terminal field and keeps the design geometry', async ({ page }) => {
  await installMockEngine(page, scenario());
  await openOn(page, 'job-1', 'nightly-bench');
  await expect(header(page).locator('.terminal-title')).toHaveText('nightly-bench');
  await expect(header(page).locator('.terminal-meta')).toHaveText('/mock-workspace · job');
  await expect(header(page).locator('.terminal-state')).toHaveText(/^Running · \d+s$/);
  await expect(screenText(page)).toContainText('ok codeaf/parse 4.2s');
  await expect(screenText(page)).toContainText('BenchmarkLoadAll-10');
  const box = async (selector: string) => (await page.locator(selector).first().boundingBox())!;
  expect((await box('.terminal-header')).height).toBe(48);
  const field = await box('.terminal-field');
  expect(await page.locator('.terminal-field').evaluate(el => getComputedStyle(el).backgroundColor)).toBe(await page.evaluate(() => { const p = document.createElement('i'); p.style.color = 'var(--term)'; document.body.append(p); const c = getComputedStyle(p).color; p.remove(); return c; }));
  expect(await page.locator('.terminal-field').evaluate(el => getComputedStyle(el).maskImage)).toContain('80px');
  const ask = await box('.terminal-ask-field');
  expect(ask.height).toBe(40);
  expect(ask.width).toBeLessThanOrEqual(600);
  expect(ask.y).toBeGreaterThan(field.y + field.height);
  expect(await page.locator('.terminal-pane .xterm-rows').evaluate(el => getComputedStyle(el).fontSize)).toBe('12.5px');
  // A job reads from the bottom edge until it fills the field.
  const last = (await page.locator('.terminal-pane .xterm-rows > div').filter({ hasText: 'BenchmarkLoadAll-10' }).boundingBox())!;
  expect(last.y + last.height).toBeGreaterThan(field.y + field.height - 80);
});

test('ANSI colours are token hues at about 60% chroma, in light and dark', async ({ page }) => {
  await installMockEngine(page, scenario());
  const measure = () => page.evaluate(async () => {
    const ansi = await import(/* @vite-ignore */ '/src/features/terminal/ansi.ts');
    const theme = await import(/* @vite-ignore */ '/src/features/terminal/theme.ts');
    const scope = document.querySelector<HTMLElement>('.terminal-host')!;
    const span = [...scope.querySelectorAll<HTMLElement>('.xterm-rows span')].find(s => s.textContent === 'ok')!;
    const painted = getComputedStyle(span).color.match(/\d+/g)!.slice(0, 3).map(Number);
    const token = theme.tokenColor(scope, 'success');
    const field = theme.tokenColor(scope, 'term');
    return { painted, expected: ansi.readable(ansi.desaturate(token, 0.6), field), ratio: ansi.toLch(painted)[1] / ansi.toLch(token)[1], field };
  });
  await page.emulateMedia({ colorScheme: 'light' });
  await openOn(page, 'job-1', 'nightly-bench');
  await expect(screenText(page)).toContainText('ok codeaf/parse');
  const light = await measure();
  light.painted.forEach((channel, i) => expect(Math.abs(channel - light.expected[i])).toBeLessThanOrEqual(2));
  expect(light.ratio).toBeGreaterThan(0.35); expect(light.ratio).toBeLessThan(0.65);
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect.poll(async () => (await measure()).field.join()).not.toBe(light.field.join());
  // The screen re-themes when the appearance attribute changes, a frame after the tokens do.
  await expect.poll(async () => { const now = await measure(); return Math.max(...now.painted.map((channel, i) => Math.abs(channel - now.expected[i]))); }).toBeLessThanOrEqual(2);
  const dark = await measure();
  expect(dark.painted.join()).not.toBe(light.painted.join());
});

test('the new terminal key opens a shell under the conversation, types into it and follows its output across a reload', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await page.goto('/');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
  await page.keyboard.press('Control+Backquote');
  await expect(tabs(page)).toHaveCount(2);
  await expect(page.getByRole('tab', { name: 'zsh', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(header(page).locator('.terminal-meta')).toHaveText('/mock-workspace · terminal');
  await expect(page.locator('.terminal-field')).toHaveCSS('mask-image', 'none');
  await expect(screenText(page)).toContainText('mock$');
  const start = engine.calls.filter(c => c.method === 'POST' && c.path.endsWith('/terminals'));
  expect(start).toHaveLength(1);
  await page.keyboard.type('ls');
  await expect.poll(() => engine.calls.filter(c => c.path.endsWith('/input')).map(c => Buffer.from(String((c.body as { dataBase64: string }).dataBase64), 'base64').toString()).join('')).toBe('ls');
  await expect(screenText(page)).toContainText('mock$ ls');
  // The key also works from inside the terminal; xterm does not swallow it.
  await page.keyboard.press('Control+Backquote');
  await expect(tabs(page)).toHaveCount(3);
  await tabs(page).nth(1).click();
  // Canonical tabs are shared; active focus is saved only in this window's copy.
  // Reload once both records describe the terminal the person actually selected.
  await expect.poll(() => page.evaluate(async () => {
    const record = await (await fetch('/api/engine/workspaces/now')).json();
    const tab = record.workspace?.tabs.find((tab: { target?: { terminalId?: string } }) => tab.target?.terminalId === 'mock-term-1');
    const writer = sessionStorage.getItem('codeaf.desktop.window-id');
    const raw = writer && localStorage.getItem(`codeaf.desktop.workspace-sync.v1:now:${writer}`);
    const local = raw ? JSON.parse(raw).local : undefined;
    const selected = document.querySelector('.workspace-tabstrip [role="tab"][aria-selected="true"]');
    return !!tab && local?.activeId === tab.id && selected?.id === `tab-${tab.id}`;
  }), { message: 'canonical terminal identity and this window saved focus agree before reload' }).toBe(true);
  await page.reload();
  await expect(header(page).locator('.terminal-title')).toHaveText('zsh');
  await expect(screenText(page)).toContainText('mock$ ls');
});

const sent = (engine: Awaited<ReturnType<typeof installMockEngine>>) => engine.calls.filter(c => c.path.endsWith('/input')).map(c => Buffer.from(String((c.body as { dataBase64: string }).dataBase64), 'base64').toString()).join('');

test('Linux: plain Ctrl editing chords typed into xterm reach the shell and never act on the app', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await page.goto('/');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
  await page.keyboard.press('Control+Backquote');
  await expect(header(page)).toBeVisible();
  await expect(screenText(page)).toContainText('mock$');
  await page.locator('.xterm-helper-textarea').focus();
  for (const k of ['w', 't', 'k', 's', 'b', 'y', '1', '9']) await page.keyboard.press(`Control+${k}`);
  // W T K S B Y are the shell's own control codes; the digits (jump-to-tab on the app side) must not switch tabs either.
  await expect.poll(() => sent(engine).startsWith('\x17\x14\x0b\x13\x02\x19')).toBe(true);
  await expect(tabs(page)).toHaveCount(2);
  await expect(header(page)).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('Linux: Ctrl+Shift+T and Ctrl+Shift+W are the desktop new and close chords in a terminal, Ctrl+Backquote still opens a shell', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await page.goto('/');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
  await page.keyboard.press('Control+Backquote');
  await expect(tabs(page)).toHaveCount(2);
  await page.locator('.xterm-helper-textarea').focus();
  await page.keyboard.press('Control+Shift+T');
  await expect(tabs(page)).toHaveCount(3);
  await page.getByRole('tab', { name: 'zsh', exact: true }).first().click();
  await page.locator('.xterm-helper-textarea').focus();
  await page.keyboard.press('Control+Shift+W');
  await expect(tabs(page)).toHaveCount(2);
  expect(sent(engine)).toBe('');
});

test('Linux: outside a terminal Ctrl+W still closes the tab', async ({ page }) => {
  await installMockEngine(page, scenario());
  await page.goto('/');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
  await page.keyboard.press('Control+Backquote');
  await expect(tabs(page)).toHaveCount(2);
  await page.getByRole('tab').first().click();
  await page.getByRole('textbox', { name: 'Message', exact: true }).focus();
  await page.keyboard.press('Control+T');
  await expect(tabs(page)).toHaveCount(3);
  await page.keyboard.press('Control+W');
  await expect(tabs(page)).toHaveCount(2);
});

test.describe('Mac', () => {
  test.beforeEach(async ({ page }) => { await page.addInitScript(() => Object.defineProperty(navigator, 'platform', { value: 'MacIntel', configurable: true })); });
  test('Cmd+W from inside a terminal still closes the tab and nothing is typed', async ({ page }) => {
    const engine = await installMockEngine(page, scenario());
    await page.goto('/');
    await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
    await page.keyboard.press('Control+Backquote');
    await expect(tabs(page)).toHaveCount(2);
    await page.locator('.xterm-helper-textarea').focus();
    await page.keyboard.press('Meta+W');
    await expect(tabs(page)).toHaveCount(1);
    expect(sent(engine)).toBe('');
  });
});

test('the screen size reaches the engine: once on attach, again when the window changes', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await openOn(page, 'job-1', 'nightly-bench');
  const resizes = () => engine.calls.filter(c => c.path.endsWith('/resize')).map(c => c.body as { cols: number; rows: number });
  await expect.poll(() => resizes().length).toBeGreaterThan(0);
  const before = resizes().at(-1)!;
  expect(before.cols).toBeGreaterThan(40);
  await page.setViewportSize({ width: 760, height: 520 });
  await expect.poll(() => resizes().at(-1)!.cols).toBeLessThan(before.cols);
  expect(resizes().at(-1)!.rows).toBeLessThan(before.rows);
});

test('a finished job shows its exit words, takes no input, and keeps its log', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await openOn(page, 'done-1', 'make test');
  await expect(header(page).locator('.terminal-state')).toHaveText(/^exit 0 · 2m \d+s ago$/);
  await expect(header(page).locator('.terminal-mark')).toHaveAttribute('data-tone', 'done');
  await expect(screenText(page)).toContainText('all green');
  await expect(page.getByRole('button', { name: 'Stop', exact: true })).toHaveCount(0);
  await page.locator('.terminal-field').click();
  await page.keyboard.type('x');
  expect(engine.calls.filter(c => c.path.endsWith('/input'))).toHaveLength(0);
});

test('Stop ends a running job and its log stays until Remove job', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await openOn(page, 'job-1', 'nightly-bench');
  await expect(header(page).locator('.terminal-mark')).toHaveAttribute('data-tone', 'running');
  await page.getByRole('button', { name: 'Stop', exact: true }).click();
  await expect(header(page).locator('.terminal-state')).toHaveText(/^closed · \d+s ago$/);
  await expect(header(page).locator('.terminal-mark')).toHaveAttribute('data-tone', 'stopped');
  await expect(screenText(page)).toContainText('ok codeaf/parse 4.2s');
  expect(engine.calls.filter(c => c.path.endsWith('/close'))).toHaveLength(1);
  const listed = () => page.evaluate(async () => {
    const client = await import(/* @vite-ignore */ '/src/features/chat/engine-client.ts');
    return (await client.listTerminals((await client.connectEngine()).id)).map((t: { id: string }) => t.id);
  });
  expect(await listed()).toContain('job-1');
  await page.getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Remove job', exact: true }).click();
  await expect(page.locator('.terminal-header')).toHaveCount(0);
  expect(engine.calls.filter(c => c.path.endsWith('/remove'))).toHaveLength(1);
  expect(await listed()).not.toContain('job-1');
});

test('Ask codeaf about this output starts a new conversation with the output attached, in a new tab', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await openOn(page, 'job-1', 'nightly-bench');
  await expect(screenText(page)).toContainText('ok codeaf/parse');
  const field = page.getByRole('textbox', { name: 'Ask codeaf about this output', exact: true });
  await field.fill('Why is parse slow?');
  await field.press('Enter');
  await expect(tabs(page)).toHaveCount(2);
  await expect(tabs(page).nth(1)).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
  const turn = engine.calls.filter(c => c.path.endsWith('/turn')).at(-1)!.body as { text: string; files: { name: string; mime: string; dataBase64: string }[] };
  expect(turn.text).toBe('Why is parse slow?');
  expect(turn.files[0]).toMatchObject({ name: 'nightly-bench-output.txt', mime: 'text/plain' });
  expect(Buffer.from(turn.files[0].dataBase64, 'base64').toString()).toContain('ok codeaf/parse 4.2s');
  expect(engine.calls.filter(c => c.method === 'POST' && c.path.endsWith('/sessions')).length).toBeGreaterThan(1);
});

test('the limit line shows when the engine refuses a terminal, and Try again starts one', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await page.goto('/');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
  const refuse = (route: import('@playwright/test').Route) => route.request().method() === 'POST' ? route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: '16 terminals are already running; close one first' }) }) : route.fallback();
  await page.route('**/terminals', refuse);
  await page.keyboard.press('Control+Backquote');
  await expect(page.locator('.terminal-note')).toContainText('16 terminals are open in this conversation. Close one to start another.');
  await expect(page.locator('.terminal-ask-field')).toHaveCount(0);
  await page.unroute('**/terminals', refuse);
  await page.getByRole('button', { name: 'Try again', exact: true }).click();
  await expect(header(page).locator('.terminal-title')).toHaveText('zsh');
  await expect(screenText(page)).toContainText('mock$');
  expect(engine.calls.filter(c => c.method === 'POST' && c.path.endsWith('/terminals'))).toHaveLength(1);
});

test('a log older than the engine keeps says so on its first line', async ({ page }) => {
  await installMockEngine(page, scenario());
  await page.route('**/terminals/job-1/stream*', route => route.fulfill({ status: 200, contentType: 'text/event-stream', body: `data: ${JSON.stringify({ seq: 12, type: 'output', cut: true, dataBase64: Buffer.from('kept line\r\n').toString('base64') })}\n\n` }));
  await openOn(page, 'job-1', 'nightly-bench');
  await expect(screenText(page)).toContainText('… earlier output trimmed to the last 512 KB');
  await expect(screenText(page)).toContainText('kept line');
});

test('a finished job offers Run again, which starts the same command in a new tab', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await openOn(page, 'done-1', 'make test');
  await page.getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Run again', exact: true }).click();
  await expect(tabs(page)).toHaveCount(2);
  await expect(tabs(page).nth(1)).toHaveAttribute('aria-selected', 'true');
  const start = engine.calls.filter(c => c.method === 'POST' && c.path.endsWith('/terminals'));
  expect(start).toHaveLength(1);
  expect(start[0].body).toMatchObject({ command: 'make test' });
  await expect(header(page).locator('.terminal-meta')).toHaveText('/mock-workspace · job');
});

for (const [id, title, remove] of [['job-1', 'nightly-bench', 'Remove job'], ['shell-1', 'zsh', 'Remove terminal']]) {
  test(`a running ${title} menu offers only copy and remove`, async ({ page }) => {
    await installMockEngine(page, scenario());
    await openOn(page, id, title);
    await expect(header(page).locator('.terminal-state')).toContainText('Running');
    await page.getByRole('button', { name: 'More', exact: true }).click();
    await expect(page.getByRole('menu').locator('.menu-label')).toHaveText(['Copy output', remove]);
    await expect(page.getByRole('menu').locator('[data-disabled]')).toHaveCount(0);
  });
}

test('the tab carries the failed dot only when the program ended with an error; running stays silent', async ({ page }) => {
  await installMockEngine(page, scenario());
  await openOn(page, 'failed-1', 'make lint');
  await expect(header(page).locator('.terminal-mark')).toHaveAttribute('data-tone', 'failed');
  await expect(page.locator('.workspace-tab[data-kind="terminal"]')).toHaveAttribute('data-state', 'failed');
  await expect(page.getByRole('tab', { name: 'make lint', exact: true })).toHaveAttribute('aria-description', /.+/);
});

test('a running job tab draws its terminal icon, not a state', async ({ page }) => {
  await installMockEngine(page, scenario());
  await openOn(page, 'job-1', 'nightly-bench');
  await expect(header(page).locator('.terminal-mark')).toHaveAttribute('data-tone', 'running');
  await expect(page.locator('.workspace-tab[data-kind="terminal"]')).not.toHaveAttribute('data-state', /.+/);
});

test('a terminal the engine no longer has says so', async ({ page }) => {
  await installMockEngine(page, scenario());
  await openOn(page, 'missing-1', 'Gone');
  await expect(page.locator('.terminal-note')).toContainText('This terminal is gone.');
});

test('the tab is accessible in light and dark, and the specimen draws both cards', async ({ page }) => {
  await installMockEngine(page, scenario());
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await openOn(page, 'job-1', 'nightly-bench');
    await expect(screenText(page)).toContainText('ok codeaf/parse');
    await expectAccessible(page);
    await page.goto('about:blank');
  }
  await page.goto('/');
  // Wait for attachment's own focus effect before moving focus to app chrome.
  await expect(screenText(page)).toContainText('ok codeaf/parse');
  await expect(page.locator('.xterm-helper-textarea')).toBeFocused();
  // Ctrl+K belongs to the shell while xterm has focus; navigate from the app chrome.
  const chrome = page.getByRole('button', { name: 'New tab', exact: true });
  await chrome.focus();
  await expect(chrome).toBeFocused();
  await openPage(page, 'Design system');
  const cards = page.locator('.terminal-specimen-card');
  await expect(cards).toHaveCount(2);
  await expect(cards.first().locator('.xterm-rows')).toContainText('BenchmarkLoadAll-10');
  await expect(cards.last().locator('.terminal-state')).toHaveText('exit 2 · 3m 5s ago');
});


test('the new-tab terminal action starts an interactive shell without sending a model message', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await page.goto('/');
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await page.getByRole('option', { name: /New terminal/ }).click();
  await expect(header(page).locator('.terminal-meta')).toHaveText('/mock-workspace · terminal');
  await expect(page.locator('.terminal-field')).toHaveCSS('mask-image', 'none');
  await expect(screenText(page)).toContainText('mock$');
  expect(engine.calls.filter(c => c.method === 'POST' && c.path.endsWith('/terminals'))).toHaveLength(1);
  expect(engine.calls.filter(c => c.method === 'POST' && c.path.endsWith('/send'))).toHaveLength(0);
  await page.keyboard.type('pwd');
  await expect(screenText(page)).toContainText('mock$ pwd');
  await page.reload();
  await expect(screenText(page)).toContainText('mock$ pwd');
});

test('failed Stop keeps the terminal attached and shows the engine error', async ({ page }) => {
  await installMockEngine(page, scenario());
  await page.route('**/api/engine/sessions/*/terminals/*/close', route => route.fulfill({ status: 503, json: { error: 'connection lost' } }));
  await openOn(page, 'job-1', 'nightly-bench');
  await expect(screenText(page)).toContainText('BenchmarkLoadAll-10');
  await header(page).getByRole('button', { name: 'Stop', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Could not stop: connection lost');
  await expect(screenText(page)).toContainText('BenchmarkLoadAll-10');
  await expect(header(page).locator('.terminal-state')).toHaveText(/^Running/);
});


test('a finished job menu closes its tab without removing the job or stopping it', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await openOn(page, 'done-1', 'make test');
  await expect(header(page).locator('.terminal-state')).toContainText('exit 0');
  await page.getByRole('button', { name: 'More', exact: true }).click();
  await expect(page.getByRole('menu').locator('.menu-label')).toHaveText(['Run again', 'Close tab', 'Remove job']);
  await page.getByRole('menuitem', { name: /Close tab/ }).click();
  await expect(header(page)).toHaveCount(0);
  expect(engine.calls.filter(c => /\/(remove|close)$/.test(c.path))).toHaveLength(0);
});
