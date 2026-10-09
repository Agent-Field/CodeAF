import { useState } from 'react';
import { Button, useMoreToRight } from '../../../components/ui';
import { diffRows } from './diff';
import { editPairs } from './stats';
import './DiffView.css';

const SIGNS = { add: '+', remove: '−', context: ' ', gap: '' } as const;
const GAP_MARK = '⋯';
const LIMIT = 40;

/** A unified diff computed from an edit call's replacement pairs: two lines of context, forty rows, then "Show all". */
export function DiffView({ args }: { args: string }) {
  const [all, setAll] = useState(false);
  const { ref, more, measure } = useMoreToRight();
  const rows = diffRows(editPairs(args));
  if (rows.length === 0) return null;
  const shown = all ? rows : rows.slice(0, LIMIT);
  return (
    <div className="work-diff">
      <div ref={ref} className="work-diff-rows" role="group" aria-label="Changes" tabIndex={0} data-more={more} onScroll={measure}>
        <div className="work-diff-body">
          {shown.map((row, at) => (
            <div key={at} className="work-diff-row" data-kind={row.kind}>
              <span className="work-diff-sign" aria-hidden="true">{SIGNS[row.kind]}</span>
              <span className="work-diff-text">{row.kind === 'gap' ? GAP_MARK : row.text}</span>
            </div>
          ))}
        </div>
      </div>
      {rows.length > shown.length && (
        <Button className="work-more" onClick={() => setAll(true)}>
          {`Show all ${rows.length} rows`}
        </Button>
      )}
    </div>
  );
}
