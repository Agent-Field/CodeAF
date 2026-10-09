import { connectEngine, EngineError, readTerminal, startTerminal, type TerminalInfo } from '../chat/engine-client';
import { newTab } from '../tabs/helpers';
import type { Tab } from '../tabs/types';
import { bind } from './bindings';
import { terminalView, type TerminalTarget } from './target';
import { limitSentence } from './state';

type Session = { id: string; sessionFile: string };

/** The engine session behind a conversation's session file. Shared, so a pane and its actions attach once. */
const sessions = new Map<string, Promise<Session>>();
export function sessionFor(sessionFile?: string): Promise<Session> {
  const known = sessionFile ? sessions.get(sessionFile) : undefined;
  if (known) return known;
  const attach = connectEngine(sessionFile).then(({ id, sessionFile: file }): Session => ({ id, sessionFile: file }));
  if (sessionFile) { sessions.set(sessionFile, attach); attach.catch(() => sessions.delete(sessionFile)); }
  return attach;
}

export type OpenTerminal = {
  /** The conversation the terminal runs under. Omitted: a new session, made on first use. */
  sessionFile?: string;
  /** Show a terminal or job the engine already runs (for example one the model started). */
  terminalId?: string;
  /** Starts a job running this command instead of an interactive shell. */
  command?: string;
  title?: string;
};

export const startSentence = (failure: unknown) => {
  if (!(failure instanceof Error) || !failure.message) return 'The terminal could not start.';
  return (failure instanceof EngineError && failure.status === 409 ? limitSentence(failure.message) : undefined) ?? failure.message;
};

/** Starts the terminal (or finds the one named) and binds it to the tab. Returns the title the tab should carry and the durable target to save on it. */
export async function startFor(paneId: string, options: OpenTerminal): Promise<{ title: string; target: TerminalTarget }> {
  const session = await sessionFor(options.sessionFile);
  const info: TerminalInfo = options.terminalId ? await readTerminal(session.id, options.terminalId) : await startTerminal(session.id, { command: options.command, title: options.title });
  bind(paneId, { sessionFile: session.sessionFile, terminalId: info.id });
  return { title: info.title, target: { sessionFile: session.sessionFile, terminalId: info.id } };
}

/**
 * The tab that shows an engine terminal, ready for `dispatch({ type: 'open', tab, background })`. It never
 * throws: when the engine refuses (Q6: sixteen live terminals) or cannot be reached, the tab opens holding the
 * one muted line (the engine's limit worded as the design words it), with "Try again".
 */
export async function openTerminalTab(options: OpenTerminal = {}): Promise<Tab> {
  const tab = newTab({ kind: 'terminal', title: options.title ?? options.command ?? 'Terminal', titleSource: 'manual' });
  try {
    const { title, target } = await startFor(tab.id, options);
    return { ...tab, ...terminalView(target), title: options.title ?? title };
  } catch (failure) {
    bind(tab.id, { sessionFile: options.sessionFile, refused: startSentence(failure) });
    return options.sessionFile ? { ...tab, sessionFile: options.sessionFile } : tab;
  }
}
