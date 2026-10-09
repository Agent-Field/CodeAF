// Line diff for one edit call's replacement pairs. Ported from tui3
// (toolstat.go diffOps, toolview.go hunks): LCS, so "+3 −1" counts lines that
// really changed, with unchanged lines kept as context.

export type EditPair = { oldText: string; newText: string };
export type DiffRow = { kind: 'add' | 'remove' | 'context' | 'gap'; text: string };

type Op = { kind: 'add' | 'remove' | 'same'; text: string };

// Past this many lines per side the table is skipped: all of that became all of this.
const CEILING = 600;

export function splitLines(text: string): string[] {
  if (text === '') return [];
  return text.replace(/\n$/, '').split('\n');
}

function wholesale(before: string[], after: string[]): Op[] {
  return [
    ...before.map((text): Op => ({ kind: 'remove', text })),
    ...after.map((text): Op => ({ kind: 'add', text })),
  ];
}

function lcsTable(before: string[], after: string[]): number[][] {
  const table = Array.from({ length: before.length + 1 }, () => new Array<number>(after.length + 1).fill(0));
  for (let i = before.length - 1; i >= 0; i--) {
    for (let j = after.length - 1; j >= 0; j--) {
      table[i][j] = before[i] === after[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1]);
    }
  }
  return table;
}

export function diffOps(before: string[], after: string[]): Op[] {
  if (before.length > CEILING || after.length > CEILING) return wholesale(before, after);
  const table = lcsTable(before, after);
  const ops: Op[] = [];
  let i = 0;
  let j = 0;
  while (i < before.length && j < after.length) {
    if (before[i] === after[j]) {
      ops.push({ kind: 'same', text: before[i++] });
      j++;
    } else if (table[i + 1][j] >= table[i][j + 1]) {
      ops.push({ kind: 'remove', text: before[i++] });
    } else {
      ops.push({ kind: 'add', text: after[j++] });
    }
  }
  while (i < before.length) ops.push({ kind: 'remove', text: before[i++] });
  while (j < after.length) ops.push({ kind: 'add', text: after[j++] });
  return ops;
}

export function diffStat(before: string[], after: string[]): { added: number; removed: number } {
  const ops = diffOps(before, after);
  return {
    added: ops.filter((op) => op.kind === 'add').length,
    removed: ops.filter((op) => op.kind === 'remove').length,
  };
}

/** Rows for one pair: changes plus `context` unchanged lines each side; the rest becomes one gap row. */
function pairRows(pair: EditPair, context: number): DiffRow[] {
  const ops = diffOps(splitLines(pair.oldText), splitLines(pair.newText));
  const near = ops.map((_, at) => ops.slice(Math.max(0, at - context), at + context + 1).some((op) => op.kind !== 'same'));
  const rows: DiffRow[] = [];
  ops.forEach((op, at) => {
    if (op.kind !== 'same') rows.push({ kind: op.kind, text: op.text });
    else if (near[at]) rows.push({ kind: 'context', text: op.text });
    else if (rows[rows.length - 1]?.kind !== 'gap') rows.push({ kind: 'gap', text: '' });
  });
  return rows;
}

export function diffRows(pairs: EditPair[], context = 2): DiffRow[] {
  const rows: DiffRow[] = [];
  pairs.forEach((pair, at) => {
    if (at > 0) rows.push({ kind: 'gap', text: '' });
    rows.push(...pairRows(pair, context));
  });
  return rows;
}
