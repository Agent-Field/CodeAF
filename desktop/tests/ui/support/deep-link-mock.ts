import { installNativeHttpMock } from './native-http-mock';
// A typed stand-in for src-tauri/src/links.rs at the IPC boundary. It replaces window.__TAURI_INTERNALS__ (what
// @tauri-apps/api's invoke and listen call), so the renderer's real adapter (src/design/nativeLinks.ts) runs unchanged.
// `deliver(page, links)` does what links.rs does when the operating system hands codeaf a link: queue it for this
// window and emit `deep-link://ready` to it. Links queued with `atBoot` are waiting before the page loads, as a link
// that launched the app is. Like links.rs it drops anything that is not a codeaf:// address before queueing it; the
// renderer's own parser is what the specs exercise beyond that.
// `clickNotice(page, targets)` does what src-tauri/src/activation.rs does when the platform reports a click on a
// notification codeaf posted: queue that notification's target for this window and emit `notification://activated`.
//
// It is NOT native proof: no operating system registered a scheme and no second process was forwarded.
import type { Page } from '@playwright/test';

export type LinkMockCall = { cmd: string; args: Record<string, unknown> };

export async function installDeepLinkMock(page: Page, options: { atBoot?: string[]; clipboard?: 'record' | 'refuse' } = {}) {
  await installNativeHttpMock(page);
  await page.addInitScript(({ atBoot, clipboard }) => {
    const calls: { cmd: string; args: Record<string, unknown> }[] = [];
    const queue: string[] = [];
    const notices: unknown[] = [];
    const callbacks = new Map<number, (data: unknown) => void>();
    const listeners = new Map<string, number[]>();
    let nextId = 1;
    const isLink = (raw: unknown) => typeof raw === 'string' && raw.toLowerCase().startsWith('codeaf://');
    function emit(event: string, payload: unknown) {
      for (const id of listeners.get(event) ?? []) callbacks.get(id)?.({ event, id, payload });
    }
    async function invoke(cmd: string, args: Record<string, unknown> = {}) {
      calls.push({ cmd, args: JSON.parse(JSON.stringify(args ?? {})) });
      if (cmd === 'plugin:event|listen') {
        const list = listeners.get(args.event as string) ?? [];
        list.push(args.handler as number);
        listeners.set(args.event as string, list);
        return args.handler;
      }
      if (cmd.startsWith('plugin:http|')) return (window as unknown as { __engineHttpInvoke: (cmd:string, args:Record<string,unknown>) => Promise<unknown> }).__engineHttpInvoke(cmd, args);
      if (cmd.startsWith('plugin:')) return null;
      // The engine connection points at this page's own origin, where the spec's mock engine answers /api/engine.
      if (cmd === 'engine_connection') return { url: location.origin, token: 'mock-token', model: 'deepseek/deepseek-v4.1-flash' };
      if (cmd === 'link_claim') return queue.splice(0, queue.length);
      if (cmd === 'notify_claim') return notices.splice(0, notices.length);
      if (cmd === 'window_claim_handoff') return null;
      if (cmd === 'window_context') return { label: 'main', placeKey: 'now' };
      if (cmd === 'web_list') return [];
      if (cmd === 'notify_attention') return { posted: 0, groups: 0, skipped: 'focused' };
      if (cmd === 'badge_set') return { applied: false, reason: 'unavailable' };
      return Promise.reject(`Command ${cmd} not found`);
    }
    queue.push(...atBoot.filter(isLink));
    const copied: string[] = [];
    Object.assign(window, {
      isTauri: true,
      __linkMock: {
        calls,
        copied,
        deliver(links: string[]) { const sound = links.filter(isLink); if (!sound.length) return; queue.push(...sound); emit('deep-link://ready', null); },
        clickNotice(targets: unknown[]) { notices.push(...targets); emit('notification://activated', null); },
      },
    });
    (window as unknown as Record<string, unknown>).__TAURI_INTERNALS__ = {
      invoke,
      transformCallback(callback: (data: unknown) => void) { const id = nextId++; callbacks.set(id, callback); return id; },
      unregisterCallback(id: number) { callbacks.delete(id); },
      runCallback(id: number, data: unknown) { callbacks.get(id)?.(data); },
      callbacks,
      metadata: { currentWindow: { label: 'main' }, currentWebview: { windowLabel: 'main', label: 'main' } },
      convertFileSrc: (path: string) => path,
    };
    (window as unknown as Record<string, unknown>).__TAURI_EVENT_PLUGIN_INTERNALS__ = { unregisterListener() { /* nothing to release */ } };
    // The clipboard is recorded (or refuses) so both browsers assert the exact text; Chromium's real clipboard is
    // checked separately in the spec.
    if (clipboard) {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: { writeText: async (text: string) => { if (clipboard === 'refuse') throw new DOMException('Write permission denied.', 'NotAllowedError'); copied.push(text); } },
      });
    }
  }, { atBoot: options.atBoot ?? [], clipboard: options.clipboard });
}

export const deliverLinks = (page: Page, links: string[]) => page.evaluate(value => (window as unknown as { __linkMock: { deliver(l: string[]): void } }).__linkMock.deliver(value), links);
export const copiedText = (page: Page) => page.evaluate(() => (window as unknown as { __linkMock: { copied: string[] } }).__linkMock.copied);
export const nativeCalls = (page: Page) => page.evaluate(() => (window as unknown as { __linkMock: { calls: { cmd: string }[] } }).__linkMock.calls.map(call => call.cmd));
export const clickNotice = (page: Page, targets: unknown[]) => page.evaluate(value => (window as unknown as { __linkMock: { clickNotice(t: unknown[]): void } }).__linkMock.clickNotice(value), targets);
