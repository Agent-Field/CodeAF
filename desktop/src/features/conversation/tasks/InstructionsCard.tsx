import { useState, type KeyboardEvent } from 'react';
import { Button, Markdown, TextArea } from '../../../components/ui';
import { parseBrief } from './brief';
import { FullBrief } from './FullBrief';
import './instructions-card.css';

export type InstructionsCardProps = {
  text: string;
  /** Changes the brief through the engine; without it (or once the task is over) there is no Edit. */
  onAmend?: (text: string) => Promise<void> | void;
};

type EditorProps = { initial: string; onSave: (text: string) => Promise<void> | void; onCancel: () => void };

/** The brief as a field: Enter alone is a newline, Cmd/Ctrl+Enter saves, Escape leaves it as it was. */
function Editor({ initial, onSave, onCancel }: EditorProps) {
  const [draft, setDraft] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const unchanged = draft.trim() === '' || draft.trim() === initial.trim();

  async function save() {
    if (unchanged || saving) return;
    setSaving(true);
    setError('');
    try {
      await onSave(draft.trim());
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : 'Could not change the brief.');
      setSaving(false);
    }
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) return;
    if (event.key === 'Escape') return onCancel();
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      void save();
    }
  }

  return (
    <div className="instructions-editor">
      <TextArea className="instructions-field" aria-label="New brief for this task" value={draft} rows={4} readOnly={saving} autoFocus onChange={(event) => setDraft(event.target.value)} onKeyDown={onKeyDown} />
      {error && <p className="instructions-error" role="alert">{error}</p>}
      <div className="instructions-editor-actions">
        <Button onClick={onCancel}>Cancel</Button>
        <Button variant="primary" disabled={unchanged} loading={saving} onClick={() => void save()}>Save</Button>
      </div>
    </div>
  );
}

/** What the task was asked to do, on a field card: three lines until opened, with Edit while it can still be changed. */
export function InstructionsCard({ text, onAmend }: InstructionsCardProps) {
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [full, setFull] = useState(false);
  if (!text.trim()) return null;
  const brief = parseBrief(text);
  return (
    <section className="instructions-card" aria-label="Instructions">
      <div className="instructions-head">
        <Button className="instructions-toggle" aria-expanded={open} onClick={() => setOpen(!open)}>
          Instructions
        </Button>
        {onAmend && !editing && <Button className="instructions-edit" onClick={() => setEditing(true)}>Edit</Button>}
      </div>
      {editing && onAmend ? (
        <Editor
          initial={text}
          onSave={async (next) => {
            await onAmend(next);
            setEditing(false);
          }}
          onCancel={() => setEditing(false)}
        />
      ) : (
        <div className="instructions-body" data-open={open || undefined}>
          <Markdown>{brief.summary}</Markdown>
        </div>
      )}
      {open && !editing && brief.sections.length > 1 && (
        <>
          <Button className="instructions-full" aria-expanded={full} onClick={() => setFull(!full)}>Full brief</Button>
          {full && <FullBrief sections={brief.sections} />}
        </>
      )}
    </section>
  );
}
