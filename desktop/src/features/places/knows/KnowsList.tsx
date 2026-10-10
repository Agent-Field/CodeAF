import { useRef, useState, type KeyboardEvent } from 'react';
import { Button, IconButton, RowActions, SectionLabel, TextInput } from '../../../components/ui';
import { toasts } from '../../../design/toasts';
import { knowsList, type KnowsLine } from './model';
import './knows.css';

export type KnowsChange = { undo: () => void | Promise<void> };
export type KnowsActions = {
  add?: (text: string) => Promise<KnowsChange>;
  edit?: (id: string, text: string) => Promise<KnowsChange>;
  remove?: (id: string) => Promise<KnowsChange>;
  confirm?: (id: string) => Promise<void>;
};
export type KnowsListProps = {
  placeName: string;
  lines: readonly KnowsLine[];
  now: Date;
  chatTitles?: Readonly<Record<string, string>>;
  actions?: KnowsActions;
  readOnly?: boolean;
};

/** The owner refreshes engine lines after each write; this list never manufactures saved knowledge. */
export function KnowsList({ placeName, lines, now, chatTitles, actions = {}, readOnly }: KnowsListProps) {
  const [all, setAll] = useState(false);
  const [draft, setDraft] = useState('');
  const [editing, setEditing] = useState<{ id: string; text: string }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const locked = useRef(false);
  const addField = useRef<HTMLInputElement>(null);
  const root = useRef<HTMLElement>(null);
  const view = knowsList(lines, now, chatTitles);
  const rows = all ? view.all : view.home;
  const canWrite = !readOnly && !busy;

  async function run(job: () => Promise<void>) {
    if (locked.current) return;
    locked.current = true;
    setBusy(true);
    setError('');
    try { await job(); }
    catch (failure) { setError(failure instanceof Error ? failure.message : 'That change could not be saved.'); }
    finally { locked.current = false; setBusy(false); }
  }
  function focusRow(id: string) {
    root.current?.querySelectorAll<HTMLElement>('[data-knows-row]').forEach(row => { if (row.dataset.knowsRow === id) row.focus(); });
  }
  function remove(id: string) {
    if (!canWrite || !actions.remove) return;
    const index = rows.findIndex(row => row.line.id === id);
    const neighbor = rows[index + 1]?.line.id ?? rows[index - 1]?.line.id;
    void run(async () => {
      const change = await actions.remove!(id);
      toasts.show({ message: ['Line removed'], undo: change.undo });
      if (editing?.id === id) setEditing(undefined);
      if (neighbor) focusRow(neighbor); else addField.current?.focus();
    });
  }
  function rowKey(event: KeyboardEvent<HTMLLIElement>, id: string) {
    // Delete in a text field edits words, never the engine's entire line.
    if (event.target !== event.currentTarget) return;
    if (event.key === 'Delete') { event.preventDefault(); remove(id); }
    if (event.key === 'Enter' && canWrite && actions.edit) {
      event.preventDefault();
      setEditing({ id, text: lines.find(line => line.id === id)!.text });
    }
    const step = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0;
    const next = rows[rows.findIndex(row => row.line.id === id) + step];
    if (step && next) { event.preventDefault(); focusRow(next.line.id); }
  }
  return <section ref={root} className="knows-list" aria-label={`What ${placeName} knows`} aria-busy={busy}>
    <div className="knows-heading">
      <SectionLabel>What {placeName} knows{view.all.length > 0 && <> · {view.all.length}</>}</SectionLabel>
      {view.allLabel && <Button className="knows-all" aria-expanded={all} onClick={() => setAll(!all)}>All</Button>}
    </div>
    <ul className="knows-rows">
      {rows.map(({ line, source, struck, prompt }) => <li key={line.id} tabIndex={0} data-knows-row={line.id} data-actions-host className="knows-row" onKeyDown={event => rowKey(event, line.id)}>
        <div className="knows-words">
          {editing?.id === line.id && !readOnly && actions.edit ? <TextInput autoFocus appearance="field" aria-label="Edit knowledge" value={editing.text} readOnly={busy}
            onChange={event => setEditing({ id: line.id, text: event.target.value })}
            onKeyDown={event => {
              if (event.key === 'Escape') { event.preventDefault(); setEditing(undefined); focusRow(line.id); }
              if (event.key !== 'Enter' || event.nativeEvent.isComposing || !editing.text.trim() || !canWrite) return;
              event.preventDefault();
              void run(async () => {
                const change = await actions.edit!(line.id, editing.text.trim());
                setEditing(undefined); focusRow(line.id);
                toasts.show({ message: ['Line updated'], undo: change.undo });
              });
            }}/>
            : <span className="knows-text" data-replaced={struck || undefined}>{line.text}</span>}
          {source && <span className="knows-source">{line.source.kind === 'you-wrote' && !struck ? 'you added' : source}</span>}
          {prompt && <div className="knows-prompt"><span>{prompt}</span>
            {!readOnly && actions.confirm && <Button disabled={!canWrite} onClick={() => void run(() => actions.confirm!(line.id))}>Yes</Button>}
            {!readOnly && actions.remove && <Button disabled={!canWrite} onClick={() => remove(line.id)}>Remove</Button>}
          </div>}
        </div>
        {!readOnly && (actions.edit || actions.remove) && <RowActions>
          {actions.edit && <IconButton size="row" iconSize="tiny" icon="pencil" label={`Edit ${line.text}`} disabled={busy} onClick={() => setEditing({ id: line.id, text: line.text })}/>}
          {actions.remove && <IconButton size="row" iconSize="tiny" icon="close" label={`Remove ${line.text}`} disabled={busy} onClick={() => remove(line.id)}/>}
        </RowActions>}
      </li>)}
    </ul>
    {!readOnly && actions.add && <TextInput ref={addField} appearance="field" className="knows-add" aria-label={`Add something ${placeName} should know`} placeholder={`Add something ${placeName} should know`} value={draft} readOnly={busy}
      onChange={event => setDraft(event.target.value)} onKeyDown={event => {
        if (event.key !== 'Enter' || event.nativeEvent.isComposing || !draft.trim() || !canWrite) return;
        event.preventDefault();
        void run(async () => { const change = await actions.add!(draft.trim()); setDraft(''); toasts.show({ message: ['Line added'], undo: change.undo }); });
      }}/> }
    {error && <p className="knows-error" role="alert">{error}</p>}
  </section>;
}
