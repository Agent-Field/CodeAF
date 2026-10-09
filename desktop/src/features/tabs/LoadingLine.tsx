import './loading-line.css';

/**
 * The 2px line along the top of a card while a web page loads (3j: the tab itself never shows a spinner).
 * Pass `progress` (0..1) when the page reports it, otherwise it runs as an indeterminate sweep.
 * The web tab (features/web/WebPane) draws it while its native view reports a load.
 */
export function LoadingLine({ active = true, progress }: { active?: boolean; progress?: number }) {
  if (!active) return null;
  const known = progress !== undefined;
  const percent = known ? Math.round(Math.min(Math.max(progress, 0), 1) * 100) : undefined;
  return <div className="load-line" role="progressbar" aria-label="Loading page" aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent} data-indeterminate={!known || undefined}><span className="load-line-bar" data-step={percent === undefined ? undefined : Math.round(percent / 10)}/></div>;
}
