// Question receipts: a quiet line in the flow where a question was asked.
// Waiting questions come from snapshot.questions, endings from recentOutcomes.

import type { EngineSnapshot } from '../../chat/engine-client.ts';
import type { TurnBlock, TurnV2 } from '../types.ts';
import { questionKey, questionsOf, type QuestionOutcome, type RichQuestion, type RichSnapshot } from './entry.ts';

type Receipt = Extract<TurnBlock, { kind: 'receipt' }>;
type Asked = { key: string; callId?: string; receipt: Omit<Receipt, 'id'> };

const withdrawnText = (reason?: string) => `No longer needed — ${reason || 'the turn moved on'}`;

function clock(iso?: string): string {
  const at = iso ? new Date(iso) : undefined;
  if (!at || Number.isNaN(at.getTime())) return '';
  return `${String(at.getHours()).padStart(2, '0')}:${String(at.getMinutes()).padStart(2, '0')}`;
}

function decidedText(o: QuestionOutcome): string {
  const who = o.decidedBy === 'person' ? 'you' : o.decidedBy;
  return [o.label || 'Answered', who, clock(o.at)].filter(Boolean).join(' · ');
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
  const key = questionKey(o);
  const decided = o.state === 'decided';
  const text = decided ? decidedText(o) : withdrawnText(o.reason);
  return { key, callId: o.callId, receipt: { kind: 'receipt', questionKey: key, state: o.state, text } };
}

function askedOf(snapshot: EngineSnapshot): Asked[] {
  const outcomes = ((snapshot as RichSnapshot).recentOutcomes ?? []).map(ended);
  const open = questionsOf(snapshot).map(waiting);
  const settled = new Set(outcomes.map((a) => a.key));
  return [...outcomes, ...open.filter((a) => !settled.has(a.key))];
}

function turnOf(turns: TurnV2[], callId?: string): TurnV2 | undefined {
  const owner = callId
    ? turns.find((t) => t.blocks.some((b) => b.kind === 'work' && b.steps.some((s) => s.calls.some((c) => c.callId === callId))))
    : undefined;
  return owner ?? turns[turns.length - 1];
}

/** Appends one receipt block per question to the turn that asked it. */
export function addReceipts(turns: TurnV2[], snapshot: EngineSnapshot): void {
  for (const asked of askedOf(snapshot)) {
    const turn = turnOf(turns, asked.callId);
    turn?.blocks.push({ ...asked.receipt, id: `${turn.id}:q:${asked.key}` });
  }
}
