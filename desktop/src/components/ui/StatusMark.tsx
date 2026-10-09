import { Icon, type IconName } from './Icon';

/** The designer's eight still status marks. Elapsed time is the only motion; words carry the meaning. */
export type Status = 'running' | 'queued' | 'done' | 'waiting' | 'failed' | 'stopped' | 'paused' | 'incomplete';
const glyphs: Partial<Record<Status, IconName>> = { done: 'check', failed: 'triangleAlert', stopped: 'ban', paused: 'pause', incomplete: 'queued' };

/** Failure reads as a red dot in a dense list (design 1c, 1d), as the triangle on its own. */
const dotInDense: ReadonlySet<Status> = new Set(['failed', 'incomplete']);

/**
 * A 14px box holding a 6px dot (running, your call), an 8px ring (queued) or a 14px glyph.
 * `dense` is for rows of work (task panel, table, detail): the box takes only the dot's 6px of
 * the line, so titles sit where the design puts them, and failure draws as a red dot.
 */
export function StatusMark({ status, label, dense }: { status: Status; label: string; dense?: boolean }) {
 const glyph = dense && dotInDense.has(status) ? undefined : glyphs[status];
 return <span className="status-mark" data-status={status} data-dense={dense || undefined} role="img" aria-label={label}>
  {glyph ? <Icon name={glyph} size="sm"/> : <span className="status-mark-dot"/>}
 </span>;
}
