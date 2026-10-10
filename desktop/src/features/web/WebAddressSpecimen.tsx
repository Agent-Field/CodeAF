import { useState } from 'react';
import { SectionHeading, Surface, Text } from '../../components/ui';
import { WebHeader } from './WebHeader';
import './web.css';

function Row({ state }: { state: 'rest' | 'loading' | 'editing' | 'no-forward' }) {
  const [url, setUrl] = useState('https://pkg.go.dev/encoding/json#Decoder');
  return <div className="web-address-specimen" data-state={state}>
    <Text>{state}</Text>
    <WebHeader url={url} loading={state === 'loading'} canBack canForward={state !== 'no-forward'} canReload
      initialEditing={state === 'editing'} onGo={setUrl} onStep={() => {}} onChat={() => {}} onExternal={() => {}}/>
  </div>;
}

// These fixtures never create a native page or conversation and follow the specimen's selected theme.
export function WebAddressSpecimen() {
  return <Surface direction="column" aria-label="Web address specimen">
    <SectionHeading>Web address row</SectionHeading>
    <Text>Specimen. Rest, loading, editing and no forward history. Use Light or Dark to inspect both appearances.</Text>
    {(['rest', 'loading', 'editing', 'no-forward'] as const).map(state => <Row key={state} state={state}/>)}
  </Surface>;
}
