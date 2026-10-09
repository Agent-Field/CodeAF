import { useMemo, useState } from 'react';
import { Button } from '../../components/ui';
import type { EngineDiffLine, EngineFileDiff } from '../chat/engine-client';
import { diffRows } from './diffRows';

const signOf: Record<EngineDiffLine['kind'], string> = { context: '', add: '+', del: '-' };

/** One diff line: old and new line numbers, the sign column, then the text. Red and green fills come from tokens. */
export function DiffLine({ line }: { line: EngineDiffLine }) {
  return <div className="file-row file-diff-row" data-kind={line.kind}>
    <span className="file-num">{line.old}</span>
    <span className="file-num">{line.new}</span>
    <span className="file-sign" aria-hidden="true">{signOf[line.kind]}</span>
    <span className="file-text">{line.text}</span>
  </div>;
}

type Props = {
  diff: Pick<EngineFileDiff, 'hunks' | 'lines' | 'truncated'>;
  /** The current file split into lines, once it has been read; folds expand into it. */
  text?: readonly string[];
  /** Called the first time a fold is opened, so the file is read only when needed. */
  onNeedText: () => void;
};

/** The unified diff: a hunk header row, changed lines, and "⋯ N unchanged lines" folds that open in place. */
export function DiffBody({ diff, text, onNeedText }: Props) {
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const rows = useMemo(() => diffRows(diff, open, text), [diff, open, text]);
  const expand = (id: string) => { onNeedText(); setOpen(current => ({ ...current, [id]: true })); };
  return <>
    {rows.map(row => {
      if (row.type === 'hunk') return <div key={row.key} className="file-hunk">{row.header}</div>;
      if (row.type === 'line') return <DiffLine key={row.key} line={row.line}/>;
      return <Button key={row.key} className="file-fold" aria-label={`Show ${row.count} unchanged ${row.count === 1 ? 'line' : 'lines'}`} onClick={() => expand(row.id)}>⋯ {row.count} unchanged {row.count === 1 ? 'line' : 'lines'}</Button>;
    })}
  </>;
}
