// Numbers a tool row wears, read from the call's JSON args. Ported from tui3
// (toolstat.go editPairs/editStat), including every legacy argument spelling.

import { diffStat, splitLines, type EditPair } from './diff.ts';

type Fields = Record<string, unknown>;
export type Stat = { added: number; removed: number; capped: boolean };

// The engine cuts long strings and ends them with "… (N more bytes)".
const CAP = /… \(\d+ more bytes\)$/;

export function parseArgs(args: string): Fields | undefined {
  const text = args.trim();
  if (!text.startsWith('{')) return undefined;
  try {
    const value: unknown = JSON.parse(text);
    return value && typeof value === 'object' ? (value as Fields) : undefined;
  } catch {
    return undefined;
  }
}

export function argString(fields: Fields | undefined, key: string): string {
  const value = fields?.[key];
  if (value === undefined || value === null) return '';
  return typeof value === 'string' ? value : JSON.stringify(value);
}

/** The text without the engine's cut marker, and whether it was cut. */
export function uncapped(text: string): { text: string; capped: boolean } {
  return CAP.test(text) ? { text: text.replace(CAP, ''), capped: true } : { text, capped: false };
}

function decodeList(raw: unknown): unknown[] {
  if (Array.isArray(raw)) return raw;
  if (typeof raw !== 'string') return [];
  try {
    const value: unknown = JSON.parse(raw); // edits sent as a JSON string
    return Array.isArray(value) ? value : [];
  } catch {
    return [];
  }
}

function pairOf(item: unknown): EditPair {
  const fields = (item && typeof item === 'object' ? item : {}) as Fields;
  return {
    oldText: argString(fields, 'oldText') || argString(fields, 'old_string'),
    newText: argString(fields, 'newText') || argString(fields, 'new_string'),
  };
}

export function editPairs(args: string): EditPair[] {
  const fields = parseArgs(args);
  if (!fields) return [];
  const pairs = decodeList(fields.edits).map(pairOf);
  for (const [oldKey, newKey] of [['oldText', 'newText'], ['old_string', 'new_string']]) {
    const pair = { oldText: argString(fields, oldKey), newText: argString(fields, newKey) };
    if (pair.oldText || pair.newText) pairs.push(pair);
  }
  return pairs;
}

export function editStat(args: string): Stat {
  const stat: Stat = { added: 0, removed: 0, capped: false };
  for (const pair of editPairs(args)) {
    const before = uncapped(pair.oldText);
    const after = uncapped(pair.newText);
    const counted = diffStat(splitLines(before.text), splitLines(after.text));
    stat.added += counted.added;
    stat.removed += counted.removed;
    stat.capped = stat.capped || before.capped || after.capped;
  }
  return stat;
}

/** Lines a write call puts down; undefined when the args carry no content. */
export function writeLines(args: string): number | undefined {
  const fields = parseArgs(args);
  if (!fields || typeof fields.content !== 'string') return undefined;
  return splitLines(uncapped(fields.content).text).length;
}

/** The path a file tool points at. */
export function argPath(args: string): string {
  const fields = parseArgs(args);
  return argString(fields, 'path') || argString(fields, 'file_path');
}

const EXIT = /Command exited with code (\d+)/;
const SHOWING = /\[Showing lines (\d+)-(\d+) of \d+/;

/** A bash call's non-zero exit code; the engine appends the footer only on failure. */
export function bashExit(output: string): number | undefined {
  const code = Number(EXIT.exec(output)?.[1]);
  return code > 0 ? code : undefined;
}

function nonEmptyLines(text: string): number {
  return text.split('\n').filter((line) => line.trim() !== '').length;
}

/** Lines a read returned: the engine's own footer when present, else what the output holds. */
export function readLines(output: string): number | undefined {
  if (output.trim() === '') return undefined;
  const range = SHOWING.exec(output);
  if (range) return Number(range[2]) - Number(range[1]) + 1;
  return nonEmptyLines(uncapped(output).text);
}

/** Matches or entries a search/list call found; undefined when the output is empty. */
export function listCount(output: string): number | undefined {
  if (output.trim() === '') return undefined;
  if (/^(No files found|\(empty directory\))/.test(output)) return 0;
  return nonEmptyLines(uncapped(output).text);
}
