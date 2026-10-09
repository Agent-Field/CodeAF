import { Icon, type IconName } from './Icon';

/** The designer's eight still status marks. Elapsed time is the only motion; words carry the meaning. */
export type Status = 'running' | 'queued' | 'done' | 'waiting' | 'failed' | 'stopped' | 'paused' | 'incomplete';
const glyphs: Partial<Record<Status, IconName>> = { done: 'check', failed: 'triangleAlert', stopped: 'ban', paused: 'pause', incomplete: 'queued' };

/** A 14px box holding a 6px dot (running, your call), an 8px ring (queued) or a 14px glyph. */
export function StatusMark({ status, label }: { status: Status; label: string }) {
 const glyph = glyphs[status];
 return <span className="status-mark" data-status={status} role="img" aria-label={label}>
  {glyph ? <Icon name={glyph} size="sm"/> : <span className="status-mark-dot"/>}
 </span>;
}
