import { useState } from 'react';
import { SystemNote, type SystemNoteKind } from './SystemNote';
import type { NoteTone } from './types';

type NoteItemProps = { text: string; long?: boolean; tone?: NoteTone };

const KIND: Record<NoteTone, SystemNoteKind> = { compaction: 'compaction', retry: 'retrying' };

/** A model-directed note over the limit is one muted line with Show, never a wall of text.
 * Folded, it shows its first line; Show reveals the whole note. */
function FoldedNote({ text }: { text: string }) {
  const [shown, setShown] = useState(false);
  const line = text.split('\n', 1)[0];
  return (
    <div className="note-folded">
      <SystemNote kind="info" action={{ label: shown ? 'Hide' : 'Show', expanded: shown, onClick: () => setShown(!shown) }}>
        {!shown && line}
      </SystemNote>
      {shown && <div className="note-folded-body">{text}</div>}
    </div>
  );
}

export function NoteItem({ text, long, tone }: NoteItemProps) {
  if (!text) return null;
  if (long) return <FoldedNote text={text} />;
  return <SystemNote kind={tone ? KIND[tone] : 'info'}>{text}</SystemNote>;
}
