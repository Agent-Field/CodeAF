// Clocks: only a task proposal (starts by itself) and a recommend-then-auto
// ask (takes the suggested answer) ever run one. Irreversible questions never do.

import type { Question } from './form.ts';

const ZERO_TIME = /^0001-/;

function parse(text: string | undefined): number | null {
  if (!text || ZERO_TIME.test(text)) return null;
  const ms = Date.parse(text);
  return Number.isNaN(ms) ? null : ms;
}

/** When the clock fires, in epoch milliseconds, or null when this question has none. */
export function deadlineAt(question: Question): number | null {
  if (question.stakes === 'irreversible') return null;
  const direct = parse(question.deadline);
  if (direct !== null) return direct;
  const policy = question.policy;
  const asked = parse(question.asked);
  if (policy?.kind !== 'recommend-then-auto' || !policy.after || asked === null) return null;
  return asked + policy.after / 1e6; // a Go duration arrives as nanoseconds
}

export function secondsLeft(question: Question, now: number): number | null {
  const at = deadlineAt(question);
  return at === null ? null : Math.max(0, Math.ceil((at - now) / 1000));
}

export function span(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, '0')}s`;
}

export function clockText(question: Question, now: number): string | null {
  const seconds = secondsLeft(question, now);
  if (seconds === null) return null;
  const lead = question.kind === 'task' ? 'Starts in' : 'Takes the suggested answer in';
  return `${lead} ${span(seconds)}`;
}
