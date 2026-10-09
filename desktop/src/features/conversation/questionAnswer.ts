// The canonical answer a question card sends. Same semantics EngineDecision
// had: the option key is the answer, `picked` repeats the chosen keys, `change`
// carries the person's words, `blanks` and `scope` only when the form asks.

import type { EngineAnswer, EngineQuestion } from '../chat/engine-client.ts';

export type QuestionDraft = { change: string; picked: string[]; blanks: Record<string, string>; scope: string };

const DRAWABLE_BLOCKS = new Set(['text', 'diagram', 'diff', 'layout', 'table']);
const UNDRAWABLE_INPUTS = new Set(['pairs', 'dial']);

export const isChecklist = (question: EngineQuestion) => question.input?.kind === 'checklist';
export const hasBlanks = (question: EngineQuestion) => question.input?.kind === 'blanks';

export function initialDraft(question: EngineQuestion): QuestionDraft {
  const blanks = Object.fromEntries((question.input?.blanks ?? []).map((blank) => [blank.label, blank.default ?? '']));
  return { change: '', picked: [], blanks, scope: 'once' };
}

/** A form this surface cannot draw honestly is sent to the terminal instead of guessed at. */
export function isUndrawable(question: EngineQuestion): boolean {
  if (UNDRAWABLE_INPUTS.has(question.input?.kind ?? '')) return true;
  const blocks = [...(question.attach ?? []), ...(question.options ?? []).flatMap((option) => option.blocks ?? [])];
  return blocks.some((block) => !DRAWABLE_BLOCKS.has(block.kind));
}

export function buildAnswer(question: EngineQuestion, key: string, draft: QuestionDraft): EngineAnswer {
  const keys = isChecklist(question) ? draft.picked : key ? [key] : [];
  const answer: EngineAnswer = { kind: question.kind, id: question.id, key: keys[0] ?? '' };
  const change = draft.change.trim();
  if (question.ref) answer.ref = question.ref;
  if (keys.length) answer.picked = keys;
  if (change) answer.change = change;
  if (hasBlanks(question)) answer.blanks = draft.blanks;
  if (draft.scope && draft.scope !== 'once') answer.scope = draft.scope;
  return answer;
}

/** Whether the free-standing Submit can send what is filled in so far. */
export function canSubmit(question: EngineQuestion, draft: QuestionDraft): boolean {
  if (isChecklist(question)) return draft.picked.length > 0;
  return hasBlanks(question) || draft.change.trim() !== '';
}

export function togglePicked(picked: string[], key: string): string[] {
  return picked.includes(key) ? picked.filter((candidate) => candidate !== key) : [...picked, key];
}
