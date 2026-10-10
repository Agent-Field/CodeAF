/**
 * One pane's outbox while the engine is not answering (Interactions Flows, I-IFL-4).
 *
 * Pure on purpose: the hook decides when the engine is unreachable and performs
 * the posts. A send with files is not held — the caller keeps the draft and the
 * files, the same as a refused send. Plain text is held in the order it was
 * sent and leaves oldest first.
 */

export type OutboxMode = 'submit' | 'steer' | 'queue';

export type HeldSend = { text: string; mode: OutboxMode };

export type HoldResult = { held: readonly HeldSend[]; accepted: boolean };

/**
 * Hold one plain-text send at the end. Files and a blank message are refused
 * so the composer can keep them; the previous queue is returned unchanged.
 */
export function holdSend(held: readonly HeldSend[], send: { text: string; mode: OutboxMode; files?: readonly unknown[] }): HoldResult {
  if (send.files && send.files.length > 0) return { held, accepted: false };
  if (send.text.trim() === '') return { held, accepted: false };
  return { held: [...held, { text: send.text, mode: send.mode }], accepted: true };
}

/** The oldest held send, and the queue that remains after it. Empty when nothing is waiting. */
export function beginPost(held: readonly HeldSend[]): { held: readonly HeldSend[]; posting?: HeldSend } {
  if (held.length === 0) return { held };
  const [posting, ...rest] = held;
  return { held: rest, posting };
}

/** Put a send back at the front. A lost connection must not skip it or reverse the rest. */
export function restoreFront(held: readonly HeldSend[], send: HeldSend): readonly HeldSend[] {
  return [send, ...held];
}
