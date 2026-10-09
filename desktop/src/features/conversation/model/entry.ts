// Engine record shapes the v2 projection reads beyond what engine-client.ts
// types today. Every field is optional: an older engine simply omits it.

import type { EngineEntry, EngineQuestion, EngineSnapshot } from '../../chat/engine-client.ts';

export type RecordedAttachment = { Path: string; Name?: string; MIME?: string; Kind?: string };

export type RichEntry = EngineEntry & {
  Took?: number; // ns; 0 or absent = unknown
  ImageRefs?: string[] | null;
  Attachments?: RecordedAttachment[] | null;
};

export type QuestionSubject = { kind?: string; id?: string; callId?: string; name?: string };

export type RichQuestion = EngineQuestion & {
  subject?: QuestionSubject | null;
  askedIn?: string; // the `ask` call that raised it, for a question about no call
  withdrawn?: { reason?: string; by?: string; at?: string } | null;
};

/** How a question ended, as the snapshot's `recentOutcomes` says it (session.QuestionOutcome). */
export type QuestionOutcome = {
  kind: string;
  token: string; // the question's ref, or its id as text
  head?: string;
  outcome: 'decided' | 'withdrawn';
  words?: string; // "Allow once", or "No longer needed — the turn moved on"
  by?: string; // person | dial | record | asker | window, or who withdrew it
  at?: string; // ISO time
  callId?: string; // the call the question was about; absent from older engines
};

export type RichSnapshot = EngineSnapshot & { recentOutcomes?: QuestionOutcome[] | null };

export const asRich = (entry: EngineEntry): RichEntry => entry as RichEntry;
export const questionsOf = (snapshot: EngineSnapshot): RichQuestion[] => (snapshot.questions ?? []) as RichQuestion[];

/** The stable key a receipt uses to find its question again. */
export function questionKey(q: { kind: string; id: number; ref?: string }): string {
  return `${q.kind}:${q.ref || q.id}`;
}

/** Only foreground questions can pause this conversation's tool rows. Older records without
 * scope metadata still associate by canonical call id; explicit task-only and withdrawn questions do not. */
export function waitingCalls(snapshot: EngineSnapshot): Set<string> {
  return new Set(questionsOf(snapshot).flatMap(q => {
    if (q.withdrawn || q.blocking?.turn === false) return [];
    if (q.blocking?.turn !== true && (q.blocking?.tasks?.length || q.asker?.kind === 'task')) return [];
    return q.subject?.callId ? [q.subject.callId] : [];
  }));
}
