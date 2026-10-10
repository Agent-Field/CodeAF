import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { expectNoHorizontalOverflow, message } from './support/conversation';
import { pendingQuestion } from './support/scenarios';
import { storageKeyFor } from '../../src/features/tabs/scroll/scrollStorage';

// CV-241, CV-244, CV-246, CV-286: the dock's reserved space and gradient, the Latest pill's entrance, scroll restore across a
// reload, and the narrow widths. The engine is a mock; the scroller, dock and pill are the real components.
const paragraphs = Array.from({ length: 90 }, (_, i) => `Paragraph ${i + 1} of a long reply that fills the reading column.`).join('\n\n');
const long: Scenario = {
  initial: { title: 'Long', running: false, entries: [{ Role: 'user', Text: 'Write a lot' }, { Role: 'assistant', Text: paragraphs, Answer: true }] },
  turns: [],
};
const scroller = (page: Page) => page.locator('.conversation-scroll');
const gap = (page: Page) => scroller(page).evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop);
const ready = (page: Page) => expect(page.getByText('Paragraph 90 of a long reply', { exact: false })).toBeVisible();
// Read the durable spot, so a reload tests restoration rather than racing the save throttle or a busy frame.
const savedSpot = (page: Page) => page.evaluate(({ namespace, base }) => {
  const token = sessionStorage.getItem(namespace);
  const raw = localStorage.getItem(`${base}${token}`);
  return raw ? JSON.parse(raw).panes?.a?.['k:conversation#0'] ?? null : null;
}, { namespace: 'codeaf.desktop.windowToken', base: storageKeyFor('browser.') });
const open = async (page: Page, scenario: Scenario = long) => {
  const engine = await installMockEngine(page, scenario);
  // A saved conversation, so the long reply is there on open and again after a reload (the seed is written once per tab session).
  await page.addInitScript(() => {
    if (sessionStorage.getItem('seeded')) return;
    const tab = { id: 'a', title: 'Long', draft: '', kind: 'conversation', titleSource: 'manual', pinned: false, sessionFile: 'chat-a.jsonl' };
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [tab], groups: [], closed: [], activeId: 'a', nextNumber: 2, recentIds: ['a'] }));
    sessionStorage.setItem('seeded', '1');
  });
  await page.goto('/');
  await expect(message(page)).toBeVisible();
  return engine;
};

for (const scheme of ['light', 'dark'] as const) {
  test.describe(scheme, () => {
    test.use({ colorScheme: scheme });

    test('CV-241: scroll padding is dock height + 24 and the dock fades to the canvas by gradient', async ({ page }) => {
      await open(page);
      await ready(page);
      const read = () => page.evaluate(() => {
        const list = document.querySelector('.conversation-scroll')!;
        const layer = document.querySelector('.conversation-dock-layer') as HTMLElement;
        const fade = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--scroll-fade-dock'));
        const clearance = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--scroll-dock-clearance'));
        const style = getComputedStyle(layer);
        return { padding: parseFloat(getComputedStyle(list).paddingBottom), layer: layer.getBoundingClientRect().height, fade, clearance, image: style.backgroundImage, opacity: style.opacity, canvas: getComputedStyle(document.body).getPropertyValue('--canvas').trim() };
      });
      const m = await read();
      expect(m.clearance).toBe(24);
      // The dock layer's height includes its fade strip, which is the strip that overlaps the list: padding = fade + 24 on top of the dock.
      expect(m.padding).toBe(m.fade + m.clearance);
      expect(m.image).toContain('linear-gradient');
      expect(m.opacity).toBe('1');
      // The last line of the transcript rests clear of the dock's own top edge.
      const last = await page.getByText('Paragraph 90 of a long reply', { exact: false }).evaluate(el => el.getBoundingClientRect().bottom);
      const dockTop = await page.locator('.composer-dock').evaluate(el => el.getBoundingClientRect().top);
      expect(dockTop - last).toBeGreaterThanOrEqual(m.clearance - 1);
    });

    test('CV-244: the Latest pill enters with a fade and a 4px rise', async ({ page }) => {
      const engine = await open(page);
      await ready(page);
      // Capture the actual entrance at insertion; a browser round trip can outlast the 200ms animation.
      await page.evaluate(() => {
        const observer = new MutationObserver(() => {
          const pill = document.querySelector<HTMLElement>('.latest-pill');
          if (!pill) return;
          const enter = pill.getAnimations().find(a => (a as CSSAnimation).animationName === 'latest-pill-enter');
          pill.dataset.enterFrames = JSON.stringify(enter ? (enter.effect as KeyframeEffect).getKeyframes().map(k => ({ opacity: String(k.opacity), transform: String(k.transform) })) : []);
          observer.disconnect();
        });
        observer.observe(document.querySelector('.conversation-main')!, { childList: true, subtree: true });
      });
      // A pill needs something live while the reader is away from the end.
      engine.update({ running: true });
      await scroller(page).evaluate(el => { el.scrollTop = 0; });
      const pill = page.getByRole('button', { name: /^Latest/ });
      await expect(pill).toBeVisible();
      const frames = await pill.evaluate(el => JSON.parse((el as HTMLElement).dataset.enterFrames ?? '[]') as { opacity: string; transform: string }[]);
      expect(frames).toHaveLength(2);
      expect(frames[0].opacity).toBe('0');
      // Computed keyframes spell the 4px rise as a function or a matrix, depending on the engine.
      expect(frames[0].transform).toMatch(/translateY\(4px\)|translate\(0px, 4px\)|matrix\(1, 0, 0, 1, 0, 4\)/);
    });

    test('CV-246: an unanchored reader returns to the same place after a reload; an anchored one reopens at the bottom', async ({ page }) => {
      // This case performs three page loads; shared-runner startup must not consume the final restore assertion's budget.
      test.setTimeout(60_000);
      await open(page);
      await ready(page);
      await scroller(page).evaluate(el => { el.scrollTop = 1200; });
      await expect.poll(() => savedSpot(page)).toMatchObject({ top: 1200, end: false });
      await page.reload();
      await expect(message(page)).toBeVisible();
      await expect.poll(async () => Math.abs((await scroller(page).evaluate(el => el.scrollTop)) - 1200), { timeout: 8000 }).toBeLessThanOrEqual(3);
      expect(await gap(page)).toBeGreaterThan(48);

      // A real wheel ends restoration immediately and returns the reader to the end.
      const box = (await scroller(page).boundingBox())!;
      await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
      await page.mouse.wheel(0, 20000);
      await expect.poll(() => gap(page)).toBeLessThanOrEqual(2);
      await expect.poll(() => savedSpot(page)).toMatchObject({ end: true });
      await page.reload();
      await expect(message(page)).toBeVisible();
      await ready(page);
      await expect.poll(() => gap(page), { timeout: 8000 }).toBeLessThanOrEqual(48);
    });

    for (const width of [320, 600, 850]) {
      test(`CV-286: ${width}px has no horizontal overflow in the transcript, dock or tray`, async ({ page }) => {
        await page.setViewportSize({ width, height: 800 });
        await open(page, { ...pendingQuestion('Set up storage'), initial: { ...pendingQuestion('Set up storage').initial, entries: long.initial.entries } });
        await expect(message(page)).toBeVisible();
        await expectNoHorizontalOverflow(page);
        const wide = await page.evaluate(() => ['.conversation-scroll', '.composer-dock', '.conversation-dock-layer'].filter(sel => { const el = document.querySelector(sel); return !!el && el.scrollWidth > el.clientWidth + 1; }));
        expect(wide).toEqual([]);
        const spill = await page.evaluate(() => [...document.querySelectorAll('.composer-dock, .conversation-dock-layer, .conversation-scroll')].filter(el => { const r = el.getBoundingClientRect(); return r.right > innerWidth + 1 || r.left < -1; }).length);
        expect(spill).toBe(0);
        // Contrast has its own spec (conversation-contrast); here axe covers structure at each width.
        const result = await new AxeBuilder({ page }).include('.conversation-main').withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).disableRules(['color-contrast']).analyze();
        expect(result.violations).toEqual([]);
      });
    }
  });
}

test.describe('reduced motion', () => {
  test.use({ reducedMotion: 'reduce' });
  test('CV-244: the pill has no entrance animation', async ({ page }) => {
    await open(page);
    const none = await page.evaluate(() => {
      const probe = document.createElement('button');
      probe.className = 'latest-pill button';
      document.body.appendChild(probe);
      const name = getComputedStyle(probe).animationName;
      probe.remove();
      return name;
    });
    expect(none).toBe('none');
  });
});
