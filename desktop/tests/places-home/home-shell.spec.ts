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

test('d5-pl-test-home: instructions card matches the notes specimen in both appearances', async ({ page }) => {
  for (const theme of ['light', 'dark']) {
    await page.setViewportSize({ width: 1200, height: 900 });
    await open(page, theme, 'place');
    await expect(page.locator('.knows-add')).toBeVisible();
    const card = page.getByRole('region', { name: 'Instructions' });
    await expect(card).toBeVisible();
    const note = card.getByRole('textbox', { name: 'Add a note, or drop a file or link…' });
    const measured = await card.evaluate(el => {
      const style = getComputedStyle(el);
      const prose = el.querySelector('.home-instructions-prose')!;
      const proseStyle = getComputedStyle(prose);
      const input = el.querySelector('.home-instructions-note') as HTMLInputElement;
      const label = document.querySelector('.type-section-label');
      const field = document.querySelector('.knows-add');
      return {
        content: el.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight),
        width: el.getBoundingClientRect().width,
        padBlock: style.paddingTop, padInline: style.paddingLeft, radius: style.borderRadius, gap: style.gap,
        size: proseStyle.fontSize, weight: proseStyle.fontWeight, leading: proseStyle.lineHeight,
        prose: proseStyle.color, heading: getComputedStyle(document.querySelector('h1')!).color,
        placeholder: getComputedStyle(input, '::placeholder').color,
        label: label ? getComputedStyle(label).color : '',
        card: style.backgroundColor, field: field ? getComputedStyle(field).backgroundColor : '',
      };
    });
    expect(measured.content).toBeCloseTo(520);
    expect(measured.width).toBeCloseTo(552);
    expect(measured.padBlock).toBe('14px');
    expect(measured.padInline).toBe('16px');
    expect(measured.radius).toBe('12px');
    expect(measured.gap).toBe('6px');
    expect(measured.size).toBe('13px');
    expect(measured.weight).toBe('400');
    expect(parseFloat(measured.leading)).toBeCloseTo(22.1, 0);
    expect(measured.prose).toBe(measured.heading);
    expect(measured.placeholder).toBe(measured.label);
    expect(measured.card).toBe(measured.field);
  }
});

test('d5-pl-test-home: write, escape, blur, a note and a dropped link', async ({ page }) => {
  await open(page, 'light', 'empty');
  await expect(page.getByRole('region', { name: 'Instructions' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Write instructions' }).click();
  const editor = page.getByRole('textbox', { name: 'Instructions' });
  await expect(editor).toBeFocused();
  await editor.fill('Use the brand voice.');
  await page.keyboard.press('Escape');
  await expect(page.getByRole('region', { name: 'Instructions' })).toHaveCount(0);
  await expect(page.getByRole('list', { name: 'Callback log' }).locator('li')).toHaveCount(0);

  await page.getByRole('button', { name: 'Write instructions' }).click();
  await editor.fill('Use the brand voice.');
  await editor.blur();
  const log = page.getByRole('list', { name: 'Callback log' }).locator('li');
  await expect(log.last()).toHaveText('saveInstructions:pl_launch:Use the brand voice.');
  const note = page.getByRole('textbox', { name: 'Add a note, or drop a file or link…' });
  await note.fill('Ship on Tuesday');
  await note.press('Enter');
  await expect(log.last()).toHaveText('saveInstructions:pl_launch:Use the brand voice.\n\nShip on Tuesday');

  await open(page, 'light', 'place');
  const card = page.getByRole('region', { name: 'Instructions' });
  await card.evaluate(el => {
    const transfer = new DataTransfer();
    transfer.setData('text/uri-list', 'https://example.com/brand');
    el.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: transfer }));
  });
  await expect(log.last()).toHaveText('dropOnInstructions:pl_codeaf:url:https://example.com/brand');

  await card.getByRole('button', { name: 'Lexer owns tolerance. Parser stays strict.' }).click();
  const placeEditor = page.getByRole('textbox', { name: 'Instructions' });
  await placeEditor.fill('REFUSE this');
  await placeEditor.blur();
  await expect(card.getByRole('alert')).toHaveText('Someone else changed these instructions.');
  await expect(placeEditor).toHaveValue('REFUSE this');
});
