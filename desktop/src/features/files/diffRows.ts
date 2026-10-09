// Turns an engine diff into the rows a unified view draws: hunk headers, lines and "N unchanged lines" folds.
import type { EngineDiffHunk, EngineDiffLine, EngineFileDiff } from '../chat/engine-client.ts';

export type DiffRow =
  | { type: 'hunk'; key: string; header: string }
  | { type: 'line'; key: string; line: EngineDiffLine }
  | { type: 'fold'; key: string; id: string; count: number };

/** First and last line of a hunk in the new file. A pure deletion has newLines 0 and sits after line newStart. */
function span(hunk: EngineDiffHunk): { first: number; last: number } {
  return hunk.newLines === 0 ? { first: hunk.newStart + 1, last: hunk.newStart } : { first: hunk.newStart, last: hunk.newStart + hunk.newLines - 1 };
}

/**
 * Rows for one diff. A fold is the gap of unchanged lines before a hunk or after the last one. A fold listed in
 * `open` is replaced by its lines, taken from `text` (the current file split into lines) and numbered in both
 * files; until `text` is known the fold stays closed.
 */
export function diffRows(diff: Pick<EngineFileDiff, 'hunks' | 'lines'>, open: Readonly<Record<string, boolean>> = {}, text?: readonly string[]): DiffRow[] {
  const rows: DiffRow[] = [];
  let end = 0;
  let offset = 0;
  const fold = (id: string, from: number, to: number) => {
    const count = to - from + 1;
    if (count <= 0) return;
    if (open[id] && text) {
      for (let n = from; n <= to; n += 1) rows.push({ type: 'line', key: `${id}:${n}`, line: { kind: 'context', old: n - offset, new: n, text: text[n - 1] ?? '' } });
      return;
    }
    rows.push({ type: 'fold', key: id, id, count });
  };
  diff.hunks.forEach((hunk, index) => {
    const { first, last } = span(hunk);
    fold(`before-${index}`, end + 1, first - 1);
    rows.push({ type: 'hunk', key: `hunk-${index}`, header: hunk.header });
    hunk.lines.forEach((line, at) => rows.push({ type: 'line', key: `${index}:${at}`, line }));
    end = last;
    offset += hunk.newLines - hunk.oldLines;
  });
  if (diff.hunks.length) fold('after', end + 1, diff.lines);
  return rows;
}
