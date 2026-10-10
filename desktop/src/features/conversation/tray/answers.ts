// The canonical answer each form sends, exactly as the engine reads it.
// key is the option; picked repeats chosen keys; change is the person's words;
// blanks is label -> value; dial is a number; scope only rides a widening
// answer; decidedBy "asker" means "you decide: take the suggested one".

import type { EngineAnswer } from '../../chat/engine-client.ts';
import { formOf, needsWords, optionLabel, visibleOptions, type Option, type Question } from './form.ts';

export type Draft = {
  change: string;
  picked: string[];
  blanks: Record<string, string>;
  dial: number | null;
  scope: string;
};

const EITHER = 'either';

function blankDefault(form: string, blank: NonNullable<NonNullable<Question['input']>['blanks']>[number]) {
  if (form === 'pairs') return blank.default || EITHER;
  if (blank.kind === 'choice') return blank.default || blank.choices?.[0] || '';
  return blank.default ?? '';
}

export function initialDraft(question: Question): Draft {
  const form = formOf(question);
  const blanks = Object.fromEntries(
    (question.input?.blanks ?? []).map((blank) => [blank.label, blankDefault(form, blank)]),
  );
  const dial = question.input?.dial;
  return { change: '', picked: [], blanks, dial: dial ? dial.default : null, scope: 'once' };
}

export const pairsEither = EITHER;

function identity(question: Question, key: string): EngineAnswer {
  const answer: EngineAnswer = { kind: question.kind, id: question.id, key };
  if (question.ref) answer.ref = question.ref;
  return answer;
}

const nonEmpty = (blanks: Record<string, string>) =>
  Object.fromEntries(Object.entries(blanks).filter(([, value]) => value.trim() !== ''));

/** Pressing one of the question's own buttons. */
export function answerFor(question: Question, draft: Draft, option: Option): EngineAnswer {
  const form = formOf(question);
  const answer = identity(question, option.key);
  answer.picked = [option.key];
  const words = draft.change.trim();
  // A permission's words are the reason for saying no; they mean nothing on a yes.
  const wordsCount = form === 'permission' ? Boolean(option.safe) : true;
  if (words && wordsCount) answer.change = words;
  if (option.widening && draft.scope !== 'once') answer.scope = draft.scope;
  if (form === 'proposal' && !option.safe) {
    const blanks = nonEmpty(draft.blanks);
    if (Object.keys(blanks).length) answer.blanks = blanks;
  }
  return answer;
}

/** Whether pressing this option can send yet: words-first options wait for words. */
export function canPress(question: Question, draft: Draft, option: Option): boolean {
  return !needsWords(question, option) || draft.change.trim() !== '';
}

/** Whether the form's own Send button can send what is filled in so far. */
export function canSend(question: Question, draft: Draft): boolean {
  switch (formOf(question)) {
    case 'checklist':
      return draft.picked.length > 0;
    case 'dial':
      return draft.dial !== null;
    case 'text':
      return draft.change.trim() !== '';
    default:
      return true;
  }
}

/** The form's own Send: checklist, blanks, pairs, dial and free text. */
export function submitAnswer(question: Question, draft: Draft): EngineAnswer {
  const form = formOf(question);
  const keyed = visibleOptions(question).find((option) => !option.safe && !option.widening);
  const answer = identity(question, form === 'checklist' ? (draft.picked[0] ?? '') : (keyed?.key ?? ''));
  if (form === 'checklist') answer.picked = draft.picked;
  if (form === 'blanks' || form === 'pairs') answer.blanks = draft.blanks;
  if (form === 'dial' && draft.dial !== null) answer.dial = draft.dial;
  if (form === 'text') answer.change = draft.change.trim();
  return answer;
}

/** "You decide": the asker takes its own suggestion. Never offered for what cannot be undone.
 * A clarification has no option row; the pick key is the suggestion, so it still counts. */
export function decideAnswer(question: Question): EngineAnswer | null {
  if (question.stakes === 'irreversible') return null;
  const pick = question.pick?.key;
  if (!pick) return null;
  const offered = formOf(question) === 'text' || visibleOptions(question).some((option) => option.key === pick);
  if (!offered) return null;
  return { ...identity(question, pick), picked: [pick], decidedBy: 'asker' };
}

/** A set's "Allow all" / "Deny all": one answer per member, by role. */
export function bulkAnswers(members: Question[], role: 'allow' | 'deny'): EngineAnswer[] {
  return members.flatMap((member) => {
    const options = visibleOptions(member);
    const option = role === 'deny' ? options.find((o) => o.safe) : options.find((o) => !o.safe && !o.widening);
    return option ? [answerFor(member, initialDraft(member), option)] : [];
  });
}

/** One quiet phrase for what an answer says; the review list and the receipt use it. */
export function summarize(question: Question, answer: EngineAnswer): string {
  const form = formOf(question);
  if (form === 'text') return question.input?.secret ? 'Sent' : `“${answer.change ?? ''}”`;
  if (answer.dial !== undefined) return String(answer.dial);
  if (form === 'checklist') return labelsOf(question, answer.picked ?? []);
  if (answer.decidedBy === 'asker') return 'You decide';
  const option = (question.options ?? []).find((candidate) => candidate.key === answer.key);
  const label = option ? optionLabel(question, option).replace(/…$/, '') : '';
  const values = Object.values(answer.blanks ?? {}).filter(Boolean);
  return [label, ...values].filter(Boolean).join(' · ');
}

function labelsOf(question: Question, keys: string[]) {
  const options = question.options ?? [];
  return keys.map((key) => options.find((option) => option.key === key)?.label ?? key).join(', ');
}

export const togglePicked = (picked: string[], key: string) =>
  picked.includes(key) ? picked.filter((candidate) => candidate !== key) : [...picked, key];
