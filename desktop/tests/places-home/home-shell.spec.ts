import { test, expect, type Page } from '@playwright/test';

async function open(page: Page, theme: string, scenario: string) {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.route('**/api/engine/**', route => route.fulfill({ json: route.request().url().includes('/knows')
    ? { revision: 1, lines: [], stillTrue: [] }
    : route.request().url().includes('/decisions') ? { decisions: [] } : { mode: 'none' } }));
  await page.goto(`/?scenario=${scenario}`);
  await page.getByTestId('home-specimen').waitFor();
}

for (const theme of ['light', 'dark']) {
  for (const width of [320, 600, 850, 1200]) {
    test(`d5-pl-test-home: ${theme} ${width}px column, heading and dock`, async ({ page }) => {
      await page.setViewportSize({ width, height: 700 });
      for (const scenario of ['place', 'empty', 'now', 'root', 'loading']) {
        await open(page, theme, scenario);
        const geometry = await page.locator('.home-page').evaluate(el => {
          const column = el.querySelector('.home-column')!;
          const composer = el.querySelector('.home-composer')!;
          const scroll = el.querySelector('.home-scroll')!;
          const c = column.getBoundingClientRect(), d = composer.getBoundingClientRect(), s = scroll.getBoundingClientRect();
          return { pane: el.getBoundingClientRect().width, column: c.width, dock: d.width, left: c.left,
            dockLeft: d.left, pad: getComputedStyle(column).paddingLeft, dockPad: getComputedStyle(composer).paddingLeft,
            mask: getComputedStyle(scroll).maskImage, scrollBottom: s.bottom, dockTop: d.top,
            overflow: el.scrollWidth - el.clientWidth };
        });
        expect(geometry.column).toBeCloseTo(width <= 850 ? geometry.pane : 680);
        expect(geometry.dock).toBeCloseTo(geometry.column);
        expect(geometry.dockLeft).toBeCloseTo(geometry.left);
        expect(geometry.pad).toBe(width <= 850 ? '16px' : '24px');
        expect(geometry.dockPad).toBe(geometry.pad);
        expect(geometry.mask).toContain('40px');
        expect(geometry.dockTop).toBeCloseTo(geometry.scrollBottom);
        expect(geometry.overflow).toBe(0);
        const dock = await page.locator('.home-composer').boundingBox();
        await page.locator('.home-scroll').evaluate(el => { el.scrollTop = el.scrollHeight; });
        expect(await page.locator('.home-composer').boundingBox()).toEqual(dock);
        if (scenario === 'loading') {
          await expect(page.locator('.home-section, .places-heading, [aria-busy="true"]')).toHaveCount(0);
        } else {
          await expect(page.getByRole('heading', { level: 1 })).toHaveCSS('font-size', '28px');
          await expect(page.getByRole('heading', { level: 1 })).toHaveCSS('font-weight', '600');
        }
        if (scenario === 'place' || scenario === 'empty') {
          await expect(page.locator('.places-heading [data-role="title"]')).toHaveCSS('width', '16px');
          await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toBeVisible();
          await page.getByRole('button', { name: 'Place actions', exact: true }).click();
          await expect(page.getByRole('menu')).toBeVisible();
          await page.keyboard.press('Escape');
        }
        if (scenario === 'place') {
          const labels = await page.locator('.type-section-label').evaluateAll(els => els.map(el => getComputedStyle(el).fontSize));
          expect(labels.length).toBeGreaterThan(0);
          expect(labels.every(size => size === '11px')).toBe(true);
        }
      }
    });
  }
}

test('d5-pl-test-home: deferred section reads draw no placeholder sections', async ({ page }) => {
  let release!: () => void;
  const waiting = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/engine/**', async route => {
    await waiting;
    await route.fulfill({ json: route.request().url().includes('/knows')
      ? { revision: 1, lines: [], stillTrue: [] }
      : route.request().url().includes('/decisions') ? { decisions: [] } : { mode: 'none' } });
  });
  try {
    await page.goto('/?scenario=place');
    await expect(page.getByRole('heading', { level: 1, name: 'codeaf' })).toBeVisible();
    await expect(page.locator('.status-line, .decided, .knows-list, [aria-busy="true"]')).toHaveCount(0);
    await expect(page.getByRole('region', { name: 'Chats' })).toBeVisible();
  } finally { release(); }
});
