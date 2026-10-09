import type { ReactNode } from 'react';
import { Button, Icon, type IconName } from '../../components/ui';
import './system-note.css';

export type SystemNoteKind = 'info' | 'stopped' | 'failure' | 'retrying' | 'compaction';

type SystemNoteProps = {
  kind: SystemNoteKind;
  children: ReactNode;
  /** Mono time beside a retry, e.g. "4s". Shown only when the engine says it. */
  time?: string;
  /** The one action a note carries: Retry on a failure, Show on a folded report. */
  action?: { label: string; onClick: () => void; expanded?: boolean };
};

const MARK: Partial<Record<SystemNoteKind, IconName>> = { info: 'info', stopped: 'ban', failure: 'warn' };
const ROLE: Record<SystemNoteKind, string> = { info: 'note', stopped: 'note', failure: 'alert', retrying: 'status', compaction: 'note' };

/** The mark in front of a note: a quiet icon, or the accent dot of a retry in flight. */
function Mark({ kind }: { kind: SystemNoteKind }) {
  const name = MARK[kind];
  if (name) return <Icon name={name} size="xs" />;
  return (
    <span className="system-note-dot" aria-hidden="true">
      <span />
    </span>
  );
}

/** Quiet single-line engine notes (design v3, Turn footer · System notes). Only a failure gets
 * colour and an action; a compaction is the one centred element in the column. */
export function SystemNote({ kind, children, time, action }: SystemNoteProps) {
  if (kind === 'compaction') {
    return (
      <div className="system-note" data-kind="compaction" role={ROLE[kind]}>
        <span className="system-note-rule" />
        <span className="system-note-text">{children}</span>
        <span className="system-note-rule" />
      </div>
    );
  }
  return (
    <div className="system-note" data-kind={kind} role={ROLE[kind]}>
      <Mark kind={kind} />
      <span className="system-note-text">{children}</span>
      {time && <span className="system-note-time">{time}</span>}
      {action && (
        <Button className="system-note-action" aria-expanded={action.expanded} onClick={action.onClick}>
          {action.label}
        </Button>
      )}
    </div>
  );
}

/** Session notes that sit together: indented under the guide rule, as the work block is. */
export function SystemNoteGroup({ children }: { children: ReactNode }) {
  return <div className="system-note-group">{children}</div>;
}
