import { test, expect, type Page } from '@playwright/test';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { installMockEngine } from './support/mock-engine';

// SH-073, Foundations F-MO-14: a closing tab's width goes to 0 over dur-base and the neighbours slide.
// Entry fades and does not scale. Reduced motion removes both. Measured, not eyeballed.
const foundation = design.foundation as Record<string, string>;
const durBase = parseFloat(foundation['dur-base']) / 1000;
const KEY = 'codeaf.desktop.workspace.v1';

const tab = (id: string, title: string) => ({ id, title, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual' });

async function seed(page: Page) {
  const state = {
    tabs: [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')],
    groups: [],
    closed: [],
    activeId: 'a',
    nextNumber: 4,
    recentIds: ['a'],
  };
  await page.addInitScript(([key, json]) => localStorage.setItem(key, json), [KEY, JSON.stringify(state)] as const);
}

async function open(page: Page) {
  await installMockEngine(page, { initial: { title: 'Alpha' } });
  await seed(page);
  await page.setViewportSize({ width: 1200, height: 800 });
  await page.goto('/');
  await expect(page.getByRole('tab', { name: 'Gamma', exact: true })).toBeVisible();
}

function widthTransition(property: string, duration: string) {
  const props = property.split(',').map(part => part.trim());
  return { hasWidth: props.includes('width'), seconds: parseFloat(duration) };
}

test('SH-073 closing a tab collapses its width so neighbours slide, and focus moves at once', async ({ page }) => {
  expect(durBase).toBeCloseTo(0.2, 2);
  await open(page);
  const gamma = page.getByRole('tab', { name: 'Gamma', exact: true });
  const before = (await gamma.boundingBox())!;
  // The slot is gone after dur-base, so the sample has to be taken in the page as the node appears.
  await page.evaluate(() => {
    const bag: { property: string; duration: string; timing: string; width: number; hidden: string | null; inert: boolean; running: boolean }[] = [];
    (window as unknown as { __exit: typeof bag }).__exit = bag;
    const take = () => {
      const el = document.querySelector<HTMLElement>('.workspace-tab-exit');
      if (!el) return;
      const style = getComputedStyle(el);
      bag.push({
        property: style.transitionProperty,
        duration: style.transitionDuration,
        timing: style.transitionTimingFunction,
        width: el.getBoundingClientRect().width,
        hidden: el.getAttribute('aria-hidden'),
        inert: el.inert,
        running: el.getAnimations().some(animation => animation.playState === 'running'),
      });
    };
    const watch = () => {
      const el = document.querySelector<HTMLElement>('.workspace-tab-exit');
      if (!el || el.dataset.watching) return;
      el.dataset.watching = 'true';
      let frames = 0;
      const step = () => {
        take();
        if (++frames < 8 && document.querySelector('.workspace-tab-exit')) requestAnimationFrame(step);
      };
      take();
      requestAnimationFrame(step);
    };
    new MutationObserver(watch).observe(document.querySelector('.workspace-tabstrip')!, { childList: true, subtree: true });
  });
  await page.getByRole('button', { name: 'Close Alpha', exact: true }).click();
  // The neighbour is focused on the close, while the slot is still collapsing.
  await expect(page.getByRole('tab', { name: 'Beta', exact: true })).toBeFocused();
  const samples = await page.evaluate(() => (window as unknown as { __exit: { property: string; duration: string; timing: string; width: number; hidden: string | null; inert: boolean; running: boolean }[] }).__exit);
  expect(samples.length).toBeGreaterThan(0);
  const motion = samples.find(sample => widthTransition(sample.property, sample.duration).hasWidth) ?? samples[0];
  const transition = widthTransition(motion.property, motion.duration);
  expect(transition.hasWidth).toBe(true);
  expect(transition.seconds).toBeCloseTo(durBase, 2);
  expect(motion.hidden).toBe('true');
  expect(motion.inert).toBe(true);
  const ease = await page.evaluate(() => {
    const probe = document.createElement('div');
    probe.style.transitionTimingFunction = 'var(--ease)';
    document.body.append(probe);
    const value = getComputedStyle(probe).transitionTimingFunction;
    probe.remove();
    return value;
  });
  const firstCurve = motion.timing.match(/^cubic-bezier\([^)]+\)/)?.[0] ?? motion.timing;
  expect(firstCurve.replaceAll(' ', '')).toBe(ease.replaceAll(' ', ''));
  const widths = samples.map(sample => sample.width);
  expect(samples.some(sample => sample.running) || Math.max(...widths) > Math.min(...widths)).toBe(true);

  await expect(page.locator('.workspace-tab-exit')).toHaveCount(0);
  const after = (await gamma.boundingBox())!;
  expect(before.x - after.x).toBeGreaterThan(parseFloat(foundation['tab-min-width']) / 2);

  const entry = await page.locator('.workspace-tab').first().evaluate(el => {
    const style = getComputedStyle(el);
    let scales = false;
    for (const sheet of document.styleSheets) {
      let rules: CSSRuleList;
      try { rules = sheet.cssRules; } catch { continue; }
      for (const rule of rules) {
        if (!(rule instanceof CSSKeyframesRule) || rule.name !== 'tab-enter') continue;
        scales = [...rule.cssRules].some(frame => frame instanceof CSSKeyframeRule && frame.style.transform !== '');
      }
    }
    return { name: style.animationName, transform: style.transform, scales };
  });
  expect(entry.name).toBe('tab-enter');
  expect(entry.transform).toBe('none');
  expect(entry.scales).toBe(false);
});

test('SH-073 reduced motion removes the close slide and the entry fade', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await open(page);
  const ruled = await page.evaluate(() => {
    const host = document.querySelector('.workspace-tabstrip');
    const probe = document.createElement('div');
    probe.className = 'workspace-tab-exit';
    probe.setAttribute('data-collapsed', '');
    host?.append(probe);
    const style = getComputedStyle(probe);
    const tab = getComputedStyle(document.querySelector('.workspace-tab')!);
    const result = { property: style.transitionProperty, duration: style.transitionDuration, animation: tab.animationName };
    probe.remove();
    return result;
  });
  expect(ruled.property).toBe('none');
  expect(ruled.animation).toBe('none');

  const gamma = page.getByRole('tab', { name: 'Gamma', exact: true });
  const before = (await gamma.boundingBox())!;
  await page.getByRole('button', { name: 'Close Alpha', exact: true }).click();
  await expect(page.locator('.workspace-tab-exit')).toHaveCount(0);
  await expect(page.getByRole('tab', { name: 'Beta', exact: true })).toBeFocused();
  const after = (await gamma.boundingBox())!;
  expect(before.x - after.x).toBeGreaterThan(parseFloat(foundation['tab-min-width']) / 2);
});
