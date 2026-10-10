// The worker's steps and the notes around them, shaped for the task view.
// Steps are always shell commands; the log draws them as a terminal would.

import { displayCommand } from './displayCommand.ts';
import { parseTime, spanText } from './taskClock.ts';
import type { Landing, LogLine, NoteLine, PageNote, PageStep, TaskPage } from './taskTypes.ts';

const NANOS_PER_MS = 1_000_000;
const SUBSECOND_LIMIT_MS = 10_000;

/** "0.1s", "3.1s", "12s", "1m 4s"; empty when the engine did not record it. */
export function tookText(nanos?: number): string {
  if (!nanos || nanos <= 0) return '';
  const ms = nanos / NANOS_PER_MS;
  return ms < SUBSECOND_LIMIT_MS ? `${(ms / 1000).toFixed(1)}s` : spanText(ms);
}

/** A not-run step without a refusal is the harness correcting a reply's form: no step a person reads. */
const isReadable = (step: PageStep): boolean => !step.not_run || Boolean(step.refused);

function stateOf(step: PageStep): LogLine['state'] {
  if (step.refused) return 'refused';
  return step.exit ? 'failed' : 'done';
}

function lineOf(step: PageStep, index: number): LogLine {
  return {
    id: `step-${step.step ?? index}`,
    command: displayCommand(step.command ?? '', step.parts),
    state: stateOf(step),
    observation: step.observation ?? '',
    fullOutput: step.full_output || undefined,
    took: tookText(step.took),
  };
}

export function stepLines(page: TaskPage): LogLine[] {
  return (page.Steps ?? []).filter(isReadable).map(lineOf);
}

export function liveLine(page: TaskPage): LogLine | undefined {
  const live = page.Live;
  if (!live?.Command) return undefined;
  return {
    id: `step-live-${live.Step ?? 0}`,
    command: displayCommand(live.Command, page.Row.LiveParts),
    state: 'running',
    observation: '',
    took: '',
    since: live.Since,
  };
}

export const refusedCount = (lines: readonly LogLine[]): number => lines.filter((line) => line.state === 'refused').length;

const plural = (count: number, word: string): string => `${count} ${word}${count === 1 ? '' : 's'}`;

/** "Ran 14 commands · 1 refused" once finished; "Working · 14 commands" while it runs. Empty with nothing to show. */
export function logSummary(lines: readonly LogLine[], live: LogLine | undefined, ended: boolean): string {
  const count = lines.length + (live ? 1 : 0);
  if (count === 0) return '';
  const refused = refusedCount(lines);
  const tail = refused ? ` · ${refused} refused` : '';
  return (ended ? `Ran ${plural(count, 'command')}` : `Working · ${plural(count, 'command')}`) + tail;
}

function authorWord(author?: string): string {
  const word = (author ?? '').trim();
  if (word.toLowerCase() === 'chat') return 'Conversation';
  // The engine records worker identities, but the task page names their voice for the person.
  return 'Note from worker';
}

// The engine's landing sentence, `landed on <branch>: <n> file(s)` (beltLandingLine in
// internal/session/task_run_belt.go), joined to the rest of the run's outcome with " · ".
const LANDING = /^landed on (\S+): (\d+ files?)$/;
const OUTCOME_JOIN = ' · ';

/** Splits the landing part out of an outcome note; the rest stays as the note's words. */
export function splitLanding(body: string): { rest: string; landing?: Landing } {
  const parts = body.split(OUTCOME_JOIN);
  const at = parts.findIndex((part) => LANDING.test(part.trim()));
  if (at < 0) return { rest: body };
  const [, branch, files] = LANDING.exec(parts[at].trim()) as RegExpExecArray;
  return { rest: parts.filter((_, index) => index !== at).join(OUTCOME_JOIN).trim(), landing: { branch, files } };
}

/** A person's note is read once a step starts after it was written. */
function receiptFor(note: PageNote, page: TaskPage, running: boolean): string {
  if (!note.Person || !running) return '';
  const readAt = parseTime(page.Live?.Since);
  return readAt > parseTime(note.At) ? 'Read' : 'Delivered at its next step';
}

/** The engine also records the result as a note; drawing both says everything twice. */
function repeatsResult(body: string, result: string): boolean {
  return Boolean(result) && body.includes(result);
}

export function noteLines(page: TaskPage, running: boolean): NoteLine[] {
  const result = (page.Result ?? '').trim();
  return (page.Notes ?? [])
    .filter((note) => note.Body?.trim() && !repeatsResult(note.Body, result))
    .map((note, index) => {
      const { rest, landing } = note.Person ? { rest: note.Body, landing: undefined } : splitLanding(note.Body);
      return {
        id: `note-${index}`,
        person: Boolean(note.Person),
        // A landing is the task's own result, not a voice: it needs no author.
        author: landing && !rest ? '' : authorWord(note.Author),
        body: rest,
        landing,
        receipt: receiptFor(note, page, running),
      };
    });
}
