import { test, expect, type Page } from '@playwright/test';
import type { AttentionItem } from '../../src/features/chat/world-client';
import { installMockEngine } from './support/mock-engine';

// App mounts Places, then Next up, then focus history. The strip draws the pill and the banner
// from that Next up. An engine that omits the newer attention fields still boots, and the rail has no Inbox row.
const CHAT = '9446cc2627f3deae';
const OTHER = 'ab12cd34ef56ab78';
const SESSION = `/mock/places/${CHAT}/transcript.jsonl`;

/** The fields an engine from before Iteration 2 actually sent. The newer ones are absent, not null. */
function oldQuestion(key: string, session: string, text: string): AttentionItem {
  return { key, session, kind: 'consent', text, sourceFolders: [], answerable: true, title: 'Config parser' };
}

async function boot(page: Page) {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  // A missing file is the browser's network line. An exception from a field the old feed omitted is a page error.
  page.on('console', message => {
    if (message.type() === 'error' && !message.text().startsWith('Failed to load resource')) errors.push(message.text());
  });
  await installMockEngine(page, {
    world: {
      rows: [],
      items: [
        oldQuestion(`${CHAT}:consent:1`, CHAT, 'Stay on this one?'),
        oldQuestion(`${OTHER}:consent:1`, OTHER, 'Allow 3 git actions?'),
      ],
    },
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
    localStorage.setItem('codeaf.desktop.workspace.v1', workspace);
    localStorage.setItem('codeaf.desktop.focus.v1.main', history);
  }, [JSON.stringify(tabs), JSON.stringify(focus)] as const);
  await page.goto('/');
  return errors;
}

test('an engine without the new attention fields boots with the pill, the back chip, and no Inbox row', async ({ page }) => {
  const errors = await boot(page);
  await expect(page.locator('.app-shell')).toHaveAttribute('data-providers', 'places next-up focus-history');
  await expect(page.getByRole('navigation', { name: 'Places' }).getByRole('button', { name: 'Inbox', exact: true })).toHaveCount(0);
  const pill = page.locator('.workspace-frame-slot .frame-pill');
  await expect(pill).toBeVisible();
  // The conversation on screen is left out. The other question still counts, with no blocking field to read.
  await expect(pill).toHaveAccessibleName(/1 need you elsewhere/);
  await expect(page.locator('.nextup-banner')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Back to Home' })).toBeVisible();
  expect(errors).toEqual([]);
});
