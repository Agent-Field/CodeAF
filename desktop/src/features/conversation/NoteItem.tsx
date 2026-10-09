import { useState } from 'react';
import { Button, Icon } from '../../components/ui';
import { SystemNote, type SystemNoteKind } from './SystemNote';
import type { NoteTone } from './types';

type NoteItemProps = { text: string; long?: boolean; tone?: NoteTone };

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

export function NoteItem({ text, long, tone }: NoteItemProps) {
  if (!text) return null;
  if (long) return <FoldedNote text={text} />;
  return <SystemNote kind={tone ? KIND[tone] : 'info'}>{text}</SystemNote>;
}
