import { useRef, useState } from 'react';
import { Button, Icon } from '../../../components/ui';
import { toasts } from '../../../design/toasts';
import { usePlacesShell } from '../shell/PlacesShell';
import './membership-note.css';

export type MembershipNoteProps = {
  /** The canonical note is kept verbatim, including the engine's source labels. */
  text: string;
  undoReceipts?: readonly string[];
  /** Structured live events can name a place containing the source delimiter. */
  placeName?: string;
};

/** Places 6f keeps membership changes in the transcript, beside their own receipt's Undo. */
export function MembershipNote({ text, undoReceipts, placeName }: MembershipNoteProps) {
  const shell = usePlacesShell();
  const [pending, setPending] = useState(false);
  const [undone, setUndone] = useState(false);
  const inFlight = useRef(false);
  const prefix = ['Now also using ', 'No longer using '].find(value => text.startsWith(value));
  const tail = prefix ? text.slice(prefix.length) : '';
  const name = placeName && tail.startsWith(placeName) ? placeName : tail.split(': ', 1)[0];
  const receipts = undoReceipts?.filter(value => typeof value === 'string' && value.length > 0);
  const canUndo = !!shell && !!receipts?.length && !undone;

  const undo = async () => {
    if (!shell || !receipts?.length || inFlight.current || undone) return;
    inFlight.current = true;
    setPending(true);
    try {
      await shell.client.undo(receipts);
      // A committed inverse stays committed even when refreshing the graph fails.
      setUndone(true);
      await shell.refresh();
    } catch (error) {
      toasts.show({ message: [error instanceof Error ? error.message : 'That change could not be undone.'], tone: 'warning' });
    } finally {
      inFlight.current = false;
      setPending(false);
    }
  };

  if (!text) return null;
  return (
    <div className="membership-note" role="note">
      <Icon name="layers" size="xs" />
      <span className="membership-note-text">
        {prefix ? <>{prefix}<span className="membership-note-place">{name}</span>{tail.slice(name.length)}</> : text}
        {canUndo && <> · <Button className="membership-note-undo" loading={pending} onClick={() => void undo()}>Undo</Button></>}
      </span>
    </div>
  );
}
