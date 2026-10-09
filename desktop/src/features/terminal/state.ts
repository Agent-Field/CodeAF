// What the header says about a terminal, as pure functions of the engine's record.
import type { TerminalInfo } from '../chat/engine-client';
import type { StateTone } from './TerminalHeader';

type Described = Pick<TerminalInfo, 'cwd' | 'kind'>;
type Stated = Pick<TerminalInfo, 'state' | 'exitCode'>;

/** "/Users/ana/codeaf" is "~/codeaf": the home folder is not news to the person who owns it. */
export const homeShort = (path: string) => path.replace(/^\/(?:Users|home)\/[^/]+(?=\/|$)/, '~');

/** "~/codeaf · job": where it runs, and what it is. */
export const metaLine = (info: Described) => `${homeShort(info.cwd)} · ${info.kind}`;

/** The colour of the state glyph: accent while it runs, quiet when it ended well, red when it failed. */
export function toneOf(info: Stated): StateTone {
  if (info.state === 'running') return 'running';
  if (info.state === 'closed') return 'stopped';
  return (info.exitCode ?? 0) === 0 ? 'done' : 'failed';
}

/** The menu's removal row (Q4: a finished job stays listed until it is removed here). */
export const removeLabel = (kind: TerminalInfo['kind']) => (kind === 'job' ? 'Remove job' : 'Remove terminal');

/** The engine's 409 on start is its limit ("16 terminals are already running; close one first"); the design words it for a person (Components, edge states). */
export function limitSentence(message: string): string | undefined {
  const count = /^(\d+) terminals are already running/.exec(message)?.[1];
  return count ? `${count} terminals are open in this conversation. Close one to start another.` : undefined;
}
