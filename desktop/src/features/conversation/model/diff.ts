// The +N −M of an edit or write, computed from the call's arguments (the
// engine returns no diff). Ported from tui3 toolstat.go editPairs/editStat.

import { argBody, argString, argsOf, lineCount, splitLines, type Fields } from './args.ts';

export type EditPair = { old: string; next: string };
export type Stat = { added: number; removed: number; capped: boolean };

// Past this a block pair degrades to "all of that became all of this".
const DIFF_CEILING = 600;
const NAME_PAIRS: [string, string][] = [
  ['oldText', 'newText'],
  ['old_string', 'new_string'],
];

function pairFrom(item: unknown): EditPair | undefined {
  if (!item || typeof item !== 'object') return undefined;
  const fields = item as Fields;
  for (const [oldName, newName] of NAME_PAIRS) {
    const old = argString(fields, oldName);
    const next = argString(fields, newName);
    if (old || next) return { old, next };
  }
  return undefined;
}

/** `edits` is an array, or an array the model sent as a JSON string. */
function editsList(raw: unknown): unknown[] {
  if (Array.isArray(raw)) return raw;
  if (typeof raw !== 'string') return [];
  try {
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

export function editPairs(fields: Fields): EditPair[] {
  const pairs = editsList(fields.edits).map(pairFrom);
  pairs.push(pairFrom(fields)); // the legacy top-level pair
  return pairs.filter((pair): pair is EditPair => pair !== undefined);
}

/** Length of the longest common subsequence, two rows of memory. */
function lcsLength(a: string[], b: string[]): number {
  let prev = new Array<number>(b.length + 1).fill(0);
  for (const line of a) {
    const row = new Array<number>(b.length + 1).fill(0);
    for (let j = 1; j <= b.length; j++) {
      row[j] = line === b[j - 1] ? prev[j - 1] + 1 : Math.max(prev[j], row[j - 1]);
    }
    prev = row;
  }
  return prev[b.length];
}

/** Changed lines between two blocks: what the LCS does not keep. */
export function diffStat(old: string[], next: string[]): { added: number; removed: number } {
  if (old.length > DIFF_CEILING || next.length > DIFF_CEILING) return { added: next.length, removed: old.length };
  const kept = lcsLength(old, next);
  return { added: next.length - kept, removed: old.length - kept };
}

export function editStat(fields: Fields): Stat {
  const total: Stat = { added: 0, removed: 0, capped: false };
  for (const pair of editPairs(fields)) {
    const old = argBody(pair.old);
    const next = argBody(pair.next);
    const part = diffStat(splitLines(old.body), splitLines(next.body));
    total.added += part.added;
    total.removed += part.removed;
    total.capped = total.capped || old.capped || next.capped;
  }
  return total;
}

export function writeStat(fields: Fields): Stat {
  const content = argBody(argString(fields, 'content'));
  return { added: lineCount(content.body), removed: 0, capped: content.capped };
}

/** The stat of one write or edit call, from its raw args string. */
export function callStat(tool: string, args: string): Stat {
  const fields = argsOf(args);
  return tool === 'write' ? writeStat(fields) : editStat(fields);
}
