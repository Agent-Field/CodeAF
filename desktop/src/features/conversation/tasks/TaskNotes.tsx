import type { NoteLine } from './taskTypes';
import './task-notes.css';

/** A note on its way, drawn at once and replaced by the engine's record. */
export type Outbox = { text: string };

function PersonNote({ body, receipt, pending }: { body: string; receipt: string; pending?: boolean }) {
  return (
    <li className="task-note task-note-person" data-pending={pending || undefined}>
      <p className="task-note-bubble">{body}</p>
      {receipt && <span className="task-note-receipt">{receipt}</span>}
    </li>
  );
}

function OtherNote({ note }: { note: NoteLine }) {
  return (
    <li className="task-note task-note-other">
      {note.author && <span className="task-note-author">{note.author}</span>}
      <p className="task-note-body">{note.body}</p>
    </li>
  );
}

/** Conversation rhythm: what the person said to the task as bubbles, what others said as quiet lines. */
export function TaskNotes({ notes, outbox }: { notes: NoteLine[]; outbox?: Outbox }) {
  if (notes.length === 0 && !outbox) return null;
  return (
    <ul className="task-notes" aria-label="Notes">
      {notes.map((note) => (note.person ? <PersonNote key={note.id} body={note.body} receipt={note.receipt} /> : <OtherNote key={note.id} note={note} />))}
      {outbox && <PersonNote body={outbox.text} receipt={'Sending…'} pending />}
    </ul>
  );
}
