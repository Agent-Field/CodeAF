// Which tabs the tray draws: order, sets (same Batch), the all-permission
// collapse, and what "Later" has folded away. Pure view logic.

import { formOf, isIrreversible, type Question } from './form.ts';

export type Tab =
  | { id: string; kind: 'question'; label: string; question: Question }
  | { id: string; kind: 'bulk'; label: string; members: Question[]; batch: string }
  | { id: string; kind: 'review'; label: string; members: Question[]; batch: string };

export type TrayLayout = { tabs: Tab[]; folded: Question[] };

export const questionKey = (question: Question) => `${question.kind}:${question.ref || question.id}`;

/** Still standing questions: deeper clarifications first, then oldest. */
export function orderQuestions(questions: Question[]): Question[] {
  return questions
    .filter((question) => !question.withdrawn)
    .map((question, index) => ({ question, index }))
    .sort((a, b) => {
      const depth = (b.question.clarificationDepth ?? 0) - (a.question.clarificationDepth ?? 0);
      if (depth) return depth;
      const asked = (a.question.asked ?? '').localeCompare(b.question.asked ?? '');
      return asked || a.index - b.index;
    })
    .map((entry) => entry.question);
}

type Group = { batch: string; members: Question[] };

/** Sets by Batch, in order of first member; an irreversible question never joins one. */
function sets(ordered: Question[]): Map<string, Question[]> {
  const byBatch = new Map<string, Question[]>();
  for (const question of ordered) {
    if (!question.batch || isIrreversible(question)) continue;
    byBatch.set(question.batch, [...(byBatch.get(question.batch) ?? []), question]);
  }
  for (const [batch, members] of byBatch) if (members.length < 2) byBatch.delete(batch);
  return byBatch;
}

export const isPermissionSet = (members: Question[]) => members.every((member) => formOf(member) === 'permission');

function setTabs({ batch, members }: Group, expanded: Set<string>): Tab[] {
  if (isPermissionSet(members) && !expanded.has(batch)) {
    return [{ id: `bulk:${batch}`, kind: 'bulk', label: `${members.length} actions`, members, batch }];
  }
  const tabs: Tab[] = members.map((question) => ({ id: questionKey(question), kind: 'question', label: question.head, question }));
  return [...tabs, { id: `review:${batch}`, kind: 'review', label: 'Review', members, batch }];
}

export function layoutTabs(questions: Question[], expanded: Set<string>, later: Set<string>): TrayLayout {
  const ordered = orderQuestions(questions);
  const folded = ordered.filter((question) => later.has(questionKey(question)));
  const shown = ordered.filter((question) => !later.has(questionKey(question)));
  const grouped = sets(shown);
  const done = new Set<string>();
  const tabs = shown.flatMap((question): Tab[] => {
    const members = question.batch ? grouped.get(question.batch) : undefined;
    if (!members) return [{ id: questionKey(question), kind: 'question', label: question.head, question }];
    if (done.has(question.batch!)) return [];
    done.add(question.batch!);
    return setTabs({ batch: question.batch!, members }, expanded);
  });
  return { tabs, folded };
}

/** True only when the turn itself is stopped on a question; landings and fuel never block. */
export function blocksComposer(questions: Question[]): boolean {
  return questions.some((question) => !question.withdrawn && question.blocking?.turn === true);
}

export type Receipt = { state: 'waiting' | 'answered' | 'withdrawn'; text: string };

/** The line left in the conversation flow at the point a question was asked. */
export function waitingReceipt(question: Question): Receipt {
  return { state: 'waiting', text: `Waiting on you: ${question.head}` };
}

export function withdrawnReceipt(): Receipt {
  return { state: 'withdrawn', text: 'No longer needed. The turn moved on.' };
}
