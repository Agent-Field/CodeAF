import { useState } from 'react';
import { Button } from '../../../components/ui';
import { diffRows } from './diff';
import { editPairs } from './stats';

const SIGNS = { add: '+', remove: '−', context: ' ', gap: '⋯' } as const;
const LIMIT = 40;

/** A unified diff computed from an edit call's replacement pairs: two lines of context, forty rows, then "Show all". */
export function DiffView({ args }: { args: string }) {
  const [all, setAll] = useState(false);
  const rows = diffRows(editPairs(args));
  if (rows.length === 0) return null;
  const shown = all ? rows : rows.slice(0, LIMIT);
  return (
    <div className="work-diff">
      <div className="work-diff-rows" role="group" aria-label="Changes">
        {shown.map((row, at) => (
          <div key={at} className="work-diff-row" data-kind={row.kind}>
            <span className="work-diff-sign" aria-hidden="true">{SIGNS[row.kind]}</span>
            <span className="work-diff-text">{row.text}</span>
          </div>
        ))}
      </div>
      {rows.length > shown.length && (
        <Button className="work-more" onClick={() => setAll(true)}>
          {`Show all ${rows.length} rows`}
        </Button>
      )}
    </div>
  );
}
