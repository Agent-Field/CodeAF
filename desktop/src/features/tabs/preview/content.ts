// What a preview says, as pure functions: the lines of a field, the question a card asks, the state words.
// No React, so node --test can run it.
import type { EngineFileDiff, EngineQuestion } from '../../chat/engine-client.ts';
import { formOf } from '../../conversation/tray/form.ts';
import type { TabSummary } from '../../conversation/tabSummary.ts';

export type FieldLine = { text: string; tone?: 'ink' | 'add' | 'del' };

const FIELD_LINES = 3;
// Colour and cursor sequences, OSC titles and stray control bytes: a preview shows words, not escapes.
const ESCAPES = /\u001b\[[0-9;?]*[ -/]*[@-~]|\u001b\][^\u0007\u001b]*(?:\u0007|\u001b\\)|\u001b[@-Z\\-_]|[\u0000-\u0008\u000b\u000c\u000e-\u001a]/g;

/** The last non-empty lines of terminal output; the newest is drawn in full ink (design 3k). */
export function tailLines(output: string, count = FIELD_LINES): FieldLine[] {
  const lines = output.replace(ESCAPES, '').split('\n').map(line => line.slice(line.lastIndexOf('\r') + 1).trimEnd()).filter(line => line.trim());
  return lines.slice(-count).map((text, index, shown) => (index === shown.length - 1 ? { text, tone: 'ink' as const } : { text }));
}

/** The first lines of a file as written. */
export function headLines(text: string, count = FIELD_LINES): FieldLine[] {
  return text.split('\n').filter(line => line.trim()).slice(0, count).map(line => ({ text: line.trimEnd() }));
}

/** The first changed lines of a diff: removed in red with a minus sign, added in green with a plus. */
export function changedLines(diff: Pick<EngineFileDiff, 'hunks'>, count = 2): FieldLine[] {
  const changed = diff.hunks.flatMap(hunk => hunk.lines).filter(line => line.kind !== 'context');
  return changed.slice(0, count).map(line => (line.kind === 'del' ? { text: `− ${line.text.trim()}`, tone: 'del' as const } : { text: `+ ${line.text.trim()}`, tone: 'add' as const }));
}

/** What the card asks. A set of permissions reads as the tray's batch card does ("Allow 3 actions?"). */
export type Ask = { text: string; /** True when every question is a permission, so "Allow all" is honest. */ permissions: boolean; count: number };

/**
 * The questions a card is entitled to show. A conversation owns every open question; a task tab owns only those that
 * name its task in `blocking.tasks` (the engine's own link, as the expanded tasks view reads it), so the conversation's
 * other questions never appear on, or get answered from, a task's card.
 */
export function questionsFor(summary: Pick<TabSummary, 'questions'> | undefined, taskId?: string): EngineQuestion[] {
  const questions = summary?.questions ?? [];
  return taskId === undefined ? questions : questions.filter(question => question.blocking?.tasks?.includes(taskId));
}

export function askOf(questions: readonly EngineQuestion[] | undefined): Ask | undefined {
  if (!questions?.length) return undefined;
  const permissions = questions.every(question => formOf(question) === 'permission');
  const [first] = questions;
  const text = permissions && questions.length > 1 ? `Allow ${questions.length} actions?` : questions.length > 1 ? `${first.head} and ${questions.length - 1} more` : first.head;
  return { text, permissions, count: questions.length };
}

export type CardState = { lead?: 'amber' | 'danger'; dot?: 'accent'; words?: string };

/** The header state of a conversation or task card: needs you, failed, "4 running", working, or nothing. */
export function stateOf(summary: TabSummary | undefined, taskId?: string): CardState {
  if (!summary) return {};
  if (taskId) {
    // A task's own state: it needs you only when a question names it, and the conversation's failure is not its failure.
    if (questionsFor(summary, taskId).length) return { lead: 'amber', words: 'Needs you' };
    const word = summary.taskState?.[taskId];
    return word ? { words: word } : {};
  }
  if (summary.mark === 'waiting') return { lead: 'amber', words: 'Needs you' };
  if (summary.mark === 'failed') return { lead: 'danger', words: 'Failed' };
  if (summary.running) return { dot: 'accent', words: `${summary.running} running` };
  if (summary.mark === 'working') return { dot: 'accent', words: 'Working' };
  return {};
}
