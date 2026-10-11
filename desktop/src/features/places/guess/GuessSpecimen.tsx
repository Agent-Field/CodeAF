import { useState } from 'react';
import { SectionHeading } from '../../../components/ui';
import { GuessPill } from './GuessPill';
import { AlsoIn, PlaceViews } from './PlaceViews';
import type { PlacesTransport } from '../client';

const places = [{ id: 'desk', name: 'Desk app' }, { id: 'papers', name: 'Papers' }];
const transport: PlacesTransport = async (path, request) => {
  if (path.endsWith('/lenses')) return { lenses: [{ name: 'by month', axis: 'month', value: '2026-10', chats: ['chat-one'] }] };
  if (path === '/places/steer') return { understood: (request.body as { sentence: string }).sentence === 'by month' };
  throw new Error('Unknown specimen request.');
};
/** Fixtures live only on the explicit specimen route, so none can be mistaken for engine data. */
export function GuessSpecimen() {
  const [selected, setSelected] = useState<string>();
  const [filter, setFilter] = useState<readonly string[]>();
  return <section className="place-guess-specimen">
    <SectionHeading>Places guess · Specimen</SectionHeading>
    <GuessPill guess={{ place: 'desk', name: 'Desk app', confidence: 80 }} choices={places} selected={selected} onSelect={setSelected}/>
    <AlsoIn text="also in Papers"/>
    <PlaceViews placeId="desk" transport={transport} onFilter={setFilter}/>
    <p role="status">{filter ? filter.join(', ') : 'All chats'}</p>
  </section>;
}
