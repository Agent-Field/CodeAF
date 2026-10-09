import { ErrorItem } from '../ErrorItem';
import { NoteItem } from '../NoteItem';
import { SystemNote, SystemNoteGroup } from '../SystemNote';

/** Every system note kind with fixture words. The engine supplies the real words. */
export function SystemNotesSpecimen() {
  return (
    <div className="system-notes-specimen">
      <SystemNote kind="stopped">Stopped</SystemNote>
      <ErrorItem text="The provider returned an error. Your draft is kept." onRetry={() => undefined} />
      <SystemNote kind="retrying" time="4s">
        Retrying. The provider was busy.
      </SystemNote>
      <NoteItem text="Earlier messages summarized" tone="compaction" />
      <SystemNoteGroup>
        <NoteItem text="Switched to the fallback model for this turn" />
        <NoteItem text={'Task 1 done: reading the folder.\nFolder: a scratch workspace.'} long />
      </SystemNoteGroup>
    </div>
  );
}
