import { NoteItem } from '../NoteItem';
import type { TurnItem } from '../types';

function words(item: TurnItem): string {
  switch (item.kind) {
    case 'note':
    case 'steer':
    case 'error':
      return item.text;
    case 'aside':
      return item.title;
    default:
      return '';
  }
}

/** Notes that happened inside the work: one muted line each. A steer wears the elbow mark.
 * A long note is the session's own report: its first line shows, the rest folds under Show. */
export function WorkNotes({ notes }: { notes: TurnItem[] }) {
  const shown = notes.filter((item) => words(item) !== '');
  if (shown.length === 0) return null;
  return (
    <>
      {shown.map((item) =>
        item.kind === 'note' ? (
          <NoteItem key={item.id} text={item.text} long={item.long} tone={item.tone} />
        ) : (
          <p key={item.id} className="work-note" data-kind={item.kind}>
            {item.kind === 'steer' ? `↳ ${words(item)}` : words(item)}
          </p>
        ),
      )}
    </>
  );
}
