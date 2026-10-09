import { test, expect, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { INK3_TEXT, expectAccessible } from '../ui/contracts';

// The Using chip and sheet (Places 6e, 6f) driven through real buttons and keys over a MOCK bridge (tests/using/harness/mock.ts): the
// places, sources and settings below are invented for the tests. What is asserted is the contract: the engine's own state word is the
// only thing that says a setting is in use, a choice and an apply are real POSTs, and a failure stays on screen.
// Screenshots go outside the source tree, for review only.
INK3_TEXT.push('.using-label', '.using-sublabel', '.using-note', '.using-row-aside', '.using-source-detail', '.using-source-note', '.using-place[data-inherited]',
  '.using-choice-value', '.using-harness-mock', '.using-harness-body', '.using-harness-log', '.using-row-main .app-icon', '.using-row-open');
const shots = process.env.USING_SHOTS ?? '/home/santosh/.codex/codeaf-design-run/using-ui-shots';
mkdirSync(shots, { recursive: true });

type Calls = { kind: string; field?: string; placeId?: string }[];
declare global { interface Window { __usingCalls: Calls; __heal?: () => void } }

async function open(page: Page, scenario: string, theme: 'light' | 'dark' = 'light', extra = '') {
  await page.emulateMedia({ colorScheme: theme });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.goto(`/?scenario=${scenario}${extra}`);
  await page.getByTestId('using-specimen').waitFor();
}
const chip = (page: Page) => page.getByRole('button', { name: /^(Using|Places)/ });
const sheet = (page: Page) => page.getByRole('dialog', { name: 'What this conversation is using' });
const log = (page: Page) => page.getByRole('list', { name: 'Callback log' }).locator('li');
const calls = (page: Page, kind: string) => page.evaluate(wanted => window.__usingCalls.filter(call => call.kind === wanted), kind);
const shot = (page: Page, name: string, theme: string) => page.screenshot({ path: `${shots}/${name}-${theme}-${test.info().project.name}.png` });
async function openSheet(page: Page) {
  await chip(page).click();
  await expect(sheet(page)).toBeVisible();
}

for (const theme of ['light', 'dark'] as const) {
  test.describe(`${theme}`, () => {
    test('chip: the count, an attention mark while a call is yours, and a 24px design-sized pill', async ({ page }) => {
      await open(page, 'full', theme);
      await expect(chip(page)).toHaveText('Using 3 places · 5 sources');
      await expect(chip(page)).toHaveCSS('height', '24px');
      await expect(chip(page)).toHaveCSS('border-top-left-radius', '7px');
      await expect(chip(page)).toHaveAttribute('aria-expanded', 'false');
      await expect(chip(page).getByRole('img', { name: 'Needs your choice' })).toBeVisible();
      await expectAccessible(page);
    });

    test('keyboard: Enter opens, focus lands in the sheet, Escape closes and returns to the chip', async ({ page }) => {
      await open(page, 'full', theme);
      await chip(page).focus();
      await page.keyboard.press('Enter');
      await expect(sheet(page)).toBeFocused();
      await expect(chip(page)).toHaveAttribute('aria-expanded', 'true');
      await page.keyboard.press('Tab');
      await expect(sheet(page).locator(':focus')).toHaveCount(1);
      await page.keyboard.press('Escape');
      await expect(sheet(page)).toHaveCount(0);
      await expect(chip(page)).toBeFocused();
      // A press outside closes it too, without taking focus from where the person pressed.
      await page.keyboard.press('Enter');
      await expect(sheet(page)).toBeVisible();
      await page.getByText('The conversation continues here.').click();
      await expect(sheet(page)).toHaveCount(0);
    });

    test('sheet: places, instructions, sources and policy, each under the place it came from', async ({ page }) => {
      await open(page, 'full', theme);
      await openSheet(page);
      const s = sheet(page);
      await expect(s).toHaveCSS('border-top-left-radius', '12px');
      const places = s.getByRole('list', { name: 'Places this conversation belongs to' });
      await expect(places.getByRole('listitem')).toHaveText(['Config parser', 'Release', 'codeaf · inherited through Release']);
      const instructions = s.locator('[data-kind="instruction"]');
      await expect(instructions).toHaveCount(2);
      await expect(instructions.nth(0)).toContainText('Keep strict mode the default for public APIs');
      await expect(instructions.nth(0)).toContainText('Config parser');
      // The longest is cut short for the model, and the sheet says so; a click opens the whole text.
      await expect(instructions.nth(1)).toContainText('Release, codeaf');
      await expect(instructions.nth(1).getByText('Cut short')).toBeVisible();
      await expect(s.getByText('1 cut short to fit the instruction limit; the model reads the rest.')).toBeVisible();
      const longer = instructions.nth(1).locator('.using-row-text');
      const folded = (await longer.boundingBox())!.height;
      await instructions.nth(1).getByRole('button').click();
      expect((await longer.boundingBox())!.height).toBeGreaterThan(folded);
      const given = s.getByRole('list', { name: 'Sources the model reads' });
      await expect(given.getByRole('listitem')).toHaveCount(5);
      await expect(given.locator('[data-source-key="file:/work/brand-voice.md"]')).toContainText('Release · added by the AI');
      await expect(given.locator('[data-source-key="repo:/work/codeaf/internal/parse"]')).toContainText('Config parser');
      await expect(s.getByText('1 source left out for room · 1 refused · 1 not found')).toBeVisible();
      await expect(s.getByRole('list', { name: 'Sources left out for room' }).getByText('Left out')).toBeVisible();
      const refused = s.getByRole('list', { name: 'Sources refused' });
      await expect(refused).toContainText('The codeaf folder holds credentials, so it is never given to a model.');
      await expect(refused.getByRole('button')).toHaveCount(0);
      const missing = given.locator('[data-source-key="file:/work/old-notes.md"]');
      await expect(missing.getByText('Not found')).toBeVisible();
      await expect(missing).toContainText('This file is no longer at /work/old-notes.md.');
      await expect(missing.getByRole('button')).toHaveCount(0);
      await shot(page, 'sheet-full', theme);
      await expectAccessible(page);
    });

    test('open: each source hands the shell a typed file, repo, url or chat; a missing or refused one has no control', async ({ page }) => {
      await open(page, 'full', theme);
      await openSheet(page);
      const s = sheet(page);
      await s.getByRole('button', { name: 'Open brand-voice.md' }).click();
      await s.getByRole('button', { name: 'Open parse' }).click();
      await s.getByRole('button', { name: 'Open codeaf.dev/changelog' }).click();
      await s.getByRole('button', { name: 'Open Config stack' }).click();
      await s.getByRole('button', { name: 'Open archive' }).click();
      await expect(log(page)).toHaveText([
        'open:{"kind":"file","path":"/work/brand-voice.md"}',
        'open:{"kind":"repo","path":"/work/codeaf/internal/parse","repoRoot":"/work/codeaf"}',
        'open:{"kind":"url","url":"https://codeaf.dev/changelog"}',
        'open:{"kind":"chat","chatId":"chat-earlier"}',
        'open:{"kind":"folder","path":"/work/archive"}',
      ]);
      await expect(s.getByRole('button', { name: /Open old-notes/ })).toHaveCount(0);
    });

    test('open: with no navigation handler there is no Open control to go nowhere', async ({ page }) => {
      await open(page, 'full', theme, '&open=0');
      await openSheet(page);
      await expect(sheet(page).getByRole('button', { name: /^Open / })).toHaveCount(0);
      await expect(sheet(page).locator('.using-source-name', { hasText: 'brand-voice.md' })).toBeVisible();
    });

    test('conflict: a pick is a real POST, remembered, and never called applied by the client', async ({ page }) => {
      await open(page, 'full', theme);
      await openSheet(page);
      const model = sheet(page).locator('[data-field="model"]');
      await expect(model.getByText('Pick one')).toBeVisible();
      await expect(model).toContainText('Release and Config parser want different models, so none is applied until you pick.');
      await expect(model).toContainText('Release wanted flash · Config parser wanted pro');
      await expect(model.getByText('Asked once; the answer is remembered for this conversation.', { exact: false })).toBeVisible();
      await model.getByRole('button', { name: /Release\s+flash/ }).click();
      await expect(model.getByText('From your next message')).toBeVisible();
      await expect(model.getByText('In use')).toHaveCount(0);
      await expect(model.getByRole('button', { name: /Release\s+flash/ })).toHaveAttribute('aria-pressed', 'true');
      expect(await calls(page, 'choose')).toEqual([{ kind: 'choose', field: 'model', placeId: 'pl_release' }]);
      // Picking the one already chosen asks nothing of the engine; picking the other changes the remembered answer.
      await model.getByRole('button', { name: /Release\s+flash/ }).click();
      expect(await calls(page, 'choose')).toHaveLength(1);
      await model.getByRole('button', { name: /Config parser\s+pro/ }).click();
      await expect(model.getByRole('button', { name: /Config parser\s+pro/ })).toHaveAttribute('aria-pressed', 'true');
      expect(await calls(page, 'choose')).toHaveLength(2);
    });

    test('conflict: a refused pick keeps the engine\'s sentence on screen and re-reads the list', async ({ page }) => {
      await open(page, 'full', theme, '&choiceFails=1');
      await openSheet(page);
      const model = sheet(page).locator('[data-field="model"]');
      const before = (await calls(page, 'using')).length;
      await model.getByRole('button', { name: /Release\s+flash/ }).click();
      const alert = sheet(page).getByRole('alert');
      await expect(alert).toContainText('That choice is no longer needed: the places agree now.');
      await expect.poll(async () => (await calls(page, 'using')).length).toBeGreaterThan(before);
      await expect(alert).toBeVisible();
      await alert.getByRole('button', { name: 'Dismiss' }).click();
      await expect(alert).toHaveCount(0);
    });

    test('permissions: a wider setting is held until a deliberate two-step apply', async ({ page }) => {
      await open(page, 'full', theme);
      await openSheet(page);
      const row = sheet(page).locator('[data-field="permissions"]');
      await expect(row).toContainText('Permissions: Allow');
      await expect(row.getByText('Waiting for you')).toBeVisible();
      await expect(row).toContainText('codeaf allows more than this conversation does now, so it is held until you say so.');
      await expect(row).toContainText('Running on Ask now.');
      const ask = row.getByRole('button', { name: 'Use Allow in this chat…' });
      await ask.click();
      expect(await calls(page, 'apply')).toEqual([]);
      const confirm = row.getByRole('button', { name: 'Use Allow', exact: true });
      await expect(confirm).toBeFocused();
      await expect(row).toContainText('Change permissions here from Ask to Allow?');
      // Escape backs out of the question only; the sheet stays and focus goes back to the button that asked.
      await page.keyboard.press('Escape');
      await expect(sheet(page)).toBeVisible();
      await expect(confirm).toHaveCount(0);
      await expect(ask).toBeFocused();
      await ask.click();
      await row.getByRole('button', { name: 'Cancel' }).click();
      expect(await calls(page, 'apply')).toEqual([]);
      await ask.click();
      await page.keyboard.press('Enter');
      expect(await calls(page, 'apply')).toEqual([{ kind: 'apply', field: 'permissions' }]);
      await expect(row.getByText('Your choice')).toBeVisible();
      await expect(row.getByRole('button', { name: /Use Allow/ })).toHaveCount(0);
    });

    test('a quiet list: the engine says applied, and only then does the sheet say in use', async ({ page }) => {
      await open(page, 'quiet', theme);
      await expect(chip(page)).toHaveText('Using 3 places · 2 sources');
      await expect(chip(page).getByRole('img')).toHaveCount(0);
      await openSheet(page);
      await expect(sheet(page).locator('[data-field="model"]').getByText('In use')).toBeVisible();
      await expect(sheet(page).locator('[data-field="model"]')).toContainText('Model: pro');
      await expect(sheet(page).getByText('left out')).toHaveCount(0);
    });

    test('failure: the sentence stays, survives closing the sheet, and Try again recovers', async ({ page }) => {
      await open(page, 'failing', theme);
      await expect(chip(page)).toHaveText('Places unavailable');
      await openSheet(page);
      await expect(sheet(page).getByRole('alert')).toContainText('The engine could not read the places file.');
      await page.keyboard.press('Escape');
      await openSheet(page);
      await expect(sheet(page).getByRole('alert')).toContainText('The engine could not read the places file.');
      await shot(page, 'sheet-failed', theme);
      await page.evaluate(() => window.__heal?.());
      await sheet(page).getByRole('button', { name: 'Try again' }).click();
      await expect(chip(page)).toHaveText('Using 3 places · 5 sources');
      await expect(sheet(page).getByRole('alert')).toHaveCount(0);
    });

    test('offline: says the engine is not reachable', async ({ page }) => {
      await open(page, 'offline', theme);
      await expect(chip(page)).toHaveText('Places offline');
      await openSheet(page);
      await expect(sheet(page).getByRole('alert')).toContainText('The engine is not reachable. Nothing answered.');
      await expectAccessible(page);
    });

    test('an engine that reads no places: no claim that anything reached it', async ({ page }) => {
      await open(page, 'noengine', theme);
      await expect(chip(page)).toHaveText('Places not read');
      await openSheet(page);
      const s = sheet(page);
      await expect(s.getByRole('status')).toContainText('This conversation was opened by another program, which reads no places.');
      await expect(s.getByRole('list', { name: 'Places this conversation belongs to' })).toBeVisible();
      await expect(s.getByText('Instructions', { exact: true })).toHaveCount(0);
      await expect(s.getByText('Sources', { exact: true })).toHaveCount(0);
      await expect(s.getByText('Policy', { exact: true })).toHaveCount(0);
    });

    test('nothing to say draws nothing: empty list, no api, and the wait for the first answer', async ({ page }) => {
      await open(page, 'empty', theme);
      await expect(chip(page)).toHaveCount(0);
      await open(page, 'absent', theme);
      await expect(chip(page)).toHaveCount(0);
      await open(page, 'slow', theme);
      await expect(chip(page)).toHaveCount(0);
      await expect(chip(page)).toHaveText('Using 3 places · 5 sources');
    });

    test('add to a place: a row only when the shell can file the conversation', async ({ page }) => {
      await open(page, 'full', theme);
      await openSheet(page);
      await expect(sheet(page).getByRole('button', { name: 'Add to a place…' })).toHaveCount(0);
      await open(page, 'full', theme, '&add=1');
      await openSheet(page);
      await sheet(page).getByRole('button', { name: 'Add to a place…' }).click();
      await expect(log(page)).toHaveText(['add-to-place']);
      await expect(sheet(page)).toHaveCount(0);
    });

    for (const width of [320, 600, 1200]) {
      test(`${width}px: no horizontal overflow, the sheet inside the window, every control reachable`, async ({ page }) => {
        await page.setViewportSize({ width, height: 700 });
        await open(page, 'full', theme);
        await openSheet(page);
        const s = sheet(page);
        const box = (await s.boundingBox())!;
        expect(box.x).toBeGreaterThanOrEqual(0);
        expect(box.x + box.width).toBeLessThanOrEqual(width);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
        expect(await s.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true);
        await shot(page, `sheet-${width}-top`, theme);
        // The sheet scrolls vertically; the last control is still reachable by keyboard.
        const last = s.locator('[data-field="permissions"]').getByRole('button', { name: 'Use Allow in this chat…' });
        await last.focus();
        await expect(last).toBeInViewport();
        await shot(page, `sheet-${width}`, theme);
        await expectAccessible(page);
      });
    }
  });
}
