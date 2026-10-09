import { useState } from 'react';
import { Button, SectionHeading, Surface, Text, ToastRegion, useToasts } from '../../../components/ui';
import { createToasts } from '../../../design/toasts';
import type { PickResult } from '../../../design/nativeControls';
import { InstructionsDialog, NameDialog, SourcesDialog, type PlaceSourceRow, type SourceAsk } from './PlaceDialogs';

// Specimen only: the place names, paths and refusals below are fixtures for the Design system page. Nothing here reaches
// the engine, and none of it is ever shown as a person's place.

const wait = (ms = 400) => new Promise<void>(resolve => setTimeout(resolve, ms));
const refuse = async (message: string) => { await wait(); throw new Error(message); };
let specimenSourceSeq = 0;

const specimenSources: PlaceSourceRow[] = [
  { id: 'specimen-src-parse', kind: 'folder', label: 'codeaf/internal/parse', state: 'ok' },
  { id: 'specimen-src-notes', kind: 'file', label: 'release-notes.md', state: 'missing' },
  { id: 'specimen-src-site', kind: 'url', label: 'https://codeaf.dev' },
];

type Open = 'instructions' | 'instructions-refused' | 'sources-desktop' | 'sources-browser' | 'rename' | 'create' | undefined;

/** The three Places sheets with fixture data: each one saves after a short wait, and the "refused" variants show how an
 * engine refusal keeps the sheet open with the person's words in it. */
export function PlaceDialogsSpecimen() {
  const [open, setOpen] = useState<Open>();
  const [instructions, setInstructions] = useState('Keep the parser strict. Tolerance lives in the lexer.');
  const [sources, setSources] = useState<PlaceSourceRow[]>(specimenSources);
  const [name, setName] = useState('Config parser');
  const close = () => setOpen(undefined);

  const add = async (asks: SourceAsk[]) => {
    await wait();
    if (asks.some(ask => ask.ref.includes('refuse'))) throw new Error('That path is outside every folder codeaf may read.');
    setSources(previous => [...previous, ...asks.map(ask => ({ id: `specimen-src-${++specimenSourceSeq}`, kind: ask.kind, label: ask.ref }))]);
  };
  const remove = async (id: string) => { await wait(); setSources(previous => previous.filter(source => source.id !== id)); };
  const pickFolder = async (): Promise<PickResult> => { await wait(); return { status: 'picked', paths: [{ path: '/specimen/reports/q3', name: 'q3' }] }; };
  const pickFiles = async (): Promise<PickResult> => { await wait(); return { status: 'busy' }; };

  return <Surface direction="column" data-testid="place-dialogs-specimen">
    <SectionHeading>Place sheets</SectionHeading>
    <Text>Specimen. Instructions, files and links, and a name, as the Places shell asks for them. Type a path containing “refuse” to see a refusal; Choose files answers that another chooser is open.</Text>
    <div className="place-dialog-pickers">
      <Button variant="quiet" onClick={() => setOpen('instructions')}>Instructions</Button>
      <Button variant="quiet" onClick={() => setOpen('instructions-refused')}>Instructions, refused</Button>
      <Button variant="quiet" onClick={() => setOpen('sources-desktop')}>Files and links (desktop)</Button>
      <Button variant="quiet" onClick={() => setOpen('sources-browser')}>Files and links (browser)</Button>
      <Button variant="quiet" onClick={() => setOpen('rename')}>Rename</Button>
      <Button variant="quiet" onClick={() => setOpen('create')}>New place</Button>
    </div>
    <InstructionsDialog open={open === 'instructions'} placeName={name} initial={instructions} onClose={close}
      onSave={async text => { await wait(); setInstructions(text); }}/>
    <InstructionsDialog open={open === 'instructions-refused'} placeName={name} initial={instructions} onClose={close}
      onSave={() => refuse('Someone else changed these instructions. Reopen to see their version.')}/>
    <SourcesDialog open={open === 'sources-desktop' || open === 'sources-browser'} placeName={name} sources={sources} native={open === 'sources-desktop'}
      onPickFolder={pickFolder} onPickFiles={pickFiles} onAdd={add} onRemove={remove} onClose={close}/>
    <NameDialog open={open === 'rename'} title={`Rename “${name}”`} label="Name" initial={name} submitLabel="Rename" onClose={close}
      onSubmit={async next => { await wait(); setName(next); }}/>
    <NameDialog open={open === 'create'} title="New place" label="Name" initial="" submitLabel="Create" onClose={close}
      onSubmit={() => refuse('A place called that already exists here.')}/>
  </Surface>;
}

/** Every toast the shell draws, with fixture sentences: the two from the design, a refusal, and an Undo that fails. */
export function ToastSpecimen() {
  // The specimen posts to a channel of its own, so pressing a button here never draws in the window's region.
  const [channel] = useState(() => createToasts());
  const { show } = useToasts(channel);
  const noop = async () => { await wait(); };
  return <Surface direction="column" data-testid="toast-specimen">
    <SectionHeading>Toasts</SectionHeading>
    <Text>Specimen. Bottom-centre for six seconds, held while hovered or focused; Escape inside one dismisses it.</Text>
    <div className="place-dialog-pickers">
      <Button variant="quiet" onClick={() => show({ text: 'Config stack closed and still running', subject: 'Config stack',
        actions: [{ label: 'Stop it', onSelect: noop }], undo: noop })}>Closed running work</Button>
      <Button variant="quiet" onClick={() => show({ text: 'Archived 6 tabs idle for more than 12h',
        actions: [{ label: 'Review', onSelect: noop }, { label: 'Restore all', primary: true, onSelect: noop }] })}>Auto-archive</Button>
      <Button variant="quiet" onClick={() => show({ text: 'Moved “Q3 report” into Reports', subject: 'Reports',
        undo: () => refuse('Q3 report has moved again since; nothing was undone.') })}>Undo that is refused</Button>
      <Button variant="quiet" onClick={() => show({ tone: 'warning', text: 'Reports cannot go inside one of its own places', subject: 'Reports', actions: [] })}>Refusal</Button>
    </div>
    <ToastRegion channel={channel}/>
  </Surface>;
}
