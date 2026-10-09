// What a person reads for a worker's shell command. The engine records every
// command whole, and marks the parts that are machinery: the `cd <run copy>`
// the harness prepends and the plandb record shims it appends. Those are cut.

import type { PlanCommandPart } from '../../chat/engine-client.ts';

export type CommandPart = PlanCommandPart;

const ELLIPSIS = '…';

const isMachinery = (part: CommandPart): boolean => Boolean(part.RecordAddressed || part.RunCopyPrefix);

function textOf(command: string, part: CommandPart): string {
  const { Start: start, End: end } = part;
  const spans = Number.isInteger(start) && Number.isInteger(end) && (start as number) <= (end as number) && (end as number) <= command.length;
  return (spans ? command.slice(start, end) : (part.Command ?? '')).trim();
}

function glue(separator = ''): string {
  if (separator === '' || separator === '\n') return separator || ' ';
  return separator === ';' ? '; ' : ` ${separator} `;
}

/** The parts that stay, rejoined with the separators the person would have typed. */
export function cutParts(command: string, parts: readonly CommandPart[] = []): string {
  const kept = parts.filter((part) => !isMachinery(part));
  if (kept.length === parts.length) return command;
  const joined = kept.map((part, index) => textOf(command, part) + (index < kept.length - 1 ? glue(part.Separator) : '')).join('');
  return joined.trim() || command;
}

/** The first line with an ellipsis when more follows. */
export function firstLine(text: string): string {
  const lines = text.split('\n').map((line) => line.trim()).filter(Boolean);
  if (lines.length === 0) return '';
  return lines.length > 1 ? `${lines[0]}${ELLIPSIS}` : lines[0];
}

export function displayCommand(command: string, parts?: readonly unknown[] | null): string {
  return firstLine(cutParts(command, (parts ?? []) as readonly CommandPart[]));
}
