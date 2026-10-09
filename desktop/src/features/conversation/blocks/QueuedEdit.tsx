import { useState } from 'react';
import { Button, TextInput } from '../../../components/ui';

/** One queued message being edited in place: Enter or Save keeps it, Esc leaves it as it was. */
export function QueuedEdit({ text, onSave, onCancel }: { text: string; onSave: (text: string) => void; onCancel: () => void }) {
  const [draft, setDraft] = useState(text);
  const save = () => (draft.trim() ? onSave(draft.trim()) : onCancel());
  return (
    <div className="queued-edit">
      <TextInput
        autoFocus
        aria-label="Edit queued message"
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter') save();
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
