import { useCallback, useEffect, useRef, useState } from 'react';
import { Button } from '../../../components/ui';
import { diffRows } from './diff';
import { editPairs } from './stats';
import './DiffView.css';

const SIGNS = { add: '+', remove: '−', context: ' ', gap: '' } as const;
const GAP_MARK = '⋯';
const LIMIT = 40;

/** Whether the scroller still has content past its right edge, which is when the edge fade shows. */
function useMoreToRight() {
  const ref = useRef<HTMLDivElement>(null);
  const [more, setMore] = useState(false);
  const measure = useCallback(() => {
    const el = ref.current;
    if (el) setMore(el.scrollLeft + el.clientWidth < el.scrollWidth - 1);
  }, []);
  useEffect(() => {
    measure();
    const el = ref.current;
    if (!el || typeof ResizeObserver === 'undefined') return;
    const watch = new ResizeObserver(measure);
    watch.observe(el);
    return () => watch.disconnect();
  });
  return { ref, more, measure };
}

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
