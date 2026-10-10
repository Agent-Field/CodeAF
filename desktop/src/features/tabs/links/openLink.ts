// What opening a codeaf link does to a workspace. Pure apart from the injected engine reads, so node tests drive it.
//
// OPENING A LINK NEVER STARTS ANYTHING. It focuses the tab that already shows the target, or opens ONE tab on it; it
// never creates a conversation, never sends a message, never starts a terminal and never asks a model. Everything a
// link names is resolved through the engine's own records: the conversation id through the engine's history store
// (the transcript path comes from the engine, never from the link), a task through that conversation's plan, a
// terminal through the engine's live terminals. A target the engine no longer has is said, and nothing opens.
import { fileTab } from '../../files/fileTarget.ts';
import { terminalView } from '../../terminal/target.ts';
import { createId, newTab, panesOf } from '../helpers.ts';
import type { Tab } from '../types.ts';
import { linkOf, linkProblemSentence, parseDeepLink, targetOf, type LinkTarget } from './deepLinks.ts';

/** The engine reads a link needs. Each throws when the engine has no such thing (a 404) or cannot be reached. */
export type LinkEngine = {
  /** The conversation behind an id, from the engine's history store (GET /history/<id>). */
  chat(chatId: string): Promise<{ sessionFile: string; title: string; archived: boolean }>;
  /** Brings an archived conversation back, as Continue in History does. Best effort. */
  unarchive(chatId: string): Promise<void>;
  /** The title of a task in that conversation's plan. */
  task(sessionFile: string, taskId: string): Promise<{ title: string }>;
  /** A terminal the engine still keeps for that conversation. */
  terminal(sessionFile: string, terminalId: string): Promise<{ title: string }>;
};

/** What the workspace does with a link: select a pane that already shows it, open one tab, or say why not. */
export type LinkOutcome =
  | { kind: 'next-up'; link: string }
  | { kind: 'focus'; id: string; link: string }
  | { kind: 'open'; tab: Tab; link: string }
  | { kind: 'refused'; sentence: string; link?: string };

export const linkSentences = {
  unreachable: 'codeaf could not reach its engine, so the link did not open.',
  chatGone: 'That conversation is no longer in codeaf, so the link did not open.',
  taskGone: 'That task is no longer in its conversation, so the link did not open.',
  terminalGone: 'That terminal has ended and is no longer kept, so the link did not open.',
  failed: 'codeaf could not open that link.',
} as const;

/** The pane in these tabs that already shows the target. A conversation link is shown by any tab on that conversation. */
export function paneShowing(tabs: readonly Tab[], target: LinkTarget): string | undefined {
  if (target.kind === 'next-up') return undefined;
  const wanted = linkOf(target);
  for (const tab of tabs) {
    for (const pane of panesOf(tab)) {
      const shown = targetOf(pane);
      if (!shown || shown.kind === 'next-up' || shown.chatId !== target.chatId) continue;
      if (target.kind === 'chat' && !target.taskId ? pane.kind === 'conversation' && shown.kind === 'chat' : linkOf(shown) === wanted) return pane.id;
    }
  }
  return undefined;
}

const statusOf = (error: unknown) => (error && typeof error === 'object' && 'status' in error ? (error as { status: unknown }).status : undefined);
const unreachable = (error: unknown) => !!(error && typeof error === 'object' && 'unreachable' in error && (error as { unreachable: unknown }).unreachable);

function refusal(error: unknown, gone: string, link: string): LinkOutcome {
  if (unreachable(error)) return { kind: 'refused', sentence: linkSentences.unreachable, link };
  return { kind: 'refused', sentence: statusOf(error) === 404 ? gone : linkSentences.failed, link };
}

/**
 * Decides what one link does. `tabs` is read twice, before and after the engine answers, so a tab that appeared
 * while the engine was being asked (the same link twice, or the person opening it by hand) is focused, not doubled.
 */
export async function resolveLink(raw: unknown, tabs: () => readonly Tab[], engine: LinkEngine): Promise<LinkOutcome> {
  const parsed = parseDeepLink(raw);
  if (!parsed.ok) return { kind: 'refused', sentence: linkProblemSentence[parsed.reason] };
  const { target, canonical: link } = parsed;
  if (target.kind === 'next-up') return { kind: 'next-up', link };
  const held = paneShowing(tabs(), target);
  if (held) return { kind: 'focus', id: held, link };

  let chat: Awaited<ReturnType<LinkEngine['chat']>>;
  try { chat = await engine.chat(target.chatId); } catch (error) { return refusal(error, linkSentences.chatGone, link); }
  if (chat.archived) void engine.unarchive(target.chatId).catch(() => undefined);
  const { sessionFile } = chat;

  let tab: Tab;
  if (target.kind === 'chat' && target.taskId) {
    let title: string;
    try { ({ title } = await engine.task(sessionFile, target.taskId)); } catch (error) { return refusal(error, linkSentences.taskGone, link); }
    tab = newTab({ kind: 'task', title: title.trim() || 'Task', titleSource: 'manual', sessionFile, route: { taskId: target.taskId, back: [''], forward: [] } });
  } else if (target.kind === 'chat') {
    const title = chat.title.trim();
    tab = newTab({ kind: 'conversation', sessionFile, ...(title ? { title, titleSource: 'engine' as const } : {}) });
  } else if (target.kind === 'terminal') {
    let title: string;
    try { ({ title } = await engine.terminal(sessionFile, target.terminalId)); } catch (error) { return refusal(error, linkSentences.terminalGone, link); }
    tab = newTab({ kind: 'terminal', title: title.trim() || 'Terminal', titleSource: 'manual', ...terminalView({ sessionFile, terminalId: target.terminalId }) });
  } else {
    tab = fileTab({ sessionFile }, target.path, target.kind, createId());
  }

  const late = paneShowing(tabs(), target);
  return late ? { kind: 'focus', id: late, link } : { kind: 'open', tab, link };
}
