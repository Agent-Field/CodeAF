// Question receipts: a quiet line in the flow where a question was asked.
// Waiting questions come from snapshot.questions, endings from recentOutcomes.
// An outcome lands where this window saw its question wait (`places`), or else
// in the turn holding the call it names, which is how a reloaded window finds it.
// One with neither has no known place and is skipped.

import type { EngineSnapshot } from '../../chat/engine-client.ts';
import type { TurnBlock, TurnV2 } from '../types.ts';
import { questionKey, questionsOf, type QuestionOutcome, type RichQuestion, type RichSnapshot } from './entry.ts';

type Receipt = Extract<TurnBlock, { kind: 'receipt' }>;
type Asked = { key: string; callId?: string; receipt: Omit<Receipt, 'id'> };

/** Question key -> the id of the turn its receipt was drawn in. */
export type ReceiptPlaces = Map<string, string>;

const MOVED_ON = 'No longer needed — the turn moved on';
const withdrawnText = (reason?: string) => (reason ? `No longer needed — ${reason}` : MOVED_ON);

const DECIDERS: Record<string, string> = { person: 'you', window: 'you', asker: 'codeaf' };

function clock(iso?: string): string {
  const at = iso ? new Date(iso) : undefined;
  if (!at || Number.isNaN(at.getTime())) return '';
  return `${String(at.getHours()).padStart(2, '0')}:${String(at.getMinutes()).padStart(2, '0')}`;
}

function decidedText(o: QuestionOutcome): string {
  const who = o.by ? (DECIDERS[o.by] ?? o.by) : '';
  return [o.words || 'Answered', who, clock(o.at)].filter(Boolean).join(' · ');
}

function waiting(q: RichQuestion): Asked {
  const key = questionKey(q);
  const gone = q.withdrawn;
  const receipt: Omit<Receipt, 'id'> = gone
    ? { kind: 'receipt', questionKey: key, state: 'withdrawn', text: withdrawnText(gone.reason) }
    : { kind: 'receipt', questionKey: key, state: 'waiting', text: `Waiting on you: ${q.head}` };
  return { key, callId: q.subject?.callId, receipt };
}

function ended(o: QuestionOutcome): Asked {
  const key = `${o.kind}:${o.token}`;
  const decided = o.outcome === 'decided';
  const text = decided ? decidedText(o) : o.words || MOVED_ON;
  return { key, callId: o.callId || undefined, receipt: { kind: 'receipt', questionKey: key, state: decided ? 'decided' : 'withdrawn', text } };
}

function outcomesOf(snapshot: EngineSnapshot): Asked[] {
  const list = (snapshot as RichSnapshot).recentOutcomes;
  return Array.isArray(list) ? list.filter((o) => o && o.kind && o.token).map(ended) : [];
}

function callTurn(turns: TurnV2[], callId?: string): TurnV2 | undefined {
  if (!callId) return undefined;
  return turns.find((t) => t.blocks.some((b) => b.kind === 'work' && b.steps.some((s) => s.calls.some((c) => c.callId === callId))));
}

function placeOpen(turns: TurnV2[], asked: Asked, places: ReceiptPlaces): TurnV2 | undefined {
  const known = turns.find((t) => t.id === places.get(asked.key));
  const turn = known ?? callTurn(turns, asked.callId) ?? turns[turns.length - 1];
  if (turn && !places.has(asked.key)) places.set(asked.key, turn.id);
  return turn;
}

/** Appends one receipt block per question to the turn that asked it. */
export function addReceipts(turns: TurnV2[], snapshot: EngineSnapshot, places: ReceiptPlaces = new Map()): void {
  const outcomes = outcomesOf(snapshot);
  const settled = new Set(outcomes.map((a) => a.key));
  const open = questionsOf(snapshot).map(waiting).filter((a) => !settled.has(a.key));
  for (const asked of open) {
    const turn = placeOpen(turns, asked, places);
    turn?.blocks.push({ ...asked.receipt, id: `${turn.id}:q:${asked.key}` });
  }
  for (const asked of outcomes) {
    // The call is the same anchor the waiting line used, so live and replay agree.
    const turn = turns.find((t) => t.id === places.get(asked.key)) ?? callTurn(turns, asked.callId);
    turn?.blocks.push({ ...asked.receipt, id: `${turn.id}:q:${asked.key}` });
  }
}
