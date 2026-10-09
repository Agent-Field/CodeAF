// A typed stand-in for src-tauri/src/web.rs at the IPC boundary. It replaces
// window.__TAURI_INTERNALS__ (what @tauri-apps/api's invoke and listen call),
// so the renderer's real adapter (src/design/nativeWeb.ts) runs unchanged.
// It mirrors the native argument checks so a malformed call fails here as it
// would there, and records every call for assertions.
//
// It is NOT native proof: no page is loaded, no view is drawn and no snapshot
// is taken by a platform. The snapshot it returns is a fixed 1x1 PNG.

import type { Page } from '@playwright/test';

export type NativeCall = { cmd: string; args: Record<string, unknown> };
export type MockState = { pane: string; url: string; title: string; loading: boolean; canBack: boolean; canForward: boolean; historyKnown: boolean; failure: unknown; notice: unknown };

export const MOCK_SHOT = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

export async function installNativeWebMock(page: Page, options: { snapshot?: boolean; engine?: boolean } = {}) {
  await page.addInitScript(({ shot, snapshot, engine }) => {
    const calls: { cmd: string; args: Record<string, unknown> }[] = [];
    const views = new Map<string, Record<string, unknown>>();
    const callbacks = new Map<number, (data: unknown) => void>();
    const listeners = new Map<string, number[]>();
    let nextId = 1;
    const paneOk = (p: unknown) => typeof p === 'string' && /^[A-Za-z0-9_-]{1,64}$/.test(p);
    const urlOk = (u: unknown) => typeof u === 'string' && u.length <= 4096 && /^https?:\/\/[^\s@/]+[^\s]*$/i.test(u);
    const rectOk = (r: unknown) => !!r && typeof r === 'object' && ['x', 'y', 'width', 'height'].every(k => Number.isInteger((r as Record<string, unknown>)[k])) && (r as { width: number }).width >= 0 && (r as { height: number }).height >= 0;
    const fail = (message: string) => Promise.reject(message);
    function emit(event: string, payload: unknown) {
      for (const id of listeners.get(event) ?? []) callbacks.get(id)?.({ event, id, payload });
    }
    async function invoke(cmd: string, args: Record<string, unknown> = {}) {
      calls.push({ cmd, args: JSON.parse(JSON.stringify(args)) });
      if (cmd === 'plugin:event|listen') {
        const list = listeners.get(args.event as string) ?? [];
        list.push(args.handler as number);
        listeners.set(args.event as string, list);
        return args.handler;
      }
      if (cmd.startsWith('plugin:')) return null;
      // With `engine`, the desktop engine connection points at this page's own origin, where a test's mock engine
      // answers /api/engine; without it the engine is unreachable, as in a packaged app that has not started it.
      if (cmd === 'engine_connection' && engine) return { url: location.origin, token: 'mock-token', model: 'deepseek/deepseek-v4.1-flash' };
      if (cmd === 'engine_connection' || cmd === 'engine_health') return fail('The local engine could not start');
      if (cmd === 'open_url') return urlOk(args.url) ? null : fail('Only web links can be opened');
      if (cmd === 'web_list') return [...views.values()];
      // Rust answers null when no tab is waiting for this window (windows.rs window_claim_handoff); every boot asks.
      if (cmd === 'window_claim_handoff') return null;
      if (!cmd.startsWith('web_')) return fail(`Command ${cmd} not found`);
      if (!paneOk(args.pane)) return fail('That pane cannot hold a web page');
      const pane = args.pane as string;
      const view = views.get(pane);
      switch (cmd) {
        case 'web_open': {
          if (!urlOk(args.url)) return fail('Only http and https pages open in codeaf');
          if (!rectOk(args.rect) || typeof args.visible !== 'boolean') return fail('That pane has no usable size');
          const state = { pane, url: args.url, title: '', loading: true, canBack: false, canForward: false, historyKnown: true, failure: null, notice: null };
          views.set(pane, state);
          return state;
        }
        case 'web_navigate':
          if (!view) return fail('That web page is closed');
          return urlOk(args.url) ? null : fail('Only http and https pages open in codeaf');
        case 'web_bounds': return view ? (rectOk(args.rect) ? null : fail('That pane has no usable size')) : fail('That web page is closed');
        case 'web_visible': return view ? (typeof args.visible === 'boolean' ? null : fail('bad')) : fail('That web page is closed');
        case 'web_history': return view ? (['back', 'forward', 'reload', 'stop'].includes(args.step as string) ? null : fail('bad step')) : fail('That web page is closed');
        case 'web_close': views.delete(pane); return view ? null : fail('That web page is closed');
        case 'web_snapshot': return view && snapshot ? { image: shot, url: view.url } : fail('unavailable');
      }
      return fail(`Command ${cmd} not found`);
    }
    Object.assign(window, { isTauri: true });
    (window as unknown as Record<string, unknown>).__TAURI_INTERNALS__ = {
      invoke,
      transformCallback(callback: (data: unknown) => void) { const id = nextId++; callbacks.set(id, callback); return id; },
      unregisterCallback(id: number) { callbacks.delete(id); },
      runCallback(id: number, data: unknown) { callbacks.get(id)?.(data); },
      callbacks,
      metadata: { currentWindow: { label: 'main' }, currentWebview: { windowLabel: 'main', label: 'main' } },
      convertFileSrc: (path: string) => path,
    };
    (window as unknown as Record<string, unknown>).__TAURI_EVENT_PLUGIN_INTERNALS__ = { unregisterListener() {} };
    (window as unknown as Record<string, unknown>).__nativeWeb = {
      calls,
      views,
      emit,
      /** Sends a state for a pane, starting from what web_open reported. */
      state(pane: string, change: Record<string, unknown>) {
        const next = { ...(views.get(pane) ?? {}), ...change };
        views.set(pane, next);
        emit('web://state', next);
      },
    };
  }, { shot: MOCK_SHOT, snapshot: options.snapshot ?? true, engine: options.engine ?? false });
}

export const nativeCalls = (page: Page, cmd?: string) => page.evaluate(name => {
  const all = (window as unknown as { __nativeWeb: { calls: NativeCall[] } }).__nativeWeb.calls;
  return name ? all.filter(call => call.cmd === name) : all;
}, cmd) as Promise<NativeCall[]>;

export const emitState = (page: Page, pane: string, change: Partial<MockState>) =>
  page.evaluate(([p, c]) => (window as unknown as { __nativeWeb: { state: (pane: string, change: unknown) => void } }).__nativeWeb.state(p as string, c), [pane, change] as const);

export const emitNewTab = (page: Page, opener: string, url: string) =>
  page.evaluate(([o, u]) => (window as unknown as { __nativeWeb: { emit: (e: string, p: unknown) => void } }).__nativeWeb.emit('web://new-tab', { opener: o, url: u }), [opener, url] as const);

/** A workspace with one web tab (and a conversation beside it) for the pane under test. */
export async function seedWebTab(page: Page, url = 'https://pkg.go.dev/encoding/json#Decoder', id = 'webpane1') {
  const state = {
    tabs: [
      { id: 'chat1', title: 'Config stack', draft: '', pinned: false },
      { id, title: 'pkg.go.dev', draft: '', pinned: false, kind: 'web', target: { url } },
    ],
    groups: [], closed: [], activeId: id, nextNumber: 3, recentIds: [id, 'chat1'],
  };
  await page.addInitScript(value => { if (!sessionStorage.getItem('seeded')) { localStorage.setItem('codeaf.desktop.workspace.v1', value); sessionStorage.setItem('seeded', '1'); } }, JSON.stringify(state));
}
