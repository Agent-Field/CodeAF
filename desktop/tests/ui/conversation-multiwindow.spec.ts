import { expect, test, type Page, type Route } from '@playwright/test';
import { expectAccessible } from './contracts';
import { expectNoHorizontalOverflow, message, posts } from './support/conversation';
import { installMockEngine, type MockEngine, type StreamRecord } from './support/mock-engine';
import { pendingQuestion } from './support/scenarios';

type WindowFeed = Window & {
  multiwindowFeed: {
    opened: number;
    active: number;
    peak: number;
    write?: (record: StreamRecord) => void;
  };
};

// A finite route.fulfill body closes SSE after every snapshot. Hold the browser
// transport open so this contract proves convergence on one subscription per window.
async function holdConversationStream(page: Page) {
  await page.addInitScript(() => {
    const feed = { opened: 0, active: 0, peak: 0 } as WindowFeed['multiwindowFeed'];
    (window as WindowFeed).multiwindowFeed = feed;
    const original = window.fetch.bind(window);
    window.fetch = async (input, init) => {
      if (!/\/sessions\/[^/]+\/events\?/.test(String(input))) return original(input, init);
      feed.opened += 1;
      feed.active += 1;
      feed.peak = Math.max(feed.peak, feed.active);
      const stream = new ReadableStream<Uint8Array>({ start(controller) {
        const write = (record: StreamRecord) => controller.enqueue(new TextEncoder().encode(`id: ${record.seq}\ndata: ${JSON.stringify(record)}\n\n`));
        feed.write = write;
        init?.signal?.addEventListener('abort', () => {
          controller.close();
          feed.active -= 1;
          if (feed.write === write) delete feed.write;
        }, { once: true });
      } });
      return new Response(stream, { headers: { 'Content-Type': 'text/event-stream' } });
    };
  });
}

for (const theme of ['light', 'dark'] as const) {
  for (const width of [320, 600, 1200]) {
    test(`CV-287: answer, queue edit and new turn mirror without reload · ${theme} · ${width}px`, async ({ context, page }) => {
      const other = await context.newPage();
      const pages = [page, other];
      let engine: MockEngine;
      const publish = async () => {
        const snapshot = engine.snapshot();
        const record: StreamRecord = { seq: snapshot.seq, type: 'snapshot', snapshot };
        await Promise.all(pages.map(view => view.evaluate(record => (window as WindowFeed).multiwindowFeed.write?.(record), record)));
      };

      // Register the existing mock's handler on both pages, once, so every write
      // changes one canonical session rather than two independent fixture copies.
      const sharedPage = new Proxy(page, {
        get(target, key) {
          if (key === 'route') return async (url: string, handler: (route: Route) => Promise<unknown>) => {
            for (const view of pages) await view.route(url, async route => {
              const before = engine?.snapshot().seq;
              await handler(route);
              if (engine && engine.snapshot().seq !== before) await publish();
            });
          };
          const value = Reflect.get(target, key);
          return typeof value === 'function' ? value.bind(target) : value;
        },
      });
      engine = await installMockEngine(sharedPage, { ...pendingQuestion(), manual: true });
      for (const view of pages) {
        await view.setViewportSize({ width, height: 800 });
        await view.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
        await holdConversationStream(view);
        await view.addInitScript(theme => {
          localStorage.setItem('codeaf-theme', theme);
          localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({
            tabs: [{ id: 'shared-chat', kind: 'conversation', title: 'Storage', titleSource: 'manual', draft: '', pinned: false, sessionFile: 'mock-session-1.jsonl' }],
            groups: [], closed: [], activeId: 'shared-chat', nextNumber: 2, recentIds: ['shared-chat'],
          }));
        }, theme);
        await view.goto('/');
        await expect(view.getByRole('region', { name: 'Waiting on you' })).toBeVisible();
        await expect.poll(() => view.evaluate(() => (window as WindowFeed).multiwindowFeed.active)).toBe(1);
      }
      expect(posts(engine, '/sessions').map(call => call.body)).toEqual([
        { sessionFile: 'mock-session-1.jsonl' }, { sessionFile: 'mock-session-1.jsonl' },
      ]);
      const navigations = [0, 0];
      pages.forEach((view, index) => view.on('framenavigated', frame => {
        if (frame === view.mainFrame()) navigations[index] += 1;
      }));

      await test.step('An answer in the first window clears the second window’s tray', async () => {
        const option = page.getByRole('radio', { name: /SQLite/ });
        await option.focus();
        await option.press('Space');
        const choose = page.getByRole('button', { name: 'Choose', exact: true });
        await choose.focus();
        await choose.press('Enter');
        await expect.poll(() => posts(engine, '/answer').length).toBe(1);
        expect(posts(engine, '/answer')[0].body).toMatchObject({ id: 7, kind: 'choice', key: 'sqlite' });
        for (const view of pages) await expect(view.getByRole('region', { name: 'Waiting on you' })).toHaveCount(0);
      });

      await test.step('Keyboard queue editing in the second window changes the first window', async () => {
        await message(page).fill('Check the indexes next');
        await message(page).press('Alt+Enter');
        for (const view of pages) await expect(view.locator('.queued-text')).toHaveText('Check the indexes next');
        const edit = other.getByRole('button', { name: 'Edit queued message', exact: true });
        await edit.focus();
        await edit.press('Enter');
        const field = other.getByRole('textbox', { name: 'Edit queued message', exact: true });
        await expect(field).toBeFocused();
        await field.fill('Check the unique indexes next');
        await field.press('Enter');
        for (const view of pages) await expect(view.locator('.queued-text')).toHaveText('Check the unique indexes next');
        expect(posts(engine, '/queue-edit')).toHaveLength(1);
        expect(posts(engine, '/queue-edit')[0].body).toMatchObject({ id: 'q1', text: 'Check the unique indexes next' });
        for (const view of pages) {
          await expectNoHorizontalOverflow(view);
          await expectAccessible(view);
        }
      });

      await test.step('The completed reply and a new keyboard-submitted turn appear in both windows', async () => {
        // The fixture starts mid-turn, so its initial reply has no pending send.
        engine.update({ entries: [...engine.snapshot().entries, ...pendingQuestion().turns![0].entries!] });
        engine.advance();
        await publish();
        for (const view of pages) {
          await expect(view.getByText('Using the option you picked.', { exact: true })).toBeVisible();
          await expect(view.locator('.queued-rows')).toHaveCount(0);
          await expect(view.getByRole('button', { name: 'Stop', exact: true })).toHaveCount(0);
        }
        await message(other).fill('Explain the transaction boundaries');
        await message(other).press('Enter');
        for (const view of pages) {
          await expect(view.locator('.user-message-text').filter({ hasText: 'Explain the transaction boundaries' })).toHaveCount(1);
          await expect(view.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
          await expect(message(view)).toHaveValue('');
          await expectNoHorizontalOverflow(view);
          await expectAccessible(view);
          expect(await view.evaluate(() => {
            const { opened, active, peak } = (window as WindowFeed).multiwindowFeed;
            return { opened, active, peak };
          })).toEqual({ opened: 1, active: 1, peak: 1 });
        }
        expect(posts(engine, '/turn').map(call => call.body)).toEqual([
          expect.objectContaining({ text: 'Check the indexes next', mode: 'queue' }),
          expect.objectContaining({ text: 'Explain the transaction boundaries', mode: 'submit' }),
        ]);
        expect(posts(engine, '/stop')).toHaveLength(0);
        expect(navigations).toEqual([0, 0]);
      });
    });
  }
}
