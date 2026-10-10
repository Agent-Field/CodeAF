import { invoke, isTauri } from '@tauri-apps/api/core';
import { ENGINE_REQUEST_TIMEOUT_MS } from '../chat/engine-client.ts';
import { checkEngine } from '../../lib/engine.ts';

/** The three state words (I-IFL-3..6). The ellipsis is the design's single character, not three dots. */
export const ENGINE_CONNECTED = 'Connected';
export const ENGINE_RECONNECTING = 'Reconnecting…';
export const ENGINE_UNREACHABLE = "Can't reach the engine";
/** The only local phrase the design gives for an engine on this computer. */
export const ENGINE_ON_THIS_MAC = 'On this Mac';

const LOOPBACK = new Set(['127.0.0.1', 'localhost', '::1', '[::1]']);

/**
 * Where the engine runs. The desktop app says "On this Mac" for a loopback
 * connection and the host name otherwise. The browser build has no
 * engine_connection command, so it names the dev transport address. An unknown
 * place is empty: the section does not invent one.
 */
export function engineWhere(input: { desktop: boolean; connectionUrl?: string; devAddress?: string }): string {
  if (!input.desktop) return (input.devAddress ?? '').trim();
  const raw = input.connectionUrl?.trim();
  if (!raw) return '';
  try {
    const host = new URL(raw).hostname;
    if (LOOPBACK.has(host)) return ENGINE_ON_THIS_MAC;
    return host;
  } catch {
    return '';
  }
}

/**
 * The address the dev server forwards /api/engine to. Vite replaces this
 * constant from the same value the proxy uses. The connection file's token is
 * never part of it, and a test run outside Vite has no address.
 */
export function devEngineAddress(): string {
  return typeof __CODEAF_DEV_ENGINE__ === 'string' ? __CODEAF_DEV_ENGINE__.trim() : '';
}

function abortError(): DOMException {
  return new DOMException('aborted', 'AbortError');
}

/** Settles with the work, or rejects once the signal aborts. A late success after that is dropped. */
function untilAbort<T>(work: Promise<T>, signal: AbortSignal): Promise<T> {
  if (signal.aborted) return Promise.reject(abortError());
  return new Promise((resolve, reject) => {
    let settled = false;
    const finish = (fn: () => void) => {
      if (settled) return;
      settled = true;
      signal.removeEventListener('abort', onAbort);
      fn();
    };
    const onAbort = () => finish(() => reject(abortError()));
    signal.addEventListener('abort', onAbort);
    work.then(value => finish(() => resolve(value)), error => finish(() => reject(error)));
  });
}

export type EngineReading = { ok: boolean; where: string };

async function desktopReading(signal: AbortSignal): Promise<EngineReading> {
  // The URL says where it runs. The token on the same command is never read.
  let url = '';
  try {
    const connection = await untilAbort(invoke<{ url?: unknown }>('engine_connection'), signal);
    if (typeof connection?.url === 'string') url = connection.url;
    await untilAbort(checkEngine(), signal);
    return { ok: true, where: engineWhere({ desktop: true, connectionUrl: url }) };
  } catch (error) {
    if (signal.aborted) throw error;
    return { ok: false, where: engineWhere({ desktop: true, connectionUrl: url }) };
  }
}

async function browserReading(signal: AbortSignal): Promise<EngineReading> {
  const where = engineWhere({ desktop: false, devAddress: devEngineAddress() });
  try {
    const response = await fetch('/api/engine/health', { cache: 'no-store', headers: { Accept: 'application/json' }, signal });
    // Health is the same door checkEngine uses. A missing or refused route did not answer ready.
    return { ok: response.ok, where };
  } catch (error) {
    if (signal.aborted) throw error;
    return { ok: false, where };
  }
}

/**
 * Reads engine_connection and checkEngine on the desktop, and the same health
 * route through the dev proxy in the browser. Past the engine's request limit
 * the check is unreachable (I-IFL-5). The caller's signal abandoning the check
 * rejects: a newer attempt, or the section leaving the page, owns that outcome.
 */
export async function readEngineWhere(caller?: AbortSignal): Promise<EngineReading> {
  if (caller?.aborted) throw abortError();
  const clock = new AbortController();
  const timer = setTimeout(() => clock.abort(), ENGINE_REQUEST_TIMEOUT_MS);
  const onCaller = () => clock.abort();
  try {
    caller?.addEventListener('abort', onCaller);
    return await (isTauri() ? desktopReading(clock.signal) : browserReading(clock.signal));
  } finally {
    clearTimeout(timer);
    caller?.removeEventListener('abort', onCaller);
  }
}
