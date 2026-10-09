import { test, expect, type Locator, type Page } from '@playwright/test';
import type { EngineQuestion } from '../../src/features/chat/engine-client';
import { installMockEngine } from './support/mock-engine';
import { pendingQuestion, plainReply } from './support/scenarios';
import { trayQuestions } from './support/scenarios-v2';
import { openApp, posts, send } from './support/conversation';
import { tokenColor } from './contracts';

const tray = (page: Page) => page.getByRole('region', { name: 'Waiting on you' });

const look = (el: Locator) =>
  el.evaluate((node) => {
    const s = getComputedStyle(node);
    return { fill: s.backgroundColor, filter: s.filter, outline: s.outlineStyle, ring: s.boxShadow };
  });

async function openSpecimen(page: Page, scheme: 'light' | 'dark') {
  await page.emulateMedia({ colorScheme: scheme });
  await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
  await openApp(page);
  await page.getByRole('button', { name: 'Design system', exact: true }).click();
}

for (const scheme of ['light', 'dark'] as const) {
  test(`${scheme}: tray answers change fill on hover, darken on press, and keep their own hue`, async ({ page }) => {
    await openSpecimen(page, scheme);
    const card = page.locator('.tray-specimen', { hasText: 'One consent' });
    const danger = card.getByRole('button', { name: 'Allow once' });
    const rest = await look(danger);
    await danger.hover();
    await expect.poll(async () => (await look(danger)).filter).not.toBe('none');
    // The destructive answer never turns into a plain field on hover.
    expect((await look(danger)).fill).toBe(rest.fill);
    const deny = card.getByRole('button', { name: 'Deny', exact: true });
    const denyRest = await look(deny);
    await deny.hover();
    await expect.poll(async () => (await look(deny)).fill).not.toBe(denyRest.fill);
    const hovered = await look(deny);
    await page.mouse.down();
    await expect.poll(async () => (await look(deny)).filter).not.toBe(hovered.filter);
    await page.mouse.move(0, 0);
    await page.mouse.up();
  });

  test(`${scheme}: inline tray fields show no ring on a mouse click`, async ({ page }) => {
    await openSpecimen(page, scheme);
    const card = page.locator('.tray-specimen', { hasText: 'One reversible consent' });
    await card.getByRole('button', { name: 'and say why' }).click();
    const field = card.getByRole('textbox', { name: 'Say why (optional)' });
    const before = await look(field);
    await field.click();
    await expect(field).toBeFocused();
    const after = await look(field);
    expect(after.outline).toBe('none');
    expect(after.ring).toBe(before.ring);
    expect(await field.evaluate((node) => getComputedStyle(node).resize)).toBe('none');
  });

  test(`${scheme}: keyboard focus draws the one shared ring: 2px accent plus the 4px soft halo`, async ({ page }) => {
    await openSpecimen(page, scheme);
    const card = page.locator('.tray-specimen', { hasText: 'One reversible consent' });
    const deny = card.getByRole('button', { name: 'Deny', exact: true });
    await deny.focus();
    await page.keyboard.press('Tab');
    await page.keyboard.press('Tab');
    const why = card.getByRole('button', { name: 'and say why' });
    await expect(why).toBeFocused();
    const ring = await why.evaluate((node) => getComputedStyle(node).boxShadow);
    const accent = await tokenColor(page, 'accent');
    expect(ring).toContain(accent);
    expect(ring).toMatch(/0px 0px 0px 2px/);
    expect(ring).toMatch(/0px 0px 0px 6px/);
    await page.keyboard.press('Enter');
    const field = card.getByRole('textbox', { name: 'Say why (optional)' });
    await page.keyboard.press('Tab');
    await expect(field).toBeFocused();
    expect(await field.evaluate((node) => getComputedStyle(node).boxShadow)).toMatch(/0px 0px 0px 2px/);
  });
}

async function openWithQuestions(page: Page, questions: EngineQuestion[]) {
  const base = pendingQuestion();
  const engine = await installMockEngine(page, { ...base, initial: { ...base.initial, entries: [], needsPerson: false, running: false, questions: [] } });
  await openApp(page);
  await send(page, 'Set up storage');
  await expect.poll(() => posts(engine, '/turn').length).toBe(1);
  engine.update({ needsPerson: true, running: true, questions });
  await expect(async () => {
    await page.reload();
    await expect(tray(page)).toBeVisible({ timeout: 1500 });
  }).toPass({ timeout: 15_000 });
}

test('the tray shrinks to its 40px line only when the reader is over a viewport from the end, and Review brings it back', async ({ page }) => {
  await openWithQuestions(page, trayQuestions());
  await page.addStyleTag({ content: '.conversation-column { min-height: 6000px; }' });
  const scroller = page.locator('.conversation-scroll');
  const line = page.getByRole('region', { name: 'Questions waiting, summary' });
  await expect(line).toBeHidden();
  // A little way up is still the tray.
  await scroller.evaluate((node) => { node.scrollTop = node.scrollHeight - node.clientHeight - 200; });
  await expect(tray(page)).toBeVisible();
  await scroller.evaluate((node) => { node.scrollTop = 0; });
  await expect(line).toBeVisible();
  await expect(tray(page)).toBeHidden();
  await expect.poll(async () => (await line.boundingBox())?.height).toBe(40);
  await expect(line).toContainText('2 need you');
  await line.getByRole('button', { name: 'Review' }).click();
  await expect(tray(page)).toBeVisible();
  await expect(line).toBeHidden();
});
