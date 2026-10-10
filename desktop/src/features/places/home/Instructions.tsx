import { useEffect, useRef, useState, type DragEvent } from 'react';
import { Button, TextArea, TextInput } from '../../../components/ui';
import { dropHighlight, droppedSource, instructionWrite, instructionsVisible, notePlaceholder, withNote } from './instructions-model';
import './instructions.css';

export type InstructionsProps = {
  /** The place whose prose this card edits. */
  placeId: string;
  /** The engine's instructions. Absent or blank is an empty card. */
  instructions?: string;
  /** Write instructions was pressed, so an empty card is drawn and the editor takes focus. */
  open?: boolean;
  readOnly?: boolean;
  /** Writes the whole instructions string. '' clears it. */
  onSave?: (text: string) => void | Promise<void>;
  /** A file path or an http(s) link dropped on the card. */
  onDropSource?: (source: { kind: 'file' | 'url'; ref: string }) => void | Promise<void>;
  /** The card was emptied, or Escape left an empty draft. The owner closes it. */
  onDismiss?: () => void;
};

const messageOf = (failure: unknown) => (failure instanceof Error && failure.message ? failure.message : 'That did not work. Nothing was changed.');

/**
 * Instructions on a place's Home (Places 6e, Components notes card). Plain prose you click to edit: blur saves, Escape
 * puts the last saved prose back and writes nothing. The notes line under it appends a paragraph. The card is absent
 * until there is prose or Write instructions opens it. A file or link dropped here is a source, not more prose.
 */
export function Instructions({ placeId, instructions = '', open, readOnly, onSave, onDropSource, onDismiss }: InstructionsProps) {
  const editable = !readOnly && !!onSave;
  const [shown, setShown] = useState(instructions);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(instructions);
  const [note, setNote] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [over, setOver] = useState(false);
  const skipEdit = useRef(false);
  const skipNote = useRef(false);
  const noting = useRef(false);
  const chain = useRef(Promise.resolve());
  const last = useRef(instructions);
  const adopted = useRef(instructions);
  const area = useRef<HTMLTextAreaElement>(null);
  const saved = shown;
  const showEditor = editable && (editing || (!!open && !saved.trim()));

  // The Home digest arrives a beat after a save. Adopting that stale copy would wipe the prose just written.
  useEffect(() => {
    if (instructions === adopted.current) return;
    adopted.current = instructions;
    if (editing) return;
    last.current = instructions;
    setShown(instructions);
    setDraft(instructions);
  }, [instructions, editing]);
  useEffect(() => { if (showEditor) area.current?.focus(); }, [showEditor]);

  if (!instructionsVisible(saved, !!open) && !editing) return null;

  const enqueue = (job: () => Promise<void>) => {
    const run = chain.current.then(job, job);
    chain.current = run.then(() => undefined, () => undefined);
    return run;
  };
  const persist = async (next: string) => {
    const write = instructionWrite(last.current, next);
    if (write === undefined) {
      setEditing(false);
      if (!next.trim()) onDismiss?.();
      return;
    }
    if (!onSave) { setEditing(false); return; }
    setBusy(true);
    setError('');
    try {
      await onSave(write);
      last.current = write;
      setShown(write);
      setDraft(write);
      setEditing(false);
      if (!write.trim()) onDismiss?.();
    } catch (failure) {
      setDraft(next);
      setEditing(true);
      setError(messageOf(failure));
    } finally {
      setBusy(false);
    }
  };
  const commitNote = () => {
    if (noting.current) return;
    const next = withNote(draft.trim() ? draft : saved, note);
    if (!next) return;
    noting.current = true;
    setNote('');
    void enqueue(() => persist(next)).finally(() => { noting.current = false; });
  };
  const revert = () => {
    skipEdit.current = true;
    setDraft(saved);
    setEditing(false);
    setError('');
    if (!saved.trim()) onDismiss?.();
  };
  const takeDrop = (event: DragEvent<HTMLElement>) => onDropSource ? droppedSource({
    types: [...event.dataTransfer.types],
    files: [...event.dataTransfer.files].map(file => ({ name: file.name, path: (file as File & { path?: string }).path })),
    text: event.dataTransfer.getData('text/uri-list') || event.dataTransfer.getData('text/plain'),
  }) : undefined;

  return <section className="home-instructions" aria-label="Instructions" data-place-id={placeId} aria-busy={busy || undefined} data-drop={over || undefined}
    onDragEnter={event => { if (onDropSource && dropHighlight([...event.dataTransfer.types])) { event.preventDefault(); setOver(true); } }}
    onDragOver={event => { if (onDropSource && dropHighlight([...event.dataTransfer.types])) { event.preventDefault(); setOver(true); } }}
    onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOver(false); }}
    onDrop={event => {
      setOver(false);
      const source = takeDrop(event);
      if (!source || !onDropSource) return;
      event.preventDefault();
      void enqueue(async () => {
        setBusy(true);
        setError('');
        try { await onDropSource(source); }
        catch (failure) { setError(messageOf(failure)); }
        finally { setBusy(false); }
      });
    }}>
    {showEditor
      ? <TextArea ref={area} className="home-instructions-editor" aria-label="Instructions" value={draft} rows={Math.max(2, draft.split('\n').length)} readOnly={busy}
          onChange={event => { setDraft(event.target.value); setEditing(true); }}
          onKeyDown={event => { if (event.key === 'Escape') { event.preventDefault(); revert(); event.currentTarget.blur(); } }}
          onBlur={() => { if (skipEdit.current) { skipEdit.current = false; return; } void enqueue(() => persist(draft)); }}/>
      : saved.trim()
        ? editable
          ? <Button className="home-instructions-prose" onClick={() => { setDraft(saved); setEditing(true); }}>{saved}</Button>
          : <p className="home-instructions-prose">{saved}</p>
        : null}
    {editable && <TextInput className="home-instructions-note" aria-label={notePlaceholder} placeholder={notePlaceholder} value={note} readOnly={busy}
      onChange={event => setNote(event.target.value)}
      onKeyDown={event => {
        if (event.key === 'Escape') { event.preventDefault(); skipNote.current = true; setNote(''); event.currentTarget.blur(); return; }
        if (event.key === 'Enter' && !event.nativeEvent.isComposing) { event.preventDefault(); commitNote(); }
      }}
      onBlur={() => { if (skipNote.current) { skipNote.current = false; return; } commitNote(); }}/>}
    {error && <p className="home-instructions-error" role="alert">{error}</p>}
  </section>;
}
