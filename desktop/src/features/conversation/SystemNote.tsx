import { useRef, useState, type ReactNode } from 'react';
import { Button, Icon, type IconName } from '../../components/ui';
import { placesTransport } from '../places/client';
import { usePlacesShell } from '../places/shell/PlacesShell';
import { toasts } from '../../design/toasts';
import './system-note.css';

export type SystemNoteKind = 'info' | 'stopped' | 'failure' | 'retrying' | 'compaction' | 'remember';

type SystemNoteProps = {
  kind: SystemNoteKind;
  children: ReactNode;
  /** Mono time beside a retry, e.g. "4s". Shown only when the engine says it. */
  time?: string;
  /** The action follows the note: Retry on a failure or Undo on a saved line. */
  action?: { label: string; onClick: () => void; expanded?: boolean; disabled?: boolean };
};

const MARK: Partial<Record<SystemNoteKind, IconName>> = { info: 'info', stopped: 'ban', failure: 'warn' };
const ROLE: Record<SystemNoteKind, string> = { info: 'note', stopped: 'note', failure: 'alert', retrying: 'status', compaction: 'note', remember: 'status' };

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

/** Quiet engine notes keep their action beside the words; a compaction is centred in the column. */
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
      {kind !== 'remember' && <Mark kind={kind} />}
      <span className="system-note-text">{children}</span>
      {time && <span className="system-note-time">{time}</span>}
      {action && (
        <>
          {kind === 'remember' && <span aria-hidden="true">·</span>}
          <Button className="system-note-action" disabled={action.disabled} aria-expanded={action.expanded} onClick={action.onClick}>
            {action.label}
          </Button>
        </>
      )}
    </div>
  );
}

/** The engine's token identifies the saved line without guessing a place from its display name. */
export function RememberNote({ text, token }: { text: string; token: string }) {
  const shell = usePlacesShell();
  const busy = useRef(false);
  const [pending, setPending] = useState(false);
  const [removed, setRemoved] = useState(false);
  async function undo() {
    if (busy.current || removed) return;
    busy.current = true;
    setPending(true);
    try {
      await placesTransport('/places/knows', { method: 'DELETE', body: { token } });
      setRemoved(true);
      await shell?.refresh();
    } catch (error) {
      toasts.show({ message: [error instanceof Error ? error.message : 'That line could not be removed.'], tone: 'warning' });
    } finally {
      busy.current = false;
      setPending(false);
    }
  }
  return <SystemNote kind="remember" action={removed ? undefined : { label: 'Undo', disabled: pending, onClick: () => void undo() }}>{removed ? 'Removed' : text}</SystemNote>;
}

/** Session notes that sit together: indented under the guide rule, as the work block is. */
export function SystemNoteGroup({ children }: { children: ReactNode }) {
  return <div className="system-note-group">{children}</div>;
}
