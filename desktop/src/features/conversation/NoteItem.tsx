import { useRef, useState } from 'react';
import { Button, Icon } from '../../components/ui';
import { RememberNote, SystemNote, type SystemNoteKind } from './SystemNote';
import type { NoteTone } from './types';
import { usePlacesShell } from '../places/shell/PlacesShell';
import { toasts } from '../../design/toasts';

type NoteItemProps = { text: string; long?: boolean; tone?: NoteTone; time?: string; undoReceipts?: string[] };

const KIND: Record<NoteTone, SystemNoteKind> = { compaction: 'compaction', retry: 'retrying' };

/** A model-directed note over the limit is one muted disclosure line, never a wall of text:
 * a chevron and the first line (design v3 Turn footer · System notes); opening reveals the whole note. */
function FoldedNote({ text }: { text: string }) {
  const [shown, setShown] = useState(false);
  const line = text.split('\n', 1)[0];
  return (
    <div className="note-folded">
      <Button className="note-folded-row" aria-expanded={shown} onClick={() => setShown(!shown)}>
        <Icon name="chevron" size="xs" motion="disclosure" />
        <span className="system-note-text">{line}</span>
      </Button>
      {shown && <div className="note-folded-body">{text}</div>}
    </div>
  );
}

export function NoteItem({ text, long, tone, time, undoReceipts }: NoteItemProps) {
  const shell = usePlacesShell();
  const [pending, setPending] = useState(false);
  const inFlight = useRef(false);
  const [undone, setUndone] = useState(false);
  const undo = async () => {
    if (!shell || inFlight.current || undone || !undoReceipts?.length) return;
    inFlight.current = true;
    setPending(true);
    try { await shell.client.undo(undoReceipts); setUndone(true); await shell.refresh(); }
    catch (error) { toasts.show({ message: [error instanceof Error ? error.message : 'That change could not be undone.'], tone: 'warning' }); }
    finally { inFlight.current = false; setPending(false); }
  };
  if (!text) return null;
  if (text.startsWith('Saved to ') && undoReceipts?.length === 1 && undoReceipts[0].startsWith('remember_')) {
    return <RememberNote text={text} token={undoReceipts[0]} />;
  }
  if (long) return <FoldedNote text={text} />;
  return <SystemNote kind={tone ? KIND[tone] : 'info'} time={time} action={shell && undoReceipts?.length && !undone ? { label: pending ? 'Undoing…' : 'Undo', onClick: () => void undo() } : undefined}>{text}</SystemNote>;
}
