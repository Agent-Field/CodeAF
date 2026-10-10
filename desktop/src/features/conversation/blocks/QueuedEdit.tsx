import { useState } from 'react';
import { Button, TextArea } from '../../../components/ui';

/**
 * One queued message being edited in place: Enter or Save keeps it, Esc leaves it as it was.
 * A text field would drop the line breaks inside a pasted-text block, so this is a
 * one-line-tall area whose value is the whole stored message.
 */
export function QueuedEdit({ text, onSave, onCancel }: { text: string; onSave: (text: string) => void; onCancel: () => void }) {
  const [draft, setDraft] = useState(text);
  const save = () => (draft.trim() ? onSave(draft.trim()) : onCancel());
  return (
    <div className="queued-edit">
      <TextArea
        autoFocus
        rows={1}
        aria-label="Edit queued message"
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={(event) => {
          // Enter saves, as it did when this was a single-line field. The area must not insert a break first.
          if (event.key === 'Enter') {
            event.preventDefault();
            save();
          }
          if (event.key === 'Escape') onCancel();
        }}
      />
      <span className="queued-esc">Esc</span>
      <Button variant="primary" className="queued-save" onClick={save}>
        Save
      </Button>
    </div>
  );
}
