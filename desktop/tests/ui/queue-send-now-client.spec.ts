import { test, expect } from '@playwright/test';

for (const outcome of ['running', 'idle', 'conflict', 'conflict-offline', 'refused'] as const) {
  test(`sendQueueNow: ${outcome}`, async ({ page }) => {
    // This fixture mounts the public hook directly because the queue menu belongs to another lane.
    await page.route('**/__queue-hook', route => route.fulfill({ contentType: 'text/html', body: '<!doctype html><div id="root"></div>' }));
    await page.goto('/__queue-hook');
    const result = await page.evaluate(async (outcome) => {
      const refreshPath = '/@react-refresh';
      const { default: refresh } = await import(refreshPath);
      refresh.injectIntoGlobalHook(window);
      const runtime = window as unknown as { $RefreshReg$: () => void; $RefreshSig$: () => (type: unknown) => unknown; __vite_plugin_react_preamble_installed__: boolean };
      runtime.$RefreshReg$ = () => {};
      runtime.$RefreshSig$ = () => (type) => type;
      runtime.__vite_plugin_react_preamble_installed__ = true;
      const fixturePath = '/tests/ui/support/queue-hook.ts';
      const { React, createRoot, useConversation } = await import(fixturePath);
      const { createElement, act } = React;
      (globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
      const initial = {
        id: 'session/1', sessionFile: 'saved.jsonl', workspace: '/workspace',
        model: 'deepseek/deepseek-v4.1-flash', persistent: true, running: outcome !== 'idle', needsPerson: false,
        entries: [{ Role: 'user', Text: 'start' }], tasks: [], questions: [],
        queue: [{ id: 'queued-1', text: 'send this' }, { id: 'queued-2', text: 'keep this' }],
        usage: { Input: 0, Output: 0, CostUSD: 0, Duration: 0, Turns: 0 }, title: 'Queue test', seq: 1,
      };
      const calls: { path: string; method: string; body?: unknown }[] = [];
      const originalFetch = window.fetch;
      let mutated = false;
      window.fetch = async (input, init) => {
        const path = String(input);
        calls.push({ path, method: init?.method ?? 'GET', body: init?.body ? JSON.parse(String(init.body)) : undefined });
        if (path.includes('/events?')) return new Response(new ReadableStream({ start() {} }), { headers: { 'Content-Type': 'text/event-stream' } });
        if (path.endsWith('/queue-send')) {
          mutated = true;
          if (outcome.startsWith('conflict')) return Response.json({ error: 'different server wording' }, { status: 409 });
          if (outcome === 'refused') return Response.json({ error: 'queue is unavailable' }, { status: 500 });
          return Response.json({ accepted: true });
        }
        if (mutated && outcome === 'conflict-offline') throw new TypeError('offline');
        if (mutated && !outcome.startsWith('conflict') && outcome !== 'refused') {
          return Response.json({ ...initial, running: true, queue: initial.queue.slice(1), entries: [...initial.entries, { Role: 'user', Text: 'send this' }], seq: 2 });
        }
        // A stale refresh must not restore a row whose delivery the 409 already proved.
        return Response.json(initial);
      };
      let conversation: ReturnType<typeof useConversation>;
      function Harness() {
        conversation = useConversation({ sessionFile: initial.sessionFile, onSessionFile() {} });
        return null;
      }
      const root = createRoot(document.getElementById('root')!);
      try {
        await act(async () => { root.render(createElement(Harness)); });
        // Await the read-only attachment explicitly; React does not await an async effect.
        await act(async () => { await conversation.retry(); });
        await act(async () => { await conversation.sendQueueNow('queued-1'); });
        return { calls, queue: conversation.snapshot.queue, entries: conversation.snapshot.entries, failed: conversation.failed, running: conversation.snapshot.running };
      } finally {
        await act(async () => { root.unmount(); });
        window.fetch = originalFetch;
      }
    }, outcome);
    expect(result.calls.filter(call => call.path.endsWith('/queue-send'))).toEqual([
      { path: '/api/engine/sessions/session%2F1/queue-send', method: 'POST', body: { id: 'queued-1' } },
    ]);
    expect(result.calls.some(call => call.path.includes('/turn'))).toBe(false);
    expect(result.queue.map((row: { id: string }) => row.id)).toEqual(outcome === 'refused' ? ['queued-1', 'queued-2'] : ['queued-2']);
    if (outcome.startsWith('conflict')) {
      expect(result.failed).toEqual({ text: '', mode: 'submit', message: 'that message has already been sent' });
      expect(result.calls.some(call => call.path.endsWith('?since=1'))).toBe(true);
    } else if (outcome === 'refused') {
      expect(result.failed.message).toBe('queue is unavailable');
    } else {
      expect(result.failed).toBeUndefined();
      expect(result.running).toBe(true);
      expect(result.entries.map((entry: { Text: string }) => entry.Text)).toEqual(['start', 'send this']);
    }
  });
}
