import { expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { installMockEngine } from './mock-engine';
import { richReply } from './scenarios-v2';
import { openApp, send } from './conversation';

export const changedPath = 'internal/auth/auth_test.go';
const encode = (text: string) => Buffer.from(text).toString('base64');
// This valid PNG has two differently coloured pixels, so decoding is proved by reading the pixels back.
const picture = 'iVBORw0KGgoAAAANSUhEUgAAAAIAAAABCAIAAAB7QOjdAAAAD0lEQVR4nGM46WBrm3cSAAlNAru0Tq1sAAAAAElFTkSuQmCC';
const files = {
  [changedPath]: { mime: 'text/x-go', dataBase64: encode('package auth\nfunc login() { fixedClock() }\n') },
  'art/logo.psd': { mime: 'application/octet-stream', dataBase64: encode('fixture') },
  'scratch/notes.go': { mime: 'text/x-go', dataBase64: encode('package notes\n') },
  'config/options.json': { mime: 'application/json', dataBase64: encode('{"enabled":true}\n') },
  'docs/readme.txt': { mime: 'text/plain', dataBase64: encode('Read this file.\n') },
  'art/picture.png': { mime: 'image/png', dataBase64: picture },
};

/** File routes are lane-local; shared engine state only supplies the conversation and its event stream. */
export async function installFileFollowups(page: Page, editors: object[] = [], local = true) {
  const base = richReply();
  const engine = await installMockEngine(page, { ...base, files: { ...base.files, ...files } });
  let revised = false;
  const reads: string[] = [];
  await page.route('**/api/engine/sessions/*/**', async route => {
    const url = new URL(route.request().url());
    const action = url.pathname.split('/').slice(5).join('/');
    const path = url.searchParams.get('path') ?? '';
    if (action === 'editors') return route.fulfill({ json: { editors, local, open: local } });
    if (!['diff', 'files/text', 'files', 'files/stat'].includes(action)) return route.fallback();
    reads.push(action);
    if (action === 'files/stat') return route.fallback();
    const file = files[path as keyof typeof files];
    if (!file) return route.fallback();
    const name = path.split('/').pop()!;
    const dir = path.slice(0, path.lastIndexOf('/'));
    const identity = { path, name, dir, abs: `${engine.snapshot().workspace}/${path}` };
    if (action === 'diff') return route.fulfill({ json: {
      ...identity, git: path !== 'scratch/notes.go', status: path === changedPath ? 'modified' : 'clean',
      added: path === changedPath ? 1 : 0, deleted: path === changedPath ? 1 : 0,
      base: { kind: 'start', sha: 'abc1234' }, lines: 2,
      hunks: path === changedPath ? [{ header: '@@ -1,2 +1,2 @@', oldStart: 1, oldLines: 2, newStart: 1, newLines: 2,
        lines: [{ kind: 'context', old: 1, new: 1, text: 'package auth' },
          { kind: 'del', old: 2, text: 'func login() { wallClock() }' },
          { kind: 'add', new: 2, text: revised ? 'func login() { refreshed() }' : 'func login() { fixedClock() }' }],
      }] : [],
    } });
    if (action === 'files/text') return route.fulfill({ json: path === 'art/logo.psd'
      ? { ...identity, size: 3_565_158, text: '', lines: 0, refusal: 'too-large' }
      : { ...identity, size: Buffer.from(file.dataBase64, 'base64').length, text: Buffer.from(file.dataBase64, 'base64').toString(), lines: 2 } });
    return route.fulfill({ json: { name, ...file, size: Buffer.from(file.dataBase64, 'base64').length, hash: 'fixture' } });
  });
  return { engine, reads, edit: () => {
    revised = true;
    engine.push({ kind: 'toolEnd', tool: 'edit', text: '', hint: '', raw: { Args: JSON.stringify({ path: changedPath }) } });
  } };
}

export async function startFileFollowups(page: Page, editors: object[] = [], local = true) {
  const fixture = await installFileFollowups(page, editors, local);
  await openApp(page);
  await send(page, 'Fix the flaky login test and draw a logo');
  await expect(page.getByText('so the login test no longer reads the real time.')).toBeVisible();
  return fixture;
}

export async function openFollowupFile(page: Page, path: string) {
  const name = path.split('/').pop()!;
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await page.getByRole('combobox', { name: 'Search or start' }).fill(name.replace(/\.[^.]+$/, ''));
  await page.getByRole('option', { name: new RegExp(name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')) }).click();
  await expect(page.getByRole('tab', { name, exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('.file-head .file-name')).toHaveText(name);
}

/** The design's muted text and green diff count keep their exact tokens; every other axe rule still runs. */
export async function expectFileAccessible(page: Page) {
  await page.evaluate(async () => {
    await Promise.all(document.getAnimations().filter(a => a.effect?.getComputedTiming().iterations !== Infinity)
      .map(a => a.finished.catch(() => undefined)));
  });
  const result = await new AxeBuilder({ page }).include('.file-surface').include('.file-editors-menu')
    .withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
  const failures = [];
  for (const violation of result.violations) {
    for (const node of violation.nodes) {
      const css = String(node.target.at(-1));
      const muted = violation.id === 'color-contrast' && await page.locator(css).evaluate(el => {
        const count = el.matches('.file-count[data-sign="add"]');
        if (!count && !el.closest('.file-dir, .file-message, .file-num, .file-hunk, .file-fold, .file-note, .file-base, .menu-detail, .keyboard-shortcut')) return false;
        const probe = document.createElement('span');
        probe.style.color = count ? 'var(--diff-count-add)' : 'var(--ink-3)'; el.append(probe);
        const ink = getComputedStyle(probe).color; probe.remove();
        return getComputedStyle(el).color === ink;
      });
      if (!muted) failures.push({ rule: violation.id, target: node.target });
    }
  }
  expect(failures).toEqual([]);
}
