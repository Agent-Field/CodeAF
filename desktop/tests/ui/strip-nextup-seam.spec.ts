import { test, expect, type Page } from '@playwright/test';
import type { AttentionItem } from '../../src/features/chat/world-client';
import { installMockEngine, type MockEngine } from './support/mock-engine';

// The strip seam: the pill is the last control, outside the tabs and outside the drag spacer;
// the banner and the hover list hang under it; the back chip sits before Home.
// Focus hides the strip, so the pill returns with the strip. A collapsed rail leaves both up.
const CHAT = '9446cc2627f3deae';
const OTHER = 'ab12cd34ef56ab78';
const FRESH = '0011223344556677';
const SESSION = `/mock/places/${CHAT}/transcript.jsonl`;

function question(key: string, session: string, text: string, extra: Partial<AttentionItem> = {}): AttentionItem {
  return { key, session, kind: 'consent', text, sourceFolders: [], answerable: true, ...extra };
}

const waiting = question(`${OTHER}:consent:1`, OTHER, 'Allow 3 git actions?', { title: 'Release notes' });
const here = question(`${CHAT}:consent:1`, CHAT, 'Stay on this one?', { title: 'Config parser' });
const arrived = question(`${FRESH}:consent:1`, FRESH, 'Ship the notes?', { title: 'Config parser', holdingUp: ['lint', 'types'] });

async function open(page: Page): Promise<MockEngine> {
  const engine = await installMockEngine(page, {
    world: { rows: [], items: [here, waiting] },
    initial: { sessionFile: SESSION, title: 'Config parser', entries: [], needsPerson: false, running: false },
  });
  const tabs = {
    tabs: [
      { id: 'home', title: 'Home', draft: '', titleSource: 'manual', kind: 'conversation', pinned: true },
      { id: 'chat', title: 'Config parser', draft: '', titleSource: 'manual', kind: 'conversation', pinned: false, sessionFile: SESSION },
    ],
    groups: [], closed: [], activeId: 'chat', nextNumber: 3, recentIds: ['chat', 'home'],
  };
  const focus = {
    entries: [
      { windowPlace: 'now', tabId: 'home', drillPath: [], scroll: [], draftKey: null, selfStarted: true },
      { windowPlace: 'now', tabId: 'chat', drillPath: [], scroll: [], draftKey: null, selfStarted: false },
    ],
    cursor: 1,
  };
  await page.addInitScript(([workspace, history]) => {
    if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', workspace);
    localStorage.setItem('codeaf.desktop.focus.v1.main', history);
  }, [JSON.stringify(tabs), JSON.stringify(focus)] as const);
  await page.goto('/');
  return engine;
}

function right(box: { x: number; width: number }) {
  return box.x + box.width;
}

test('the pill, banner and back chip sit on the strip and survive the rail and Focus', async ({ page, browserName }) => {
  const engine = await open(page);
  const pill = page.getByRole('button', { name: /\d+ need you elsewhere/ });
  const chip = page.getByRole('button', { name: 'Back to Home' });
  const home = page.getByRole('tab', { name: 'Home', exact: true });
  await expect(pill).toBeVisible();
  await expect(pill).toHaveAccessibleName(/1 need you elsewhere/);
  await expect(chip).toBeVisible();

  const outside = await pill.evaluate(el => !!el.closest('.workspace-tabstrip, .workspace-tab-spacer'));
  expect(outside).toBe(false);
  const pillBox = (await pill.boundingBox())!;
  const barBox = (await page.locator('.workspace-tabbar').boundingBox())!;
  const homeBox = (await home.boundingBox())!;
  const chipBox = (await chip.boundingBox())!;
  const tabsBox = (await page.locator('.workspace-tabstrip').boundingBox())!;
  const allTabs = (await page.getByRole('button', { name: 'All tabs', exact: true }).boundingBox())!;
  expect(Math.abs(right(pillBox) - right(barBox))).toBeLessThanOrEqual(2);
  expect(pillBox.x).toBeGreaterThan(right(allTabs) - 1);
  expect(pillBox.x).toBeGreaterThan(right(tabsBox) - 1);
  expect(right(chipBox)).toBeLessThanOrEqual(homeBox.x + 1);

  const spacer = page.locator('.workspace-tab-spacer');
  await expect(spacer).toHaveAttribute('data-tauri-drag-region', 'true');
  expect((await spacer.boundingBox())!.width).toBeGreaterThan(0);
  const spacerCovered = await spacer.evaluate(el => {
    for (let node: Element | null = el; node; node = node.parentElement) {
      if (getComputedStyle(node).getPropertyValue('-webkit-app-region') === 'no-drag') return true;
    }
    return false;
  });
  expect(spacerCovered).toBe(false);
  if (browserName === 'chromium') {
    const region = await pill.evaluate(el => getComputedStyle(el).getPropertyValue('-webkit-app-region'));
    expect(region).toBe('no-drag');
  }

  await pill.hover();
  const popover = page.getByRole('group', { name: /need you/ });
  await expect(popover).toBeVisible();
  const popBox = (await popover.boundingBox())!;
  expect(popBox.y).toBeGreaterThanOrEqual(pillBox.y + pillBox.height - 1);
  expect(Math.abs(right(popBox) - right(pillBox))).toBeLessThanOrEqual(2);

  await engine.setWorld({ items: [here, waiting, arrived] });
  const banner = page.locator('.nextup-banner[data-phase="rest"]');
  await expect(banner).toBeVisible();
  await expect(banner).toContainText('Ship the notes?');
  await expect(banner).toContainText('holding up 2 tasks');
  await expect(pill).toHaveAccessibleName(/2 need you elsewhere/);
  const bannerBox = (await banner.boundingBox())!;
  const pillNow = (await pill.boundingBox())!;
  expect(bannerBox.y).toBeGreaterThanOrEqual(pillNow.y + pillNow.height - 1);
  expect(Math.abs(right(bannerBox) - right(pillNow))).toBeLessThanOrEqual(2);

  await page.keyboard.press('Control+s');
  await expect(page.locator('.app-shell')).toHaveClass(/sidebar-collapsed/);
  await expect(pill).toBeVisible();
  const collapsed = (await pill.boundingBox())!;
  const barCollapsed = (await page.locator('.workspace-tabbar').boundingBox())!;
  expect(Math.abs(right(collapsed) - right(barCollapsed))).toBeLessThanOrEqual(2);

  await page.keyboard.press('Control+Shift+F');
  await expect(page.locator('.app-shell')).toHaveAttribute('data-focus', 'true');
  await expect(pill).toBeHidden();
  await page.locator('.shell-hotzone[data-edge="top"]').hover();
  await expect(page.locator('.app-shell')).toHaveAttribute('data-peek', 'strip');
  await expect(pill).toBeVisible();

  await page.evaluate(() => {
    (window as unknown as { __next: unknown[] }).__next = [];
    window.addEventListener('codeaf:next-up-start', event => {
      (window as unknown as { __next: unknown[] }).__next.push((event as CustomEvent).detail);
    });
  });
  await pill.click();
  await expect.poll(() => page.evaluate(() => (window as unknown as { __next: unknown[] }).__next)).toEqual([{}]);
});
