import { test, expect, type Page } from '@playwright/test';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

// SH-168 / SH-169: at the small breakpoint the card stays 8px inside the window and
// is at most min(300px, 100vw - 16px). Pins keep their preview at narrow widths;
// ordinary inactive tabs compress to a title tooltip. A screen that cannot hover never opens a card.
const delay = design.interaction.previewOpenDelay;
const pad = Number.parseInt(design.foundation['preview-collision-padding'], 10);
const column = Number.parseInt(design.foundation['preview-card-width'], 10);

const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, titleSource: 'manual', kind: 'conversation', draft: 'A saved thought', pinned: false, ...over });

async function seed(page: Page, pinned = true) {
  const tabs = [tab('a', 'Active tab'), tab('b', 'Port fix', { pinned }), tab('c', 'Plain tab')];
  const state = { tabs, groups: [], closed: [], activeId: 'a', nextNumber: tabs.length + 1, recentIds: tabs.map(item => item.id) };
  await page.addInitScript(value => { localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}

const card = (page: Page, title: string) => page.getByRole('group', { name: `Preview of ${title}`, exact: true });

async function openWorkspace(page: Page, pinned = true) {
  const base = plainReply();
  await installMockEngine(page, base);
  await seed(page, pinned);
  await page.goto('/');
  await expect(page.getByRole('tab', { name: 'Port fix', exact: true })).toBeVisible();
}

test('at 600px and below the card stays inside the viewport by the collision padding', async ({ page }) => {
  expect(pad).toBe(8);
  expect(column).toBe(300);
  await openWorkspace(page);
  for (const width of [600, 360, 320]) {
    await page.setViewportSize({ width, height: 700 });
    const port = page.getByRole('tab', { name: 'Port fix', exact: true });
    await port.hover();
    const preview = card(page, 'Port fix');
    await expect(preview).toBeVisible();
    const box = await preview.boundingBox();
    expect(box, `card box at ${width}`).toBeTruthy();
    const cap = Math.min(column, width - pad * 2);
    expect(box!.width).toBeLessThanOrEqual(cap + 1);
    expect(box!.x).toBeGreaterThanOrEqual(pad - 1);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width - pad + 1);
    const tabBox = await port.boundingBox();
    if (tabBox && tabBox.x + box!.width > width - pad) {
      expect(Math.abs(width - (box!.x + box!.width) - pad)).toBeLessThan(1.5);
    }
    const maxWidth = await preview.locator('.preview-card').evaluate(el => getComputedStyle(el).maxWidth);
    expect(Number.parseFloat(maxWidth)).toBeLessThanOrEqual(cap + 1);
    await page.mouse.move(0, 0);
    await expect(preview).toHaveCount(0);
  }
});

test('a hover-none screen never opens a preview card', async ({ page }) => {
  await page.addInitScript(() => {
    const native = window.matchMedia.bind(window);
    window.matchMedia = (query: string) => {
      if (query !== '(hover: none)') return native(query);
      return {
        matches: true,
        media: query,
        onchange: null,
        addListener() {},
        removeListener() {},
        addEventListener() {},
        removeEventListener() {},
        dispatchEvent() { return false; },
      } as MediaQueryList;
    };
  });
  await openWorkspace(page);
  const port = page.getByRole('tab', { name: 'Port fix', exact: true });
  await port.hover();
  await page.waitForTimeout(delay * 1.5);
  await expect(card(page, 'Port fix')).toHaveCount(0);
  await port.focus();
  await page.waitForTimeout(delay * 0.4);
  await expect(card(page, 'Port fix')).toHaveCount(0);
});

// Shell 3j compresses ordinary inactive tabs; their full title replaces the card.
test('a compressed inactive tab shows its title tooltip and keeps keyboard navigation', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 700 });
  await openWorkspace(page, false);
  const port = page.getByRole('tab', { name: 'Port fix', exact: true });
  await expect(port.locator('..')).toHaveAttribute('data-compressed', 'true');
  await port.hover();
  await expect(page.getByRole('tooltip', { name: 'Port fix', exact: true })).toBeVisible();
  await expect(card(page, 'Port fix')).toHaveCount(0);
  await page.keyboard.press('Escape');
  await page.mouse.move(0, 0);
  await page.getByRole('tab', { name: 'Active tab', exact: true }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(port).toBeFocused();
  await expect(port).toHaveAttribute('aria-selected', 'true');
  await expect(card(page, 'Port fix')).toHaveCount(0);
});
