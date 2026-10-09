import { useState } from 'react';
import { Button, SectionHeading, Surface, Text } from '../../../components/ui';
import type { ChooserMode, PlaceRowModel } from './contracts';
import { GoToChooser } from './GoToChooser';

/** Fixtures for the Design system page only: the designer's own names from Places 6c, never a person's places. The clock is
 * fixed so Recent's times read the same on every run. */
const specimenNow = new Date('2026-10-09T12:00:00Z');
const minutesAgo = (minutes: number) => new Date(specimenNow.getTime() - minutes * 60_000).toISOString();
const row = (id: string, name: string, tint: PlaceRowModel['tint'], extra: Partial<PlaceRowModel> = {}): PlaceRowModel =>
  ({ id, name, tint, pinned: false, archived: false, ...extra });

const specimenPlaces: readonly PlaceRowModel[] = [
  row('sp_codeaf', 'codeaf', 'tide', { meta: '4 inside', pinned: true, lastOpenedAt: minutesAgo(60 * 5) }),
  row('sp_software', 'Software', 'tide', { meta: '3 inside', parentName: 'codeaf', parents: ['sp_codeaf'] }),
  row('sp_lexer', 'Lexer', 'tide', { meta: '5 chats', parentName: 'Software', parents: ['sp_software'] }),
  row('sp_marketing', 'Marketing', 'rose', { meta: '2 inside', parentName: 'codeaf', parents: ['sp_codeaf'], lastOpenedAt: minutesAgo(60 * 24) }),
  row('sp_launch', 'Launch site', 'rose', { meta: '3 chats', parentName: 'Marketing', parents: ['sp_marketing'] }),
  row('sp_release', 'Release', 'tide', { meta: '6 chats', parentName: 'codeaf', parents: ['sp_codeaf', 'sp_software'] }),
  row('sp_config', 'Config parser', 'tide', { meta: '9 chats', parentName: 'codeaf', parents: ['sp_codeaf'], status: 'waiting', statusLabel: '2 need you in Config parser', lastOpenedAt: minutesAgo(2) }),
  row('sp_reports', 'Reports', 'sage', { meta: '47 inside' }),
  row('sp_q3', 'Q3 report', 'sage', { meta: '12 chats', parentName: 'Reports', parents: ['sp_reports'], lastOpenedAt: minutesAgo(60 * 24 * 3) }),
  row('sp_q2', 'Q2 report', 'sage', { meta: '9 chats', parentName: 'Reports', parents: ['sp_reports'], status: 'failed', statusLabel: 'A task failed in Q2 report' }),
  row('sp_churn', 'Churn deep-dive', 'sage', { meta: '6 chats', parentName: 'Reports', parents: ['sp_reports'] }),
  row('sp_personal', 'Personal', 'graphite', { meta: '8 chats' }),
  row('sp_reading', 'Reading', 'iris', { meta: '4 chats', parentName: 'Personal', parents: ['sp_personal'], lastOpenedAt: minutesAgo(60 * 24 * 12) }),
];
const specimenChildren: ReadonlyMap<string, readonly string[]> = new Map([
  ['root', ['sp_codeaf', 'sp_reports', 'sp_personal']],
  ['sp_codeaf', ['sp_software', 'sp_marketing', 'sp_release', 'sp_config']],
  ['sp_software', ['sp_lexer', 'sp_release']],
  ['sp_marketing', ['sp_launch']],
  ['sp_reports', ['sp_q3', 'sp_q2', 'sp_churn']],
  ['sp_personal', ['sp_reading']],
]);
const nameOf = (id: string) => specimenPlaces.find(place => place.id === id)?.name ?? id;

const modes: readonly { label: string; mode: ChooserMode }[] = [
  { label: 'Go to', mode: { kind: 'go' } },
  { label: 'Merge Marketing', mode: { kind: 'merge', placeId: 'sp_marketing', placeName: 'Marketing' } },
  { label: 'Add Software to another place', mode: { kind: 'parent', placeId: 'sp_software', placeName: 'Software' } },
  { label: 'File a chat', mode: { kind: 'file', chatIds: ['sp_chat'], chatTitle: 'Trailing commas', exclude: ['sp_config'] } },
];

/** The Go to chooser in each of its four questions. Choosing only reports what would happen; creating is refused on purpose,
 * so the refusal line inside the sheet can be seen. */
export function GoToChooserSpecimen() {
  const [mode, setMode] = useState<ChooserMode>();
  const [said, setSaid] = useState('');
  return <Surface direction="column">
    <SectionHeading>Go to a place</SectionHeading>
    <Text>Specimen: the places below are fixtures from the design, not your places. Nothing here is written.</Text>
    <div className="goto-specimen-actions">
      {modes.map(entry => <Button key={entry.label} variant="quiet" onClick={() => setMode(entry.mode)}>{entry.label}</Button>)}
    </div>
    {said && <Text>{said}</Text>}
    <GoToChooser open={mode !== undefined} mode={mode ?? { kind: 'go' }} places={specimenPlaces} childrenOf={specimenChildren} total={58} now={specimenNow}
      onChoose={id => setSaid(`Chose ${nameOf(id)}.`)}
      onChooseInNewWindow={id => setSaid(`Opened ${nameOf(id)} in a new window.`)}
      onCreate={name => Promise.reject(new Error(`Specimen: “${name}” was not created.`))}
      onClose={() => setMode(undefined)}/>
  </Surface>;
}
