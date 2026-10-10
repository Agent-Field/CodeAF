import { useState } from 'react';
import { SectionHeading, Surface, Text } from '../../../components/ui';
import { RailRow } from '../rail/RailRow';
import { UsingChip, type UsingChipProps } from '../using/UsingChip';
import { HomeSpecimen } from './HomeSpecimen';
import { PlacesComponentsSpecimen } from './PlacesComponentsSpecimen';
import '../rail/rail-places.css';
import './places-specimen.css';

const noop = () => {};
const railStates = [
  { label: 'Typical', row: { name: 'Marketing', tint: 'rose', parentName: 'codeaf', meta: 3 } },
  { label: 'Selected', row: { name: 'Marketing', tint: 'rose', active: true } },
  { label: 'Hover shows close', row: { name: 'Config parser', tint: 'tide', close: { onClose: noop, tabs: 2, shortcut: '⌘⇧W' } }, hover: true },
  { label: 'Needs you', row: { name: 'Reading', tint: 'iris', status: 'waiting', statusLabel: '2 need you in Reading' } },
  { label: 'Failed', row: { name: 'Nightly', tint: 'sand', status: 'failed', statusLabel: 'A task failed in Nightly' } },
  { label: 'Closed, still running', row: { name: 'Background', tint: 'sage', closedButBusy: true } },
  { label: 'Now', row: { name: 'Now', tint: 'graphite', meta: 3 } },
] as const;
const chips: { label: string; props: Omit<UsingChipProps, 'open' | 'onToggle'> }[] = [
  { label: 'Count line', props: { text: 'Using 3 places · 4 sources', tone: 'quiet' } },
  { label: 'One place', props: { text: 'Using 1 place', tone: 'quiet', place: { name: 'Marketing', tint: 'rose' } } },
  { label: 'A choice waits', props: { text: 'Using 2 places', tone: 'attention', mark: 'A choice waits on you' } },
  { label: 'Failed', props: { text: 'Could not read what this chat uses', tone: 'failed' } },
];

/** Every Places specimen on the Design system page: primitives, rail place rows, the Using chip and Home. Specimen only:
 * names and counts are fixtures, and the rail rows are inert so the page's tab order stays the page's own. */
export function PlacesSpecimen() {
  const [open, setOpen] = useState(false);
  return <Surface direction="column">
    <SectionHeading>Places</SectionHeading>
    <Text>Specimen. Tints, tiles, rows and cards in every state; the fixtures are never a person's places.</Text>
    <PlacesComponentsSpecimen/>
    <div className="places-specimen-rail" data-testid="places-rail-states">
      {railStates.map(({ label, row, ...rest }) => <div key={label} className="places-specimen-rail-cell rail">
        <small>{label}</small>
        <div inert aria-hidden="true" data-hover={'hover' in rest || undefined}><RailRow active={false} {...row}/></div>
      </div>)}
      <div className="places-specimen-rail-cell rail"><small>Empty</small><Text>Nothing here yet</Text></div>
    </div>
    <div className="places-specimen-chips" data-testid="places-using-chips">
      {chips.map(({ label, props }) => <div key={label} className="places-specimen-rail-cell"><small>{label}</small><UsingChip {...props} open={open} onToggle={() => setOpen(value => !value)}/></div>)}
    </div>
    <HomeSpecimen scenario="place"/>
  </Surface>;
}
