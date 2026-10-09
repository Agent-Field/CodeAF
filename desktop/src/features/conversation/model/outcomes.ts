// Which ended questions earn a receipt in the flow. Pure: no React.
//
// A task proposal that went away (it started on its own when its clock ran
// out, or the work stopped waiting on it) leaves no receipt: the task's own
// notice and the task panel already say what became of it, by name. Drawn as
// a receipt it was a nameless "No longer needed" row, one per proposal, that
// only a window which had watched the card wait could place, so it showed live
// and vanished on reload. Each question is drawn once.

import type { EngineSnapshot } from '../../chat/engine-client.ts';
import type { QuestionOutcome, RichSnapshot } from './entry.ts';

const keyOf = (o: QuestionOutcome) => `${o.kind}:${o.token}`;

/** True when the outcome is only a proposal the task panel already accounts for. */
const isRetiredProposal = (o: QuestionOutcome) => o.kind === 'task' && o.outcome === 'withdrawn';

/** The snapshot with its outcomes cut to the ones a receipt says something new about. */
export function receiptOutcomes(snapshot: EngineSnapshot): EngineSnapshot {
  const list = (snapshot as RichSnapshot).recentOutcomes;
  if (!Array.isArray(list)) return snapshot;
  const seen = new Set<string>();
  const drawn = list.filter((o) => {
    if (!o || isRetiredProposal(o) || seen.has(keyOf(o))) return false;
    seen.add(keyOf(o));
    return true;
  });
  return { ...snapshot, recentOutcomes: drawn } as RichSnapshot;
}
