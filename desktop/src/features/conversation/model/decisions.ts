// What the person decided about a call, for the step row to say in a word ("allowed once").
// The engine's outcome carries the call it was about and the words of the choice; only
// choices with a known past tense are drawn, so an unfamiliar word stays unsaid (emptiness law).

import type { EngineSnapshot } from '../../chat/engine-client.ts';
import type { RichSnapshot } from './entry.ts';

const PAST: Record<string, string> = {
  'allow once': 'allowed once',
  always: 'always allowed',
  'always allow': 'always allowed',
  'allow always': 'always allowed',
  deny: 'denied',
};

const normal = (words: string) => words.trim().toLowerCase().replace(/[.…]+$/, '');

/** CallID -> the decision in the past tense, for every call a person decided on. */
export function decisionsOf(snapshot: EngineSnapshot): Map<string, string> {
  const decisions = new Map<string, string>();
  for (const outcome of (snapshot as RichSnapshot).recentOutcomes ?? []) {
    const word = outcome?.words ? PAST[normal(outcome.words)] : undefined;
    if (outcome?.outcome === 'decided' && outcome.callId && word) decisions.set(outcome.callId, word);
  }
  return decisions;
}
