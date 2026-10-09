// What one window keeps for itself between launches, per place: its unconfirmed changes, the last tab set the
// engine confirmed (so a window opens instantly, and offline), its focus and its scroll. Kept in localStorage
// under the window's own name, so two windows never write each other's copy.
//
// A window that closed with changes the engine never confirmed leaves its copy behind. The next window to open
// on that place takes those changes over — but only when nothing holds that window's lock (Web Locks: a live
// window holds its own for its lifetime), so a live window's queue is never replayed twice. Where Web Locks are
// missing, nothing is taken over and nothing is deleted: the copy waits for its own window to come back.
import { parseShared, type WindowLocal } from './shared.ts';
import type { Entry, Persisted } from './controller.ts';
import type { WorkspaceKey } from './client.ts';

export const persistPrefix = 'codeaf.desktop.workspace-sync.v1';
export const persistKey = (key: WorkspaceKey, writer: string) => `${persistPrefix}:${key}:${writer}`;
const windowIdKey = 'codeaf.desktop.window-id';
const lockName = (writer: string) => `codeaf-workspace-window:${writer}`;

const isObject = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const isStringList = (value: unknown): value is string[] => Array.isArray(value) && value.every(item => typeof item === 'string');
const isNumberMap = (value: unknown): value is Record<string, number> => isObject(value) && Object.values(value).every(n => typeof n === 'number' && Number.isFinite(n) && n >= 0);

/** A random writer tag in the engine's pattern. */
const freshId = () => `win-${(globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`).replace(/[^A-Za-z0-9_-]/g, '').slice(0, 40)}`;

/**
 * This window's name. A native window is named by its label (`main`, `w-3`), which the shell restores on relaunch,
 * so its focus, scroll and queue come back with it. A browser tab keeps a random name for its session.
 */
export function windowWriter(storage: Pick<Storage, 'getItem' | 'setItem'> | undefined = safeSession()): string {
  const label = (globalThis as { __TAURI_INTERNALS__?: { metadata?: { currentWindow?: { label?: unknown } } } }).__TAURI_INTERNALS__?.metadata?.currentWindow?.label;
  if (typeof label === 'string' && /^[A-Za-z0-9_-]{1,56}$/.test(label)) return `win-${label}`;
  try {
    const kept = storage?.getItem(windowIdKey);
    if (kept && /^[A-Za-z0-9_-]{1,64}$/.test(kept)) return kept;
    const id = freshId();
    storage?.setItem(windowIdKey, id);
    return id;
  } catch { return freshId(); }
}

/** A new name for a browser tab that turned out to share one (a duplicated tab copies its session storage). */
export function renameWindow(storage: Pick<Storage, 'setItem'> | undefined = safeSession()): string {
  const id = freshId();
  try { storage?.setItem(windowIdKey, id); } catch { /* The name lives for this page only. */ }
  return id;
}

function safeSession(): Storage | undefined { try { return globalThis.sessionStorage; } catch { return undefined; } }
export function safeLocal(): Storage | undefined { try { return globalThis.localStorage; } catch { return undefined; } }

function readEntries(value: unknown): Entry[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap(item => {
    if (!isObject(item) || !isObject(item.action) || typeof item.action.type !== 'string' || !isStringList(item.ids)) return [];
    const entry: Entry = { action: item.action as unknown as Entry['action'], ids: item.ids };
    if (typeof item.want === 'boolean') entry.want = item.want;
    if (typeof item.reopened === 'string') entry.reopened = item.reopened;
    if (typeof item.prior === 'string') entry.prior = item.prior;
    return [entry];
  });
}

function readLocal(value: unknown): WindowLocal {
  const v = isObject(value) ? value : {};
  return {
    activeId: typeof v.activeId === 'string' ? v.activeId : undefined,
    recentIds: isStringList(v.recentIds) ? v.recentIds : [],
    focus: isNumberMap(v.focus) ? v.focus : {},
    scroll: isNumberMap(v.scroll) ? v.scroll : {},
  };
}

/** Reads a saved copy; anything that does not validate is ignored (and left where it is), never trusted. */
export function parsePersisted(text: string | null): Persisted | undefined {
  if (!text) return undefined;
  let value: unknown;
  try { value = JSON.parse(text); } catch { return undefined; }
  if (!isObject(value) || !Number.isSafeInteger(value.revision) || (value.revision as number) < 0) return undefined;
  const base = value.base === undefined ? undefined : parseShared(value.base);
  if (value.base !== undefined && !base) return undefined;
  return { base, revision: value.revision as number, pending: readEntries(value.pending), local: readLocal(value.local) };
}

export function readPersisted(storage: Storage | undefined, key: WorkspaceKey, writer: string): Persisted | undefined {
  try { return parsePersisted(storage?.getItem(persistKey(key, writer)) ?? null); } catch { return undefined; }
}

export function writePersisted(storage: Storage | undefined, key: WorkspaceKey, writer: string, state: Persisted) {
  storage?.setItem(persistKey(key, writer), JSON.stringify({ ...state, at: Date.now() }));
}

type Locks = { request: (name: string, options: { ifAvailable?: boolean }, run: (lock: unknown) => Promise<void> | void) => Promise<void> };
const webLocks = (): Locks | undefined => (globalThis.navigator as { locks?: Locks } | undefined)?.locks;

/**
 * Holds this window's lock for as long as it lives. Resolves false when another live page already holds it (a
 * duplicated browser tab), true when held, and undefined where Web Locks do not exist.
 */
export async function holdWindow(writer: string, locks: Locks | null | undefined = webLocks()): Promise<{ held: boolean | undefined; release: () => void }> {
  if (!locks) return { held: undefined, release: () => undefined };
  let release!: () => void;
  const released = new Promise<void>(resolve => { release = resolve; });
  const held = await new Promise<boolean>(resolve => {
    void locks.request(lockName(writer), { ifAvailable: true }, lock => {
      if (!lock) { resolve(false); return; }
      resolve(true);
      return released;
    }).catch(() => resolve(false));
  });
  return { held, release };
}

/**
 * Takes over the unconfirmed changes other windows left for this place, when their windows are gone. Their copies
 * are removed only after the caller has saved the changes as its own (`commit`), so a crash in between loses
 * nothing: the copy is simply taken over again next time.
 */
export async function adoptOrphans(storage: Storage | undefined, key: WorkspaceKey, writer: string, locks: Locks | null | undefined = webLocks()): Promise<{ entries: Entry[]; commit: () => void; abandon: () => void }> {
  const none = { entries: [], commit: () => undefined, abandon: () => undefined };
  if (!storage || !locks) return none;
  const prefix = `${persistPrefix}:${key}:`;
  const names: string[] = [];
  try { for (let i = 0; i < storage.length; i++) { const name = storage.key(i); if (name?.startsWith(prefix) && name !== persistKey(key, writer)) names.push(name); } }
  catch { return none; }
  const entries: Entry[] = [];
  const taken: { name: string; release: () => void }[] = [];
  for (const name of names) {
    const other = name.slice(prefix.length);
    const saved = parsePersisted(storage.getItem(name));
    if (!saved?.pending.length) continue;
    const { held, release } = await holdWindow(other, locks);
    if (!held) continue;
    entries.push(...saved.pending);
    taken.push({ name, release });
  }
  return {
    entries,
    commit: () => {
      for (const { name, release } of taken) {
        // The base and focus of a window that is gone are no use to anyone; its changes now live in this window's copy.
        try { storage.removeItem(name); } catch { /* Left in place: it will be taken over again, and replays are idempotent. */ }
        release();
      }
    },
    /** Lets the copies go untouched (this window closed before it could take them over). */
    abandon: () => { for (const { release } of taken) release(); },
  };
}
