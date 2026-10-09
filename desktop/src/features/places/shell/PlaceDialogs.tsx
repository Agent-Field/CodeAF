import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode, type RefObject } from 'react';
import { Button, SectionHeading, Segmented, Text, TextArea, TextInput } from '../../../components/ui';
import type { PickResult } from '../../../design/nativeControls';
import type { SourceCheck, SourceKind } from '../client';
import './place-dialogs.css';

// The three small sheets the Places shell asks the person through: a place's instructions, its files and links, and a
// name (rename or create). Each is a native modal <dialog> on overlay-surface, so the browser traps focus and Escape
// cancels. Each keeps the person's words when the engine refuses: the refusal is drawn in an alert line and the sheet
// stays open. None of them fetches or invents anything; every list and every write comes from the owner.

const messageOf = (error: unknown) => (error instanceof Error && error.message ? error.message : 'That did not work.');

type SheetProps = {
  open: boolean;
  title: string;
  /** One sentence under the title, when the sheet needs it. */
  lede?: ReactNode;
  /** The field that takes focus when the sheet opens. */
  initialFocus: RefObject<HTMLElement | null>;
  onClose: () => void;
  children: ReactNode;
};

/** The shared sheet. The dialog stays on the page; its body is drawn only while open, so every opening starts from the
 * owner's values. Focus goes to the first field on open and back to whatever had it on close. */
function Sheet({ open, title, lede, initialFocus, onClose, children }: SheetProps) {
  const dialog = useRef<HTMLDialogElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const titleId = useId();
  useEffect(() => {
    const element = dialog.current;
    if (!element) return;
    if (open && !element.open) {
      opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      element.showModal();
      // React's autoFocus runs before showModal, while the dialog cannot take focus, so the first field is focused by hand.
      initialFocus.current?.focus();
    } else if (!open && element.open) element.close();
  }, [open, initialFocus]);
  // The close event fires for Escape, Cancel and a finished save alike; the owner learns of each one exactly here.
  const closed = () => {
    const back = opener.current;
    opener.current = null;
    if (back?.isConnected) back.focus();
    onClose();
  };
  return <dialog ref={dialog} className="place-dialog" aria-labelledby={titleId} onClose={closed}>
    {open && <>
      <div className="place-dialog-head">
        <SectionHeading id={titleId} className="place-dialog-title">{title}</SectionHeading>
        {lede && <Text className="place-dialog-lede">{lede}</Text>}
      </div>
      {children}
    </>}
  </dialog>;
}

/** Ends the sheet the way Escape does, so the close event (and with it focus return and onClose) runs once. */
const closeSheet = (element: Element | null) => {
  const dialog = element?.closest('dialog');
  if (dialog?.open) dialog.close();
};

function Refusal({ message }: { message?: string }) {
  return message ? <Text role="alert" className="place-dialog-error">{message}</Text> : null;
}

// ---------------------------------------------------------------------------

export type InstructionsDialogProps = {
  open: boolean;
  placeName: string;
  /** The instructions as the engine has them now; '' when there are none. */
  initial: string;
  onSave: (text: string) => Promise<void>;
  onClose: () => void;
};

/** "Instructions for “X”": plain prose the AI reads in every chat in the place. Saving unchanged text writes nothing. */
export function InstructionsDialog({ open, placeName, initial, onSave, onClose }: InstructionsDialogProps) {
  const field = useRef<HTMLTextAreaElement>(null);
  return <Sheet open={open} title={`Instructions for “${placeName}”`} lede="Plain prose the AI reads in every chat in this place." initialFocus={field} onClose={onClose}>
    <InstructionsBody field={field} placeName={placeName} initial={initial} onSave={onSave}/>
  </Sheet>;
}

function InstructionsBody({ field, placeName, initial, onSave }: { field: RefObject<HTMLTextAreaElement | null>; placeName: string; initial: string; onSave: (text: string) => Promise<void> }) {
  const [text, setText] = useState(initial);
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState(false);
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = event.currentTarget;
    if (text === initial) { closeSheet(form); return; }
    setPending(true);
    setError(undefined);
    try {
      await onSave(text);
      closeSheet(form);
    } catch (refusal) {
      setError(messageOf(refusal));
    } finally {
      setPending(false);
    }
  };
  return <form className="place-dialog-body" onSubmit={event => void submit(event)}>
    <TextArea ref={field} className="place-dialog-area" rows={8} value={text} aria-label={`Instructions for ${placeName}`} onChange={event => setText(event.target.value)}/>
    <Refusal message={error}/>
    <div className="place-dialog-foot">
      <Button variant="quiet" onClick={event => closeSheet(event.currentTarget)}>Cancel</Button>
      <Button variant="primary" type="submit" loading={pending}>Save</Button>
    </div>
  </form>;
}

// ---------------------------------------------------------------------------

export type PlaceSourceRow = { id: string; kind: SourceKind; label: string; state?: SourceCheck['state'] };
export type SourceAsk = { kind: SourceKind; ref: string };

export type SourcesDialogProps = {
  open: boolean;
  placeName: string;
  /** The place's sources as the engine reports them; the sheet draws exactly these. */
  sources: readonly PlaceSourceRow[];
  /** Whether a real chooser exists (`nativeControls().desktop`). Without one, only typing a path or a link is offered. */
  native: boolean;
  onPickFolder: () => Promise<PickResult>;
  onPickFiles: () => Promise<PickResult>;
  onAdd: (ask: SourceAsk[]) => Promise<void>;
  onRemove: (sourceId: string) => Promise<void>;
  onClose: () => void;
};

const kindWord: Record<SourceKind, string> = { folder: 'Folder', repo: 'Repository', file: 'File', url: 'Link', chat: 'Chat' };
/** Only the states a person has to act on get a word; a readable source says nothing (the emptiness law). */
const stateWord: Partial<Record<NonNullable<PlaceSourceRow['state']>, string>> = { missing: 'missing', unreadable: 'unreadable' };
const isWebLink = (text: string) => /^https?:\/\//i.test(text);
type DiskKind = 'folder' | 'file';
const diskKinds: { value: DiskKind; label: string }[] = [{ value: 'folder', label: 'Folder' }, { value: 'file', label: 'File' }];

/** "Files and links for “X”": the place's sources, a Remove per row, the native choosers where they exist, and always a
 * field for a typed absolute path or an http(s) link. */
export function SourcesDialog(props: SourcesDialogProps) {
  const field = useRef<HTMLInputElement>(null);
  return <Sheet open={props.open} title={`Files and links for “${props.placeName}”`} initialFocus={field} onClose={props.onClose}>
    <SourcesBody field={field} {...props}/>
  </Sheet>;
}

function SourcesBody({ field, sources, native, onPickFolder, onPickFiles, onAdd, onRemove }: SourcesDialogProps & { field: RefObject<HTMLInputElement | null> }) {
  const [text, setText] = useState('');
  const [diskKind, setDiskKind] = useState<DiskKind>('folder');
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  /** 'folder' | 'file' while a chooser is open, 'add' while a typed add is out, or the id of a source being removed. */
  const [pending, setPending] = useState<string>();
  const typed = text.trim();
  const link = isWebLink(typed);

  const attempt = async (what: string, work: () => Promise<void>) => {
    setPending(what);
    setError(undefined);
    setNotice(undefined);
    try { await work(); } catch (refusal) { setError(messageOf(refusal)); } finally { setPending(undefined); }
  };

  const choose = (kind: DiskKind) => attempt(kind, async () => {
    const picked = await (kind === 'folder' ? onPickFolder() : onPickFiles());
    if (picked.status === 'busy') { setNotice('Another chooser is already open.'); return; }
    if (picked.status === 'unavailable') { setNotice('Choosing from disk needs the desktop app. Paste an absolute path or a link.'); return; }
    if (picked.status !== 'picked' || picked.paths.length === 0) return;
    await onAdd(picked.paths.map(path => ({ kind, ref: path.path })));
  });

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!typed || pending) return;
    void attempt('add', async () => {
      await onAdd([{ kind: link ? 'url' : diskKind, ref: typed }]);
      setText('');
    });
  };

  return <div className="place-dialog-body">
    {sources.length > 0 && <ul className="place-dialog-sources" aria-label="Files and links">
      {sources.map(source => <li key={source.id} className="place-dialog-source">
        <span className="place-dialog-source-label">{source.label}</span>
        <span className="place-dialog-source-meta">{kindWord[source.kind]}{source.state && stateWord[source.state] ? ` · ${stateWord[source.state]}` : ''}</span>
        <Button variant="ghost" className="place-dialog-source-remove" aria-label={`Remove ${source.label}`} loading={pending === source.id}
          disabled={pending !== undefined} onClick={() => void attempt(source.id, () => onRemove(source.id))}>Remove</Button>
      </li>)}
    </ul>}
    {native
      ? <div className="place-dialog-pickers">
        <Button variant="quiet" loading={pending === 'folder'} disabled={pending !== undefined} onClick={() => void choose('folder')}>Choose a folder…</Button>
        <Button variant="quiet" loading={pending === 'file'} disabled={pending !== undefined} onClick={() => void choose('file')}>Choose files…</Button>
      </div>
      : <Text className="place-dialog-quiet">Choosing from disk needs the desktop app. Paste an absolute path or a link.</Text>}
    <form className="place-dialog-add" onSubmit={submit}>
      <TextInput ref={field} appearance="field" className="place-dialog-add-field" value={text} aria-label="Add a path or a link" placeholder="/absolute/path or https://…"
        onChange={event => setText(event.target.value)}/>
      {!link && <Segmented label="Kind of path" options={diskKinds} value={diskKind} onChange={setDiskKind}/>}
      <Button variant="primary" type="submit" loading={pending === 'add'} disabled={!typed || (pending !== undefined && pending !== 'add')}>Add</Button>
    </form>
    {notice && <Text role="status" className="place-dialog-quiet">{notice}</Text>}
    <Refusal message={error}/>
    <div className="place-dialog-foot">
      <Button variant="quiet" onClick={event => closeSheet(event.currentTarget)}>Done</Button>
    </div>
  </div>;
}

// ---------------------------------------------------------------------------

export type NameDialogProps = {
  open: boolean;
  title: string;
  /** The field's accessible name and its label ("Name"). */
  label: string;
  initial: string;
  submitLabel: string;
  onSubmit: (name: string) => Promise<void>;
  onClose: () => void;
};

/** One name, for Rename and New place. The name is trimmed; an empty one is refused before anything is written, and an
 * unchanged rename writes nothing. */
export function NameDialog({ open, title, label, initial, submitLabel, onSubmit, onClose }: NameDialogProps) {
  const field = useRef<HTMLInputElement>(null);
  return <Sheet open={open} title={title} initialFocus={field} onClose={onClose}>
    <NameBody field={field} label={label} initial={initial} submitLabel={submitLabel} onSubmit={onSubmit}/>
  </Sheet>;
}

function NameBody({ field, label, initial, submitLabel, onSubmit }: Pick<NameDialogProps, 'label' | 'initial' | 'submitLabel' | 'onSubmit'> & { field: RefObject<HTMLInputElement | null> }) {
  const [name, setName] = useState(initial);
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState(false);
  const inputId = useId();
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = event.currentTarget;
    const trimmed = name.trim();
    if (!trimmed) { setError('A place needs a name.'); field.current?.focus(); return; }
    if (initial.trim() && trimmed === initial.trim()) { closeSheet(form); return; }
    setPending(true);
    setError(undefined);
    try {
      await onSubmit(trimmed);
      closeSheet(form);
    } catch (refusal) {
      setError(messageOf(refusal));
    } finally {
      setPending(false);
    }
  };
  return <form className="place-dialog-body" onSubmit={event => void submit(event)} noValidate>
    <label className="place-dialog-label" htmlFor={inputId}>{label}</label>
    <TextInput ref={field} id={inputId} appearance="field" value={name} aria-invalid={error ? true : undefined}
      onChange={event => { setName(event.target.value); if (error) setError(undefined); }}/>
    <Refusal message={error}/>
    <div className="place-dialog-foot">
      <Button variant="quiet" onClick={event => closeSheet(event.currentTarget)}>Cancel</Button>
      <Button variant="primary" type="submit" loading={pending}>{submitLabel}</Button>
    </div>
  </form>;
}
