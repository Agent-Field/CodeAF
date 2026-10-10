// Next up for one window: the world feed arranged by nextUpQueue, plus Accept and Skip.
//
// Accept does not call the engine immediately. The shared toast counts the six
// seconds (and pauses while the pointer or focus is on it, so Undo stays
// reachable). Undo takes the whole batch back and nothing is posted. When the
// toast settles, each acceptable question is answered with its suggestion key.
// A refusal after that is one muted line, not a toast per question.
//
// Skip lives on this controller only. Another window has its own controller, so
// its walk order does not follow this one, and the engine is never told.

import { useCallback, useSyncExternalStore } from 'react';
import design from '../../design/tokens.json' with { type: 'json' };
import { toasts, type Toasts } from '../../design/toasts.ts';
import { EngineError, fetchEngine, type EngineAnswer } from '../chat/engine-client.ts';
import type { AttentionItem } from '../chat/world-client.ts';
import { worldStore } from '../chat/world-store.ts';
import { nextUpQueue, type NextUpQueue } from './model.ts';

/** The shared toast window (`interaction.toastDuration`). Accept commits when this toast settles. */
export const ACCEPT_WINDOW_MS = design.interaction.toastDuration;

const ACCEPT_TOAST_KEY = 'nextup-accept';

type WorldSource = {
  getState: () => { items: readonly AttentionItem[] };
  subscribe: (listener: () => void) => () => void;
};

type Poster = (path: string, init: { method: 'POST'; body: string }) => Promise<Response>;

export type NextUpView = {
  queue: NextUpQueue;
  /** Question keys sent to the back, earliest first. This window only. */
  skipped: readonly string[];
  /** True from Accept until Undo or until the toast settles and the posts start. */
  pendingAccept: boolean;
  /**
   * The questions that did not land, in one line. Empty when there is nothing
   * to say (nothing has been committed, or every answer landed).
   */
  failureLine: string;
};

/** "Accepted 1 suggestion" / "Accepted 3 suggestions". One matches the Accept button. */
export function acceptedSuggestionsText(count: number): string {
  return `Accepted ${count} ${count === 1 ? 'suggestion' : 'suggestions'}`;
}

/**
 * One muted line naming the questions that did not land. Empty when there are
 * none, so a surface draws nothing rather than a sentence about zero.
 */
export function acceptFailureLine(items: readonly { text: string }[]): string {
  const names = items.map(item => item.text.trim()).filter(name => name !== '');
  if (names.length === 0) return items.length === 0 ? '' : 'Could not accept.';
  return `Could not accept: ${names.join(' · ')}`;
}

/** The answer body: the suggestion's own key, on the question the feed named. */
export function suggestionAnswer(item: AttentionItem): EngineAnswer | undefined {
  const key = item.suggestion?.key;
  if (typeof key !== 'string' || key === '') return undefined;
  // A feed that did not number the question is still posted. The engine refuses
  // an id that matches nothing, and that refusal joins the failure line.
  const id = typeof item.id === 'number' && Number.isSafeInteger(item.id) && item.id >= 0 ? item.id : 0;
  return { kind: item.kind, id, key, picked: [key], decidedBy: 'person' };
}

/** Where one suggestion answer is posted. The session id is a path segment, so it is encoded. */
export function suggestionAnswerPath(session: string): string {
  return `/sessions/${encodeURIComponent(session)}/answer`;
}

/**
 * Posts one suggestion answer and stops at the engine's accept flag. It does not
 * re-read the conversation: Accept may cover several sessions, and the world
 * feed is what drops a question once it has been answered.
 */
export async function answerSuggestion(session: string, answer: EngineAnswer, post: Poster = fetchEngine): Promise<void> {
  const response = await post(suggestionAnswerPath(session), { method: 'POST', body: JSON.stringify(answer) });
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: unknown } | null;
    const named = typeof body?.error === 'string' ? body.error : '';
    throw new EngineError(named || `The engine request failed (${response.status}).`, response.status);
  }
  const result: unknown = await response.json().catch(() => null);
  if (!result || typeof result !== 'object' || !('accepted' in result) || result.accepted !== true) {
    throw new EngineError('The engine did not accept this action.');
  }
}

export type NextUpOptions = {
  world?: WorldSource;
  /** Posts one answer. Tests pass a fake; the window uses the session answer endpoint. */
  answer?: (session: string, answer: EngineAnswer) => Promise<void>;
  channel?: Toasts;
};

export function createNextUp(options: NextUpOptions = {}) {
  const world = options.world ?? worldStore;
  const answer = options.answer ?? answerSuggestion;
  const channel = options.channel ?? toasts;
  let skipped: readonly string[] = [];
  let failureLine = '';
  let pendingId = 0;
  let nextId = 0;
  let worldVersion = 0;
  // One cached view per conversation. Two callers (the pill and the walk) must
  // not invalidate each other, or React treats a stable queue as a new snapshot.
  const caches = new Map<string, { sig: string; view: NextUpView }>();
  const listeners = new Set<() => void>();
  let unworld: (() => void) | undefined;
  let commitSeq = 0;

  const emit = () => { caches.clear(); for (const listener of [...listeners]) listener(); };

  const items = () => {
    const list = world.getState().items;
    return Array.isArray(list) ? list : [];
  };

  function onWorld() {
    const live = new Set<string>();
    for (const item of items()) if (item && typeof item.key === 'string' && item.key !== '') live.add(item.key);
    // A question that left the feed is not waiting, so it is not skipped either.
    if (skipped.some(key => !live.has(key))) skipped = skipped.filter(key => live.has(key));
    worldVersion++;
    emit();
  }

  function view(conversationKey: string): NextUpView {
    const shown = typeof conversationKey === 'string' ? conversationKey : '';
    const sig = `${worldVersion}\0${skipped.join('\0')}\0${pendingId}\0${failureLine}`;
    const hit = caches.get(shown);
    if (hit?.sig === sig) return hit.view;
    const view: NextUpView = {
      queue: nextUpQueue({ items: items(), conversationKey: shown, skipped }),
      skipped,
      pendingAccept: pendingId !== 0,
      failureLine,
    };
    caches.set(shown, { sig, view });
    return view;
  }

  function skip(conversationKey: string) {
    const front = view(conversationKey).queue.items[0];
    if (!front) return;
    skipped = skipped.filter(key => key !== front.key).concat(front.key);
    emit();
  }

  /**
   * The batch is the acceptable set at the click. The toast already named that
   * count, so a later skip or a feed change does not add or drop one of them.
   */
  function accept(conversationKey: string) {
    const batch = view(conversationKey).queue.acceptable.slice();
    if (batch.length === 0) return;
    const mine = ++nextId;
    pendingId = mine;
    failureLine = '';
    let open = true;
    const close = () => {
      if (!open || pendingId !== mine) return false;
      open = false;
      pendingId = 0;
      return true;
    };
    channel.show({
      key: ACCEPT_TOAST_KEY,
      message: [acceptedSuggestionsText(batch.length)],
      durationMs: ACCEPT_WINDOW_MS,
      undo() { if (close()) emit(); },
      onSettled() {
        // Replacing this toast (a second Accept) settles it without being Undo.
        // Only the batch whose id is still pending may be posted.
        if (!close()) return;
        emit();
        void commit(batch);
      },
    });
    emit();
  }

  async function commit(batch: readonly AttentionItem[]) {
    const seq = ++commitSeq;
    const settled = await Promise.all(batch.map(async item => {
      const body = suggestionAnswer(item);
      if (!body || item.session === '') return item;
      try {
        await answer(item.session, body);
        return undefined;
      } catch {
        return item;
      }
    }));
    // A newer commit owns the line. This one still ran; its result is stale.
    if (seq !== commitSeq) return;
    const failed = settled.filter((item): item is AttentionItem => item !== undefined);
    failureLine = acceptFailureLine(failed);
    emit();
    // Info, not danger: the line is muted. The dot stays the toast's own mark.
    if (failureLine) channel.show({ message: [failureLine], durationMs: ACCEPT_WINDOW_MS });
  }

  return {
    view,
    accept,
    skip,
    subscribe(listener: () => void) {
      listeners.add(listener);
      if (listeners.size === 1) unworld = world.subscribe(onWorld);
      let done = false;
      return () => {
        if (done) return;
        done = true;
        listeners.delete(listener);
        if (listeners.size === 0) { unworld?.(); unworld = undefined; }
      };
    },
  };
}

export type NextUp = ReturnType<typeof createNextUp>;

/** The window's Next up. Skip and the pending Accept belong to this window alone. */
export const nextUp = createNextUp();

/**
 * The queue for the conversation on screen, and Accept / Skip.
 * `conversationKey` is that conversation's session id; empty counts every one.
 */
export function useNextUp(conversationKey = '', controller: NextUp = nextUp) {
  const snapshot = () => controller.view(conversationKey);
  const view = useSyncExternalStore(controller.subscribe, snapshot, snapshot);
  const accept = useCallback(() => { controller.accept(conversationKey); }, [controller, conversationKey]);
  const skip = useCallback(() => { controller.skip(conversationKey); }, [controller, conversationKey]);
  return { ...view.queue, skipped: view.skipped, pendingAccept: view.pendingAccept, failureLine: view.failureLine, accept, skip };
}
