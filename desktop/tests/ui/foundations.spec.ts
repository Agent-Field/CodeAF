import { readFileSync } from 'node:fs';
import { test, expect, type Locator, type Page } from '@playwright/test';
import { tokenColor } from './contracts';

// Playwright's loader rejects a bare JSON import. The file is still the only source of expected values.
const design = JSON.parse(readFileSync(new URL('../../src/design/tokens.json', import.meta.url), 'utf8')) as {
  foundation: Record<string, string>;
  themes: { light: Record<string, string>; dark: Record<string, string> };
  tints: { hues: Record<string, { h: string; a: string; swatch: string }> };
};
import { installMockEngine } from './support/mock-engine';
import { openPage } from './support/shell-navigation';

const foundation = design.foundation;
const themes = design.themes;
type ThemeName = keyof typeof themes;

// Foundations §03, including the roles the ramp draws and the ones that only exist as variables (tab, scrim, frame-glass).
const colourRoles = [
  'canvas', 'frame', 'surface', 'field', 'field-2', 'bubble', 'term', 'line', 'guide',
  'ink', 'ink-2', 'ink-3', 'accent', 'accent-ink', 'accent-soft', 'accent-rule',
  'amber', 'amber-soft', 'danger', 'danger-soft', 'success', 'diff-add',
  'tab', 'tab-hover', 'scrim', 'frame-glass',
  'site-icon-hue-1', 'site-icon-hue-2', 'site-icon-hue-3', 'site-icon-hue-4', 'site-icon-hue-5', 'site-icon-hue-6',
] as const;

// Foundations §04. Mono small is the specimen's tabular row; its size token is the same 11px as type-mono-small-size.
const typeScale: { role: string; size: keyof typeof foundation; leading: keyof typeof foundation; weight: keyof typeof foundation; tracking?: keyof typeof foundation; mono?: boolean; tabular?: boolean }[] = [
  { role: 'title', size: 'type-title-size', leading: 'type-title-leading', weight: 'type-title-weight', tracking: 'type-title-tracking' },
  { role: 'h1', size: 'type-h1-size', leading: 'type-h1-leading', weight: 'type-h1-weight', tracking: 'type-h1-tracking' },
  { role: 'h2', size: 'type-h2-size', leading: 'type-h2-leading', weight: 'type-h2-weight' },
  { role: 'h3', size: 'type-h3-size', leading: 'type-h3-leading', weight: 'type-h3-weight' },
  { role: 'prose', size: 'type-prose-size', leading: 'type-prose-leading', weight: 'type-prose-weight' },
  { role: 'label', size: 'type-label-size', leading: 'type-label-leading', weight: 'type-label-weight' },
  { role: 'caption', size: 'type-caption-size', leading: 'type-caption-leading', weight: 'type-caption-weight' },
  { role: 'mono', size: 'type-mono-size', leading: 'type-mono-leading', weight: 'type-mono-weight', mono: true },
  { role: 'mono-small', size: 'foundations-type-mono-small-size', leading: 'foundations-type-mono-small-leading', weight: 'foundations-type-mono-small-weight', mono: true, tabular: true },
];

const spaces = ['space-1', 'space-2', 'space-3', 'space-4', 'space-5', 'space-6', 'space-8', 'space-12'] as const;
const rhythms = ['space-turn', 'space-answer', 'space-work-row', 'space-work-block', 'work-indent', 'chat-column-max-width', 'row-h', 'hit', 'panel'] as const;
const radii = ['chip', 'control', 'card', 'bubble', 'dock'] as const;

// Foundations §07. The first column is the design glyph; data-icon is the registry name whose Lucide component is that glyph.
const stepGlyphs: { category: string; glyph: string; icon: string }[] = [
  { category: 'search', glyph: 'search', icon: 'search' },
  { category: 'read', glyph: 'book-open', icon: 'book' },
  { category: 'edit', glyph: 'pencil', icon: 'pencil' },
  { category: 'create', glyph: 'file-plus', icon: 'create' },
  { category: 'run', glyph: 'terminal', icon: 'terminal' },
  { category: 'test', glyph: 'flask-conical', icon: 'test' },
  { category: 'browse', glyph: 'globe', icon: 'browse' },
  { category: 'transfer', glyph: 'arrow-left-right', icon: 'transfer' },
  { category: 'communicate', glyph: 'message-square', icon: 'communicate' },
  { category: 'coordinate', glyph: 'network', icon: 'coordinate' },
  { category: 'plan', glyph: 'list-checks', icon: 'plan' },
  { category: 'wait', glyph: 'hourglass', icon: 'wait' },
  { category: 'work', glyph: 'wrench', icon: 'tool' },
];

const iconSizes = ['icon-micro', 'icon-tiny', 'file-chip-icon', 'icon-xs', 'icon-sm', 'composer-plus-size', 'icon-md'] as const;
const motionDurations = ['dur-fast', 'dur-base', 'dur-slow', 'dur-spring', 'dur-press'] as const;
const motionEases = ['ease', 'spring'] as const;
const motionRises = ['motion-rise-row', 'motion-rise-send', 'motion-rise-toast'] as const;
const motionScales = ['motion-scale-popover', 'motion-scale-quicklook'] as const;

const markGlyphs: Record<string, string | undefined> = {
  running: undefined, queued: undefined, done: 'check', waiting: undefined,
  failed: 'triangleAlert', stopped: 'ban', paused: 'pause', incomplete: 'queued',
};

async function usedColor(scope: Locator, css: string) {
  return scope.evaluate((el, value) => {
    const probe = document.createElement('span');
    probe.style.color = value;
    el.append(probe);
    const color = getComputedStyle(probe).color;
    probe.remove();
    return color;
  }, css);
}

async function usedLength(page: Page, css: string) {
  return page.evaluate(value => {
    const probe = document.createElement('span');
    probe.style.width = value;
    document.body.append(probe);
    const width = getComputedStyle(probe).width;
    probe.remove();
    return width;
  }, css);
}

async function usedEase(page: Page, css: string) {
  return page.evaluate(value => {
    const probe = document.createElement('span');
    probe.style.transitionTimingFunction = value;
    document.body.append(probe);
    const ease = getComputedStyle(probe).transitionTimingFunction;
    probe.remove();
    return ease;
  }, css);
}

async function usedDuration(page: Page, css: string) {
  return page.evaluate(value => {
    const probe = document.createElement('span');
    probe.style.transitionDuration = value;
    document.body.append(probe);
    const duration = getComputedStyle(probe).transitionDuration;
    probe.remove();
    return duration;
  }, css);
}

async function usedShadow(page: Page, css: string) {
  return page.evaluate(value => {
    const probe = document.createElement('div');
    probe.style.boxShadow = value;
    document.body.append(probe);
    const shadow = getComputedStyle(probe).boxShadow;
    probe.remove();
    return shadow;
  }, css);
}

async function openFoundations(page: Page, theme: ThemeName) {
  // Reduced motion collapses durations and rises to zero. Measure the token values with motion allowed.
  await page.emulateMedia({ colorScheme: theme, reducedMotion: 'no-preference' });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, { initial: { entries: [], title: '' } });
  await page.goto('/');
  await openPage(page, 'Design system');
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
  const specimen = page.locator('.foundations-specimen');
  await expect(specimen.getByRole('heading', { name: 'Foundations', exact: true })).toBeVisible();
  return specimen;
}

/**
 * The specimen's solid-frame rule is one query: reduced transparency or more contrast.
 * Chromium can emulate reduced transparency directly. WebKit's automation channel only
 * exposes contrast, which is the other half of that same rule.
 */
async function emulateSolidFrame(page: Page, theme: ThemeName) {
  if (test.info().project.name === 'chromium') {
    const client = await page.context().newCDPSession(page);
    await client.send('Emulation.setEmulatedMedia', {
      features: [
        { name: 'prefers-reduced-transparency', value: 'reduce' },
        { name: 'prefers-color-scheme', value: theme },
        { name: 'prefers-reduced-motion', value: 'no-preference' },
        { name: 'prefers-contrast', value: 'no-preference' },
        { name: 'forced-colors', value: 'none' },
      ],
    });
    await expect.poll(() => page.evaluate(() => matchMedia('(prefers-reduced-transparency: reduce)').matches)).toBe(true);
    return;
  }
  await page.emulateMedia({ contrast: 'more', colorScheme: theme, reducedMotion: 'no-preference' });
  await expect.poll(() => page.evaluate(() => matchMedia('(prefers-contrast: more)').matches)).toBe(true);
}

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: foundations colour, tints, type, space, marks, icons, motion and materials`, async ({ page }) => {
    test.setTimeout(90_000);
    const specimen = await openFoundations(page, theme);
    const palette = themes[theme] as Record<string, string>;

    for (const role of colourRoles) {
      const painted = await usedColor(page.locator('body'), palette[role]);
      const live = await tokenColor(page, role);
      expect(live, `${theme} ${role}`).toBe(painted);
      const swatch = specimen.locator(`.fd-ramp-pair [data-role="${role}"][data-theme="${theme}"]`);
      if (await swatch.count()) {
        expect(await swatch.evaluate(el => getComputedStyle(el).backgroundColor), `${theme} swatch ${role}`).toBe(live);
      }
    }

    const other = theme === 'light' ? 'dark' : 'light';
    const otherPalette = themes[other] as Record<string, string>;
    for (const role of ['canvas', 'frame', 'surface', 'field', 'field-2', 'bubble', 'term', 'line', 'guide', 'ink', 'ink-2', 'ink-3', 'accent', 'accent-soft', 'accent-rule', 'amber', 'danger', 'success'] as const) {
      const swatch = specimen.locator(`.fd-ramp-pair [data-role="${role}"][data-theme="${other}"]`);
      const painted = await usedColor(swatch, otherPalette[role]);
      expect(await swatch.evaluate(el => getComputedStyle(el).backgroundColor), `${other} swatch ${role}`).toBe(painted);
    }

    for (const [name, hue] of Object.entries(design.tints.hues)) {
      const card = specimen.locator(`.fd-tint[data-tint="${name}"][data-theme="${theme}"]`);
      const custom = await card.evaluate(el => {
        const style = getComputedStyle(el);
        const probe = document.createElement('span');
        probe.style.color = 'var(--swatch)';
        el.append(probe);
        const swatch = getComputedStyle(probe).color;
        probe.remove();
        return { h: style.getPropertyValue('--h').trim(), a: style.getPropertyValue('--a').trim(), swatch };
      });
      expect(Number(custom.h), name).toBe(Number(hue.h));
      expect(Number(custom.a), name).toBeCloseTo(Number(hue.a), 5);
      expect(custom.swatch, name).toBe(await usedColor(page.locator('body'), palette[`places-swatch-${name}`]));
      const accent = await usedColor(card, palette.accent);
      const canvas = await usedColor(card, palette.canvas);
      expect(await usedColor(card, 'var(--accent)'), `${name} accent`).toBe(accent);
      expect(await usedColor(card, 'var(--canvas)'), `${name} canvas`).toBe(canvas);
    }
    const sandAccent = await usedColor(specimen.locator(`.fd-tint[data-tint="sand"][data-theme="${theme}"]`), 'var(--accent)');
    const tideAccent = await usedColor(specimen.locator(`.fd-tint[data-tint="tide"][data-theme="${theme}"]`), 'var(--accent)');
    const sandCanvas = await usedColor(specimen.locator(`.fd-tint[data-tint="sand"][data-theme="${theme}"]`), 'var(--canvas)');
    const tideCanvas = await usedColor(specimen.locator(`.fd-tint[data-tint="tide"][data-theme="${theme}"]`), 'var(--canvas)');
    expect(sandAccent).not.toBe(tideAccent);
    expect(sandCanvas).not.toBe(tideCanvas);

    for (const row of typeScale) {
      const sample = specimen.locator(`[data-type="${row.role}"]`);
      const expected = await sample.evaluate((el, spec) => {
        const probe = document.createElement('span');
        probe.style.fontSize = spec.size;
        probe.style.lineHeight = spec.leading;
        probe.style.fontWeight = spec.weight;
        if (spec.tracking) probe.style.letterSpacing = spec.tracking;
        if (spec.mono) probe.style.fontFamily = spec.mono;
        el.append(probe);
        const style = getComputedStyle(probe);
        const value = { size: style.fontSize, leading: style.lineHeight, weight: style.fontWeight, tracking: style.letterSpacing, family: style.fontFamily };
        probe.remove();
        return value;
      }, { size: foundation[row.size], leading: foundation[row.leading], weight: foundation[row.weight], tracking: row.tracking ? foundation[row.tracking] : '', mono: row.mono ? foundation.mono : '' });
      const actual = await sample.evaluate(el => {
        const style = getComputedStyle(el);
        return { size: style.fontSize, leading: style.lineHeight, weight: style.fontWeight, tracking: style.letterSpacing, family: style.fontFamily, variant: style.fontVariantNumeric };
      });
      expect(actual.size, row.role).toBe(expected.size);
      expect(actual.leading, row.role).toBe(expected.leading);
      expect(actual.weight, row.role).toBe(expected.weight);
      if (row.tracking) expect(actual.tracking, row.role).toBe(expected.tracking);
      if (row.mono) expect(actual.family, row.role).toBe(expected.family);
      if (row.tabular) expect(actual.variant, row.role).toContain('tabular-nums');
    }
    const labelName = specimen.locator('.fd-type-row:has([data-type="label"]) strong');
    expect(await labelName.evaluate(el => getComputedStyle(el).fontWeight)).toBe(await page.evaluate(weight => {
      const probe = document.createElement('span');
      probe.style.fontWeight = weight;
      document.body.append(probe);
      const value = getComputedStyle(probe).fontWeight;
      probe.remove();
      return value;
    }, foundation['type-label-strong']));

    for (const key of spaces) {
      const bar = specimen.locator(`[data-space="${key}"]`);
      expect(await bar.evaluate(el => getComputedStyle(el).width), key).toBe(await usedLength(page, foundation[key]));
    }
    for (const key of rhythms) {
      expect(await usedLength(page, `var(--${key})`), key).toBe(await usedLength(page, foundation[key]));
    }
    for (const key of radii) {
      const chip = specimen.locator(`[data-radius="${key}"]`);
      const token = foundation[`radius-${key}` as keyof typeof foundation];
      expect(await chip.evaluate(el => getComputedStyle(el).borderTopLeftRadius), key).toBe(await usedLength(page, token));
    }
    for (const level of ['1', '2', '3'] as const) {
      const plate = specimen.locator(`[data-depth="${level}"]`);
      const token = palette[`sh-${level}`];
      const drawn = await plate.evaluate(el => getComputedStyle(el).boxShadow);
      expect(drawn, `sh-${level}`).toBe(await usedShadow(page, `var(--sh-${level})`));
      expect(drawn, `sh-${level} token`).toBe(await usedShadow(page, token));
    }

    const ink = await tokenColor(page, 'ink');
    const ink3 = await tokenColor(page, 'ink-3');
    for (const [status, icon] of Object.entries(markGlyphs)) {
      const row = specimen.locator(`.fd-status-grid .fd-mark:has([data-status="${status}"])`);
      const mark = row.locator('.status-mark');
      expect(await mark.evaluate(el => getComputedStyle(el).color), status).toBe(await tokenColor(page, `mark-${status}`));
      expect(await row.locator('span').last().evaluate(el => getComputedStyle(el).color), status).toBe(ink);
      if (icon) await expect(mark.locator('[data-icon]')).toHaveAttribute('data-icon', icon);
      const box = status === 'queued' ? 'mark-ring' : status === 'running' || status === 'waiting' ? 'mark-dot' : 'mark-box';
      const sized = status === 'queued' || status === 'running' || status === 'waiting' ? mark.locator('.status-mark-dot') : mark;
      expect(await sized.evaluate(el => getComputedStyle(el).width), status).toBe(await usedLength(page, foundation[box]));
    }
    expect(ink3).not.toBe(ink);

    for (const step of stepGlyphs) {
      const icon = specimen.locator(`[data-category="${step.category}"] [data-icon]`);
      await expect(icon).toHaveAttribute('data-icon', step.icon);
      const drawn = await icon.locator('svg').evaluate(el => el.innerHTML);
      const reference = await page.locator(`.icon-specimens [data-icon="${step.icon}"] svg`).first().evaluate(el => el.innerHTML);
      expect(drawn, `${step.category} → ${step.glyph}`).toBe(reference);
    }
    const checks = await page.locator('.icon-specimens [data-icon="checklist"] svg').first().evaluate(el => el.innerHTML);
    expect(await specimen.locator('[data-category="plan"] svg').evaluate(el => el.innerHTML)).toBe(checks);

    for (const key of iconSizes) {
      expect(await usedLength(page, `var(--${key})`), key).toBe(await usedLength(page, foundation[key]));
    }
    expect(await specimen.locator('.fd-category-grid .app-icon').first().evaluate(el => getComputedStyle(el).width)).toBe(await usedLength(page, foundation['icon-sm']));
    expect(await specimen.locator('.fd-work .app-icon').first().evaluate(el => getComputedStyle(el).width)).toBe(await usedLength(page, foundation['icon-xs']));
    const stroke = await specimen.locator('.fd-category-grid svg').first().evaluate(el => getComputedStyle(el).strokeWidth);
    expect(Number.parseFloat(stroke)).toBe(Number(foundation['icon-stroke']));

    for (const key of motionDurations) {
      expect(await usedDuration(page, `var(--${key})`), key).toBe(await usedDuration(page, foundation[key]));
    }
    for (const key of motionEases) {
      expect(await usedEase(page, `var(--${key})`), key).toBe(await usedEase(page, foundation[key]));
    }
    for (const key of motionRises) {
      expect(await usedLength(page, `var(--${key})`), key).toBe(await usedLength(page, foundation[key]));
    }
    for (const key of motionScales) {
      const specified = await page.evaluate(name => getComputedStyle(document.documentElement).getPropertyValue(name).trim(), `--${key}`);
      expect(Number(specified), key).toBe(Number(foundation[key]));
    }

    const frame = await tokenColor(page, 'frame');
    const glass = await tokenColor(page, 'frame-glass');
    const solid = specimen.locator('[data-material-option="solid"] .fd-material-rail');
    const tinted = specimen.locator('[data-material-option="tinted"] .fd-material-rail');
    expect(await solid.evaluate(el => getComputedStyle(el).backgroundColor)).toBe(frame);
    expect(await solid.evaluate(el => getComputedStyle(el).backdropFilter)).toBe('none');
    expect(await tinted.evaluate(el => getComputedStyle(el).backgroundColor)).toBe(glass);
    expect(await tinted.evaluate(el => getComputedStyle(el).backdropFilter)).not.toBe('none');
    const railInk = await specimen.locator('.fd-material-rail').evaluateAll(nodes => nodes.flatMap(rail => [...rail.querySelectorAll('strong, span')].map(el => getComputedStyle(el).color)));
    expect(railInk.length).toBeGreaterThan(0);
    for (const color of railInk) expect(color).not.toBe(ink3);

    await page.evaluate(() => {
      document.documentElement.dataset.windowActive = 'false';
      document.documentElement.dataset.material = 'solid';
    });
    const shell = page.locator('.app-shell');
    expect(await shell.evaluate(el => getComputedStyle(el).backgroundColor)).toBe(frame);
    expect(await shell.evaluate(el => getComputedStyle(el).backdropFilter)).toBe('none');

    await emulateSolidFrame(page, theme);
    for (const option of ['solid', 'tinted', 'clear']) {
      const rail = specimen.locator(`[data-material-option="${option}"] .fd-material-rail`);
      expect(await rail.evaluate(el => getComputedStyle(el).backdropFilter), option).toBe('none');
      expect(await rail.evaluate(el => getComputedStyle(el).backgroundColor), option).toBe(frame);
    }
  });
}
