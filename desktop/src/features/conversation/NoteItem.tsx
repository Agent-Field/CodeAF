import { useState } from 'react';
import { Button } from '../../components/ui';

type NoteItemProps = { text: string; long?: boolean };

/** A model-directed note over the limit is one muted line with Show, never a wall of text. */
function FoldedNote({ text }: { text: string }) {
  const [shown, setShown] = useState(false);
  return (
    <div className="note-folded" role="note">
      <div className="note-folded-line">
        {!shown && <span className="note-folded-text">{text}</span>}
        <Button className="note-folded-toggle" aria-expanded={shown} onClick={() => setShown(!shown)}>
          {shown ? 'Hide' : 'Show'}
        </Button>
      </div>
      {shown && <div className="note-folded-body">{text}</div>}
    </div>
  );
}

export function NoteItem({ text, long }: NoteItemProps) {
  if (!text) return null;
  if (long) return <FoldedNote text={text} />;
  return (
    <div className="note-item" role="note">
      <span className="note-item-text">{text}</span>
    </div>
  );
}
