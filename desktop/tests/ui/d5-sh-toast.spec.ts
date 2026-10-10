import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, expectDesignSurface } from './contracts';
import { installMockEngine } from './support/mock-engine';

const NOW = new Date('2026-10-10T16:00:00Z');
const modifier = process.platform === 'darwin' ? 'Meta' : 'Control';
const titles = ['Config stack', 'Lexer rewrite'];

async function launch(page: Page, theme: string, width: number, archive = false) {
  await page.setViewportSize({ width, height: 720 });
  await page.clock.install({ time: NOW });
  await page.clock.pauseAt(NOW);
  const engine = await installMockEngine(page, {
    initial: { running: !archive, title: '', entries: [{ Role: 'user', Text: 'Fix the config' }] },
    history: { conversations: [{ id: 'old', title: 'Old chat', at: NOW.toISOString(), messages: [] }] },
  });
  const tabs = archive
    ? [{ id: 'intro', title: 'Intro' }, { id: 'old', title: 'Old chat', sessionFile: '/mock/places/old/transcript.jsonl' }]
    : [{ id: 'intro', title: 'Intro' }, ...titles.map((title, index) => ({ id: `running-${index}`, title, sessionFile: 'mock-session-1.jsonl' }))];
  await page.addInitScript(({ theme, tabs, archive, stamp }) => {
    localStorage.setItem('codeaf-theme', theme);
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({
      tabs: tabs.map(tab => ({ ...tab, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual' })),
      groups: [], closed: [], activeId: archive ? 'intro' : 'running-0', nextNumber: 4, recentIds: ['intro'],
    }));
    if (archive) localStorage.setItem('codeaf.desktop.activity.v1', JSON.stringify({ old: { at: stamp - 13 * 3_600_000, hold: false } }));
  }, { theme, tabs, archive, stamp: NOW.getTime() });
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
  if (!archive) {
    await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
    await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
  }
  return engine;
}

async function closeRunning(page: Page, title: string) {
  await page.getByRole('tab', { name: title, exact: true }).click();
  await page.keyboard.press(`${modifier}+w`);
  await expect(page.locator('.toast-text')).toHaveText(`${title} closed and still running`);
  await expect(page.getByRole('tab', { name: title, exact: true })).toHaveCount(0);
  // Closing restores strip focus on the next frame; complete it before testing the notification's keyboard path.
  await page.clock.runFor(250);
}

async function surface(page: Page, width: number) {
  const region = page.getByRole('status', { name: 'Notifications' });
  const card = region.locator('.toast');
  await expect(card).toHaveCount(1);
  await expect(region).toHaveAttribute('aria-live', 'polite');
  await expect(region).toHaveAttribute('aria-atomic', 'true');
  await expectDesignSurface(page, card);
  const geometry = await card.evaluate(el => {
    const r = el.getBoundingClientRect(), s = getComputedStyle(el);
    return { left: r.left, right: r.right, center: r.x + r.width / 2, height: r.height, radius: s.borderRadius, padding: s.padding, gap: s.gap };
  });
  // Measured from v-Components.html's closing toast, shared with the archive notice (SH-146/149).
  expect(geometry.height).toBeCloseTo(40, 2);
  expect(geometry.radius).toBe('12px');
  expect(geometry.padding).toBe('0px 6px 0px 14px');
  expect(geometry.gap).toBe('12px');
  expect(geometry.center).toBeCloseTo(width / 2, 0);
  expect(geometry.left).toBeGreaterThanOrEqual(8);
  expect(geometry.right).toBeLessThanOrEqual(width - 8);
  await expect(region).toHaveCSS('bottom', '24px');
  for (const button of await card.getByRole('button').all()) {
    await expect(button).toBeVisible();
    await expect(button).toHaveCSS('height', '26px');
  }
  return card;
}

async function tabTo(page: Page, name: string) {
  const action = page.locator('.toast').getByRole('button', { name, exact: true });
  // Traverse the real document order rather than focusing the action directly.
  for (let i = 0; i < 40; i++) {
    await page.keyboard.press('Tab');
    if (await action.evaluate(el => el === document.activeElement)) return action;
  }
  throw new Error(`Tab did not reach toast action ${name}`);
}

for (const theme of ['light', 'dark']) for (const width of [320, 600, 1200]) {
  test(`SH-146–149 closing: timing, replacement, keyboard and actions · ${theme} ${width}px`, async ({ page }) => {
    const engine = await launch(page, theme, width);
    await closeRunning(page, titles[0]);
    const card = await surface(page, width);
    await expect(card.getByRole('button')).toHaveText(['Stop it', 'Undo']);
    expect(engine.calls.filter(call => call.path.endsWith('/stop'))).toHaveLength(0);
    await page.clock.runFor(1749);
    await card.hover();
    await page.clock.runFor(10000);
    await expect(card).toBeVisible();
    const stop = await tabTo(page, 'Stop it');
    await expect(stop).toBeFocused();
    await page.mouse.move(0, 0);
    await page.clock.runFor(10000);
    await expect(card).toBeVisible();
    await page.keyboard.press('Tab');
    const undo = card.getByRole('button', { name: 'Undo', exact: true });
    await expect(undo).toBeFocused();
    await page.clock.runFor(10000);
    await expect(card).toBeVisible();
    await page.keyboard.press('Tab');
    await page.clock.runFor(4000);
    await expect(page.locator('.toast-region')).not.toHaveAttribute('data-exiting');
    await page.clock.runFor(1);
    await expect(page.locator('.toast-region')).toHaveAttribute('data-exiting', 'true');
    await page.clock.runFor(200);
    await expect(card).toHaveCount(0);

    await page.getByRole('tab', { name: titles[1], exact: true }).click();
    await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
    await page.keyboard.press(`${modifier}+Shift+t`);
    await expect(page.getByRole('tab', { name: titles[0], exact: true })).toBeVisible();
    await closeRunning(page, titles[0]);
    await page.clock.runFor(3000);
    await closeRunning(page, titles[1]);
    await page.clock.runFor(5499);
    await expect(card).toHaveCount(1);
    await expect(card).not.toContainText(titles[0]);
    await tabTo(page, 'Stop it');
    await page.keyboard.press('Tab');
    await expect(undo).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('tab', { name: titles[1], exact: true })).toBeVisible();
    expect(engine.snapshot().running).toBe(true);

    await closeRunning(page, titles[1]);
    await card.hover();
    await page.clock.resume();
    await expectAccessible(page, '.toast-region');
    await tabTo(page, 'Stop it');
    await page.keyboard.press('Escape');
    await expect(card).toHaveCount(0);
    await page.keyboard.press(`${modifier}+Shift+t`);
    await closeRunning(page, titles[1]);
    await tabTo(page, 'Stop it');
    await page.keyboard.press('Enter');
    await expect.poll(() => engine.calls.filter(call => call.path.endsWith('/stop')).length).toBe(1);
    await expect(card).toHaveCount(0);
  });

  test(`SH-146–149 auto-archive uses the shared surface · ${theme} ${width}px`, async ({ page }) => {
    const engine = await launch(page, theme, width, true);
    const card = await surface(page, width);
    await expect(card).toContainText('Archived 1 tab idle for more than 12h');
    await expect(card.getByRole('button')).toHaveText(['Review', 'Restore all']);
    await expect(card.locator('.toast-icon .app-icon')).toHaveCount(1);
    await card.hover();
    await page.clock.runFor(10000);
    await expect(card).toBeVisible();
    await tabTo(page, 'Review');
    await page.mouse.move(0, 0);
    await page.clock.runFor(10000);
    await expect(card).toBeVisible();
    await page.keyboard.press('Tab');
    await expect(card.getByRole('button', { name: 'Restore all' })).toBeFocused();
    await page.clock.resume();
    await expectAccessible(page, '.toast-region');
    await page.keyboard.press('Enter');
    await expect(page.getByRole('tab', { name: /Old chat/ })).toBeVisible();
    await expect.poll(() => engine.history.archived()).toEqual([{ id: 'old', archived: true }, { id: 'old', archived: false }]);
    await expect(card).toHaveCount(0);
  });
}

for (const theme of ['light', 'dark']) test(`SH-150 toast rise is removed by reduced motion · ${theme}`, async ({ page }) => {
  await launch(page, theme, 320);
  await closeRunning(page, titles[0]);
  const card = page.locator('.toast');
  await expect(card).toHaveCSS('animation-name', 'toast-enter');
  await expect(card).toHaveCSS('animation-duration', '0.2s');
  const frames = await card.evaluate(el => el.getAnimations().flatMap(animation => (animation.effect as KeyframeEffect).getKeyframes()).map(frame => frame.transform));
  expect(frames).toContain('translateY(8px)');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect(card).toHaveCSS('animation-name', 'none');
  await expect(card).toHaveCSS('transform', 'none');
  expect(await card.evaluate(el => el.getAnimations().length)).toBe(0);
});

for (const theme of ['light', 'dark']) test(`SH-146 archive expires after six seconds · ${theme}`, async ({ page }) => {
  await launch(page, theme, 600, true);
  const region = page.getByRole('status', { name: 'Notifications' });
  await expect(region.locator('.toast')).toContainText('Archived 1 tab');
  await page.clock.runFor(5999);
  await expect(region).not.toHaveAttribute('data-exiting');
  await page.clock.runFor(1);
  await expect(region).toHaveAttribute('data-exiting', 'true');
  await page.clock.runFor(200);
  await expect(region.locator('.toast')).toHaveCount(0);
  await expect(page.getByRole('tab', { name: /Old chat/ })).toHaveCount(0);
});
