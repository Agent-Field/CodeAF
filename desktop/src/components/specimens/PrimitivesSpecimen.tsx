import { useState, type ReactNode } from 'react';
import design from '../../design/tokens.json';
import { formatShortcut } from '../../design/keyboard';
import { createToasts } from '../../design/toasts';
import { Button, BreathingDot, FilterTabs, IconButton, KeyboardShortcut, QuickLook, SectionHeading, Select, Shimmer, StopGlyph, Tag, ToastRegion, ToastView, sentenceParts, useToasts } from '../ui';
import { SearchField } from '../ui/SearchField';
import { PlaceSwatch, type SwatchRole } from '../../features/places/components/PlaceSwatch';
import './primitives-specimen.css';

const sixMs = design.interaction.toastDuration;
const tenMs = design.interaction.toastUndoDuration;
const choices = [
 { value: 'alpha', label: 'Alpha' },
 { value: 'beta', label: 'Beta' },
 { value: 'gamma', label: 'Gamma', disabled: true },
];
const filters = [
 { value: 'all', label: 'All' },
 { value: 'open', label: 'Open' },
 { value: 'files', label: 'Files', disabled: true },
];
const swatchRoles: SwatchRole[] = ['rail', 'tile', 'sheet', 'title', 'card', 'choice', 'menu'];
const noop = () => {};

function Group({ state, title, children }: { state: string; title: string; children: ReactNode }) {
 return <div className="primitives-specimen-group" data-specimen-state={state}>
  <span className="primitives-specimen-label">{title}</span>
  <div className="primitives-specimen-row">{children}</div>
 </div>;
}

/** Fixtures for the Design system page. Nothing here is a person's work, and the toast channel is not the window's. */
export function PrimitivesSpecimen() {
 const [channel] = useState(() => createToasts());
 const toasts = useToasts(channel);
 const [look, setLook] = useState(false);
 const [choice, setChoice] = useState({ rest: 'alpha', selected: 'beta', focus: 'alpha', narrow: 'beta' });
 const [query, setQuery] = useState({ rest: '', focus: '', narrow: '' });
 const [filter, setFilter] = useState({ rest: 'all', selected: 'open', focus: 'all', narrow: 'all' });
 const pick = (key: keyof typeof choice) => (value: string) => setChoice(current => ({ ...current, [key]: value }));
 const type = (key: keyof typeof query) => (value: string) => setQuery(current => ({ ...current, [key]: value }));
 const choose = (key: keyof typeof filter) => (value: string) => setFilter(current => ({ ...current, [key]: value }));
 const showFor = (ms: number, text: string) => toasts.show({ text, subject: 'Specimen', undo: noop, durationMs: ms });
 return <section className="primitives-specimen" aria-label="Shared primitives">
  <SectionHeading>Shared primitives</SectionHeading>
  <p>Specimen. Fixtures only. These toasts, the Quick Look and the counts are not your work.</p>
  <Group state="rest" title="Rest">
   <Button variant="primary">Allow once</Button>
   <Button variant="raised">Always…</Button>
   <Button variant="quiet">Deny</Button>
   <Button variant="ghost">Later</Button>
   <Button variant="danger">Stop task</Button>
   <Button variant="primary" size="tray">Tray primary</Button>
   <Button variant="raised" size="tray">Tray raised</Button>
   <Button variant="quiet" size="tray">Tray quiet</Button>
   <Button variant="ghost" size="tray">Tray ghost</Button>
   <Button variant="danger" size="tray">Tray danger</Button>
   <Select label="Sample choice" value={choice.rest} onValueChange={pick('rest')} options={choices}/>
   <span className="primitives-specimen-field"><SearchField label="Search conversations" placeholder="Search" value={query.rest} onChange={type('rest')}/></span>
   <FilterTabs label="Filter conversations" options={filters} value={filter.rest} onChange={choose('rest')}/>
   <Tag tone="plain">Suggested</Tag>
   <span data-kbd="box"><KeyboardShortcut command="K" variant="box"/></span>
   <span data-kbd="inline"><KeyboardShortcut label="⌘/Ctrl K" variant="inline"/></span>
   {swatchRoles.map(role => <span className="primitives-specimen-swatch" key={role}><PlaceSwatch tint={role === 'card' ? 'graphite' : 'tide'} role={role}/>{role}</span>)}
   <span className="primitives-specimen-motion"><Shimmer>Settled step</Shimmer></span>
   <span className="primitives-specimen-motion"><Shimmer active>Running the parser tests</Shimmer></span>
   <span className="primitives-specimen-motion"><BreathingDot/>4 running</span>
   <span className="primitives-specimen-stop"><span className="primitives-specimen-ink"><StopGlyph size="md"/></span>Stop, ink</span>
   <span className="primitives-specimen-stop"><span className="primitives-specimen-ink"><StopGlyph size="sm"/></span>Stop, small</span>
   <span className="primitives-specimen-stop"><span className="primitives-specimen-accent"><StopGlyph size="md"/></span>Stop, accent</span>
   <ToastView toast={{ message: sentenceParts('Config stack closed and still running', 'Config stack'), lead: 'dot', actions: [{ label: 'Stop it', onSelect: noop }], undo: noop }}/>
  </Group>
  <Group state="hover" title="Hover hint">
   <IconButton label="Copy hint" title="Copy the step" shortcut={formatShortcut('⌘/Ctrl C')} icon="copy" iconSize="sm"/>
   <IconButton label="Edit hint" title="Edit the step" icon="pencil" iconSize="sm"/>
   <IconButton label="Close hint" title="Close and stop" shortcut={formatShortcut('⌘/Ctrl W')} icon="close" iconSize="sm"/>
  </Group>
  <Group state="selected" title="Selected">
   <Select label="Selected choice" value={choice.selected} onValueChange={pick('selected')} options={choices}/>
   <FilterTabs label="Selected filters" options={filters} value={filter.selected} onChange={choose('selected')}/>
   <span className="primitives-specimen-swatch"><PlaceSwatch tint="iris" role="choice" selected label="Iris, chosen"/>Iris</span>
   <span className="primitives-specimen-swatch"><PlaceSwatch tint="rose" role="menu" dimmed label="Rose, not chosen"/>Rose</span>
  </Group>
  <Group state="disabled" title="Disabled">
   <Button variant="quiet" disabled>Disabled control</Button>
   <Button variant="quiet" size="tray" disabled>Disabled tray</Button>
   <Button variant="primary" size="tray" loading>Loading tray</Button>
   <Select label="Disabled choice" value="alpha" onValueChange={noop} options={choices} disabled/>
   <FilterTabs label="Disabled filters" options={filters} value="all" onChange={noop} disabled/>
  </Group>
  <Group state="focus" title="Focus target">
   <Button variant="primary" data-specimen-state="focus">Focus Allow once</Button>
   <Button variant="quiet" size="tray" data-specimen-state="focus">Focus tray answer</Button>
   <Select label="Focus choice" value={choice.focus} onValueChange={pick('focus')} options={choices}/>
   <span className="primitives-specimen-field"><SearchField label="Focus search" placeholder="Search" value={query.focus} onChange={type('focus')}/></span>
   <FilterTabs label="Focus filters" options={filters} value={filter.focus} onChange={choose('focus')}/>
  </Group>
  <Group state="toast" title="Sample toasts">
   <Button variant="quiet" data-toast-ms={sixMs} onClick={() => showFor(sixMs, 'Specimen notice for six seconds')}>Show a 6 second toast</Button>
   <Button variant="quiet" data-toast-ms={tenMs} onClick={() => showFor(tenMs, 'Specimen notice for ten seconds')}>Show a 10 second toast</Button>
  </Group>
  <Group state="quick-look" title="Quick Look">
   <Button variant="quiet" onClick={() => setLook(true)}>Open Quick Look</Button>
  </Group>
  <div className="primitives-specimen-group" data-specimen-state="narrow">
   <span className="primitives-specimen-label">Narrow container</span>
   <div className="primitives-specimen-narrow">
    <div className="primitives-specimen-row">
     <Button variant="primary">Narrow allow</Button>
     <Button variant="quiet" size="tray">Narrow tray</Button>
     <Button variant="quiet" disabled>Narrow disabled</Button>
     <IconButton label="Narrow copy" title="Copy the step" icon="copy" iconSize="sm"/>
     <Select label="Narrow choice" value={choice.narrow} onValueChange={pick('narrow')} options={choices}/>
     <span className="primitives-specimen-field"><SearchField label="Narrow search" placeholder="Search" value={query.narrow} onChange={type('narrow')}/></span>
     <FilterTabs label="Narrow filters" options={[{ value: 'all', label: 'All' }, { value: 'files', label: 'Files' }]} value={filter.narrow} onChange={choose('narrow')}/>
     <Tag tone="plain">Suggested</Tag>
     <span data-kbd="box"><KeyboardShortcut command="K" variant="box"/></span>
     <span data-kbd="inline"><KeyboardShortcut label="⌘/Ctrl K" variant="inline"/></span>
     {swatchRoles.map(role => <PlaceSwatch key={role} tint="tide" role={role}/>)}
     <Shimmer active>Running</Shimmer>
     <BreathingDot/>
     <span className="primitives-specimen-ink"><StopGlyph/></span>
     <ToastView toast={{ message: ['Held'], lead: 'dot', undo: noop }}/>
     <Button variant="quiet" onClick={() => setLook(true)}>Narrow Quick Look</Button>
    </div>
   </div>
  </div>
  <QuickLookSheet open={look} onClose={() => setLook(false)}/>
  <ToastRegion channel={channel}/>
 </section>;
}

function QuickLookSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
 return <QuickLook open={open} onClose={onClose} title="Reading" tint="iris" footer={<Button variant="primary" onClick={onClose}>Go to Reading</Button>}>
  <span>Last week you finished the Raft notes and started on Spanner.</span>
 </QuickLook>;
}
