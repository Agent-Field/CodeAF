// What shape of answer a question wants, and the human words for its buttons.
// Pure: no React, so node --test can run it.

import type { EngineQuestion } from '../../chat/engine-client.ts';

export type Question = EngineQuestion;
export type Option = NonNullable<Question['options']>[number];
export type Form =
  | 'permission'
  | 'choice'
  | 'checklist'
  | 'blanks'
  | 'pairs'
  | 'dial'
  | 'text'
  | 'landing'
  | 'fuel'
  | 'proposal';

const INPUT_FORMS = new Set(['dial', 'pairs', 'checklist', 'blanks']);

export function formOf(question: Question): Form {
  if (question.kind === 'task') return 'proposal';
  if (question.kind === 'landing' || question.kind === 'conflict' || question.ask === 'landing') return 'landing';
  if (question.kind === 'fuel') return 'fuel';
  const input = question.input?.kind ?? '';
  if (INPUT_FORMS.has(input)) return input as Form;
  if (question.kind === 'consent' || question.ask === 'permission') return 'permission';
  return question.options?.length ? 'choice' : 'text';
}

export const isIrreversible = (question: Question) => question.stakes === 'irreversible';

/** Options as drawn: a wider permission is never offered for something that cannot be undone. */
export function visibleOptions(question: Question): Option[] {
  const options = question.options ?? [];
  return isIrreversible(question) ? options.filter((option) => !option.widening) : options;
}

/** The option that is only an answer once the person has written words. */
export function needsWords(question: Question, option: Option): boolean {
  const form = formOf(question);
  return (form === 'landing' && option.key === 's') || (form === 'fuel' && option.key === '1');
}

export const hasWordsField = (question: Question) =>
  question.input?.kind === 'text' && Boolean(question.options?.length);

const LANDING_LABELS: Record<string, string> = {
  a: 'Accept',
  n: 'Not right',
  s: 'Tell it…',
  r: 'Recheck',
  d: 'Let codeaf decide',
  u: 'Take it back',
};

const capital = (text: string) => text.charAt(0).toUpperCase() + text.slice(1);

// A phrase of plain words: its first word is lowercase letters only, and more words follow.
const PLAIN_PHRASE = /^[a-z]+\s+\S/;

/** Engine words are literal. Only a plain phrase ("not now") gains a capital to start a
 * button; a path, a file name or a lone word ("src/c.txt", "notes.md", "json") keeps its case. */
export const sentenceStart = (text: string) => (PLAIN_PHRASE.test(text) ? capital(text) : text);

export function optionLabel(question: Question, option: Option): string {
  const form = formOf(question);
  if (form === 'permission') {
    if (option.widening) return 'Always…';
    return option.safe ? 'Deny' : 'Allow once';
  }
  if (form === 'proposal') return option.safe ? 'No' : 'Start it';
  if (form === 'landing' && LANDING_LABELS[option.key]) return LANDING_LABELS[option.key];
  const dots = needsWords(question, option) && !option.label.endsWith('…') ? '…' : '';
  return sentenceStart(option.label) + dots;
}

export type Variant = 'primary' | 'raised' | 'quiet';

/** The Pick is the loud one; the Safe answer is always the quiet one. */
export function optionVariant(question: Question, option: Option): Variant {
  if (option.safe) return 'quiet';
  const pick = question.pick?.key;
  if (pick) return option.key === pick ? 'primary' : 'raised';
  const first = visibleOptions(question).find(
    (candidate) => !candidate.safe && !candidate.widening && !needsWords(question, candidate),
  );
  return first?.key === option.key ? 'primary' : 'raised';
}

const SCOPE_LABELS: Record<string, string> = {
  task: 'For this task',
  project: 'In this project',
  always: 'Everywhere, from now on',
};

export const scopeLabel = (scope: string) => SCOPE_LABELS[scope] ?? capital(scope);
export const widerScopes = (question: Question) => (question.scope ?? []).filter((scope) => scope !== 'once');

export function permissionRoles(question: Question) {
  const options = visibleOptions(question);
  return {
    allow: options.find((option) => !option.safe && !option.widening),
    always: options.find((option) => option.widening),
    deny: options.find((option) => option.safe),
  };
}

/** Options that carry the same comparison axes, as rows of a small table; null when they do not line up. */
export function compareRows(question: Question): string[][] | null {
  const options = question.options ?? [];
  if (options.length < 2) return null;
  const shared = Object.keys(options[0].dimensions ?? {});
  const axes = shared.filter((axis) => options.every((option) => option.dimensions?.[axis]));
  if (!axes.length) return null;
  return [
    ['', ...options.map((option) => sentenceStart(option.label))],
    ...axes.map((axis) => [axis, ...options.map((option) => option.dimensions?.[axis] ?? '')]),
  ];
}
