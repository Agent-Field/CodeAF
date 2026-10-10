import { Icon } from '../../components/ui';
import './status-line.css';

/** The body of GET /places/{id}/decide-status. Home prints `mode` and one count. It never prints `learning.kind`. */
export type DecideStatus = {
  mode: 'deciding' | 'learning' | 'always-ask' | 'none';
  /** Agreements this week. Absent when the read did not send one. */
  agreedWeek?: number;
  /** Decisions this week. Absent when the read did not send one. */
  totalWeek?: number;
  /** The learning ring the read chose to show. `kind` may arrive and is not drawn (P-7). */
  learning?: { kind?: string; agreed: number; of: number };
};

const middle = '\u00b7';

/** A count clause is drawn only for a real, positive tally. Zero, missing, and impossible numbers are not a sentence. */
function countClause(agreed: number | undefined, total: number | undefined, tail: string): string {
  if (typeof agreed !== 'number' || typeof total !== 'number') return '';
  if (!Number.isInteger(agreed) || !Number.isInteger(total)) return '';
  if (agreed < 1 || total < 1 || agreed > total) return '';
  return ` ${middle} ${agreed} of ${total} ${tail}`;
}

/**
 * The words under a place's name. `none` and a missing read draw nothing: a place that has never been asked
 * anything has no line. Learning never names a kind; the tray shows that kind's own count.
 */
export function statusLineText(status: DecideStatus | null | undefined): string | null {
  if (!status) return null;
  if (status.mode === 'none') return null;
  if (status.mode === 'always-ask') return 'Always asks you';
  if (status.mode === 'deciding') return `Deciding automatically${countClause(status.agreedWeek, status.totalWeek, 'agreed this week')}`;
  if (status.mode === 'learning') return `Learning${countClause(status.learning?.agreed, status.learning?.of, 'agreed')}`;
  return null;
}

/**
 * The quiet line under a place Home title (Decisions 12a). Place it as the next child of the heading column,
 * which already gaps 10px: this row does not pad itself, or the gap would double.
 */
export function StatusLine({ status }: { status?: DecideStatus | null }) {
  const text = statusLineText(status);
  if (!text || !status) return null;
  return <p className="status-line" data-mode={status.mode}>
    <Icon name="sparkle" size="tiny"/>
    <span className="status-line-text">{text}</span>
  </p>;
}
