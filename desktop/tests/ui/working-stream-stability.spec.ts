import { test, expect, type Page } from '@playwright/test';
import { question, taskRows } from './support/scenarios';
import { installMockEngine } from './support/mock-engine';
import type { EngineEvent } from '../../src/features/chat/engine-client';

const event = (kind: string, text = '', call = ''): EngineEvent => ({ kind, text, tool: call ? 'bash' : '', hint: call ? `bash ${call}` : '', raw: call ? { CallID: call, Args: '{"command":"pwd"}', Output: 'checked', Took: 1000000000 } : {} });
// A held SSE transport: the older fixture's finite response reconnects between every push,
// which cannot represent several assistant/tool phases on one live connection.
type FeedWindow = Window & { workingFixtureFeed?: (record: unknown) => void };
async function holdStream(page: Page) {
  await page.addInitScript(() => {
    const original = window.fetch;
    window.fetch = async (input, init) => {
      if (!/\/sessions\/[^/]+\/events\?/.test(String(input))) return original(input, init);
      const stream = new ReadableStream<Uint8Array>({ start(controller) {
        const write = (record: unknown) => controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(record)}\n\n`));
        (window as FeedWindow).workingFixtureFeed = write;
        init?.signal?.addEventListener('abort', () => { controller.close(); if ((window as FeedWindow).workingFixtureFeed === write) delete (window as FeedWindow).workingFixtureFeed; }, { once: true });
      } });
      return new Response(stream, { headers: { 'Content-Type': 'text/event-stream' } });
    };
  });
}
async function feed(page: Page, record: unknown) {
  await expect.poll(() => page.evaluate(() => Boolean((window as FeedWindow).workingFixtureFeed))).toBe(true);
  await page.evaluate(record => (window as FeedWindow).workingFixtureFeed!(record), record);
}
for (const theme of ['light', 'dark']) {
  test(`Working stays stable throughout one multi-step turn and preserves manual choice · ${theme}`, async ({ page }, info) => {
    const engine = await installMockEngine(page, { initial: { entries: [] }, manual: true });
    await page.clock.install();
    await holdStream(page);
    const push = async (next: EngineEvent) => { engine.push(next); await feed(page, { seq: engine.snapshot().seq, type: 'event', event: next }); };
    const update = async (patch: Parameters<typeof engine.update>[0]) => { engine.update(patch); await feed(page, { seq: engine.snapshot().seq, type: 'snapshot', snapshot: engine.snapshot() }); };
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    await page.goto('/');
    const message = page.getByRole('textbox', { name: 'Message', exact: true });
    await message.fill('Investigate both paths'); await message.press('Enter');
    await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
    await push(event('reasoning', 'Tracing the first path.'));
    const toggle = page.locator('.work-toggle').last();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    const clock = toggle.locator('.work-live-time');
    const initialTime = await clock.innerText();
    await page.clock.fastForward(2100);
    await expect(clock).not.toHaveText(initialTime);
    for (const next of [event('toolBegin', '', 'a'), event('toolEnd', '', 'a'), event('text', 'The first path is checked.'), event('assistantDone'), event('reasoning', 'Tracing the second path.'), event('toolBegin', '', 'b'), event('toolEnd', '', 'b'), event('text', 'The second path is checked.')]) {
      await push(next);
      await expect(toggle).toHaveAttribute('aria-expanded', 'true');
      await expect(toggle).toContainText('Working');
    }
    if (process.env.CODEAF_UI_RESULTS) await page.screenshot({ path: `${process.env.CODEAF_UI_RESULTS}/working-active-${theme}-${info.project.name}.png` });
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    const shimmer = toggle.locator('.thinking-shimmer');
    await expect(shimmer).toHaveCSS('animation-duration', '2.4s');
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await expect(shimmer).toHaveCSS('animation-name', 'none');
    const beforeReduced = await clock.innerText();
    await page.clock.fastForward(2100);
    await expect(clock).not.toHaveText(beforeReduced);
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    if (process.env.CODEAF_UI_RESULTS) await page.screenshot({ path: `${process.env.CODEAF_UI_RESULTS}/working-collapsed-${theme}-${info.project.name}.png` });
    await push(event('assistantDone')); await push(event('toolBegin', '', 'c'));
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await toggle.click();
    await expect(page.locator('.work-step-title.thinking-shimmer')).toHaveCount(1);
    await expect(page.locator('.work-block .thinking-shimmer')).toHaveCount(1);
    if (process.env.CODEAF_UI_RESULTS) await page.screenshot({ path: `${process.env.CODEAF_UI_RESULTS}/working-live-step-${theme}-${info.project.name}.png` });
    await toggle.click();
    // The canonical journal replaces provisional block IDs, without taking back the person's disclosure choice.
    const user = engine.snapshot().entries[0];
    const tool = { Role: 'tool', Text: '', Tool: 'bash', CallID: 'a', Hint: 'bash a', Args: '{"command":"pwd"}', Answered: true, Took: 1000000000 } as never;
    const backgroundQuestion = { ...question, blocking: { turn: false, tasks: ['background'] }, asker: { kind: 'task', name: 'Background check' }, subject: { callId: 'c' } } as never;
    await update({ entries: [user, tool], running: true, tasks: [{ ...taskRows[2], ID: 'background', Parent: '', Title: 'Background check', Waiting: true }], questions: [backgroundQuestion] });
    await expect(toggle.locator('.thinking-shimmer')).toHaveCount(1);
    const backgroundTime = await clock.innerText();
    await page.clock.fastForward(2100);
    await expect(clock).not.toHaveText(backgroundTime);
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    if (process.env.CODEAF_UI_RESULTS) await page.screenshot({ path: `${process.env.CODEAF_UI_RESULTS}/working-background-question-${theme}-${info.project.name}.png` });
    await update({ questions: [{ ...question, blocking: { turn: true }, subject: { callId: 'c' } } as never] });
    await expect(toggle.locator('.thinking-shimmer')).toHaveCount(0);
    const waitingTime = await clock.innerText();
    await page.clock.fastForward(2100);
    await expect(clock).toHaveText(waitingTime);
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    if (process.env.CODEAF_UI_RESULTS) await page.screenshot({ path: `${process.env.CODEAF_UI_RESULTS}/working-waiting-${theme}-${info.project.name}.png` });
    // Questions without a call association still pause the whole active turn.
    await update({ questions: [{ ...question, blocking: { turn: true } }] });
    await expect(toggle.locator('.thinking-shimmer')).toHaveCount(0);
    await update({ questions: [] });
    await expect(toggle.locator('.thinking-shimmer')).toHaveCount(1);
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await update({ entries: [user, tool, { Role: 'assistant', Text: 'Both paths are verified.', Answer: true } as never], running: false });
    await expect(page.getByText('Both paths are verified.')).toBeVisible();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await expect(page.locator('.work-block .thinking-shimmer')).toHaveCount(0);
    await expect(clock).toHaveCount(0);
    const finished = await toggle.textContent() ?? '';
    await page.clock.fastForward(2100);
    await expect(toggle).toHaveText(finished);
    await page.reload();
    await expect(page.locator('.work-toggle')).toHaveAttribute('aria-expanded', 'true');
    // A separate turn with no manual choice collapses only when the canonical engine finishes it.
    await message.fill('Check one more path'); await message.press('Enter');
    await push(event('toolBegin', '', 'd')); await push(event('toolEnd', '', 'd')); await push(event('text', 'One more path is checked.'));
    await expect(page.locator('.work-toggle').last()).toHaveAttribute('aria-expanded', 'true');
    const entries = [...engine.snapshot().entries, { ...tool, CallID: 'd', Hint: 'bash d' }, { Role: 'assistant', Text: 'Last check complete.', Answer: true } as never];
    await update({ entries, running: false });
    await expect(page.getByText('Last check complete.')).toBeVisible();
    await expect(page.locator('.work-toggle').last()).toHaveAttribute('aria-expanded', 'false');
    await page.locator('.answer-block').last().hover();
    await page.locator('.answer-worked').last().click();
    await expect(page.locator('.work-toggle').last()).toHaveAttribute('aria-expanded', 'true');
    await page.locator('.work-toggle').last().click();
    await expect(page.locator('.work-toggle').last()).toHaveAttribute('aria-expanded', 'false');
    if (process.env.CODEAF_UI_RESULTS) await page.screenshot({ path: `${process.env.CODEAF_UI_RESULTS}/working-finished-${theme}-${info.project.name}.png` });
    expect(engine.calls.filter(call => call.path.endsWith('/stop'))).toHaveLength(0);
  });
}
