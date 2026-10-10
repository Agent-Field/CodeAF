import { useState } from 'react';
import { SectionLabel, Text, type MenuEntry } from '../../../components/ui';
import { useDropTarget, usePointerDrag } from '../../../components/ui/usePointerDrag';
import { AttentionList, AttentionRow } from '../components/AttentionRow';
import { ChatList, ChatRow } from '../components/ChatRow';
import { PlaceHeading } from '../components/PlaceHeading';
import { PlaceSwatch, placeTints, tintLabel, type SwatchRole, type TintName } from '../components/PlaceSwatch';
import { PlaceTile, PlaceTileGrid } from '../components/PlaceTile';
import './places-components-specimen.css';

const noop = () => {};
const swatchRoles: readonly SwatchRole[] = ['rail', 'tile', 'sheet', 'title', 'card', 'choice', 'menu'];
const looks = [{ label: 'Rest' }, { label: 'Hover', appearance: 'hover' as const }, { label: 'Pressed', appearance: 'pressed' as const }, { label: 'Focus', appearance: 'focus' as const }];

/** A live tile. A chat dropped on it reports the chat id; the tile itself is not picked up. */
function SpecimenTile({ tile, selected, dropOn, setSelected, setDropOn, say, menu }: {
  tile: { id: string; name: string; tint: TintName; meta: string; status?: 'waiting'; tintSource?: 'own' | 'inherited' };
  selected: boolean; dropOn: boolean; setSelected: (id: string) => void; setDropOn: (id: string | undefined) => void;
  say: (line: string) => void; menu: MenuEntry[];
}) {
  const dropRef = useDropTarget({
    kind: 'place-tile',
    id: tile.id,
    accepts: (payload) => payload.kind === 'chat',
    hover: () => setDropOn(tile.id),
    leave: () => setDropOn(undefined),
    drop: (_point, payload) => {
      setDropOn(undefined);
      say(`${tile.id}:drop:${payload.id || 'none'}`);
      return true;
    },
  });
  return <PlaceTile ref={dropRef} id={tile.id} name={tile.name} tint={tile.tint} tintSource={tile.tintSource} meta={tile.meta} status={tile.status}
    selected={selected} dropTarget={dropOn}
    onGoTo={() => { setSelected(tile.id); say(`${tile.id}:goTo`); }} onOpenInNewWindow={() => say(`${tile.id}:newWindow`)} onQuickLook={() => { setSelected(tile.id); say(`${tile.id}:quickLook`); }}
    menu={menu}/>;
}

/** The one chat a person can carry onto a live tile. */
function SpecimenChat({ say, menu }: { say: (line: string) => void; menu: MenuEntry[] }) {
  const pointer = usePointerDrag({ payload: { kind: 'chat', id: 'specimen-c1', ids: ['specimen-c1'] } });
  return <ChatRow {...pointer} id="specimen-c1" title="Trailing commas across the config stack" excerpt="Open · 4 tasks running" status="running" timeLabel="Today" timeIso="2026-10-09T09:00:00Z" model="DS Flash" onOpen={() => say('c1:open')} onOpenInNewTab={() => say('c1:newTab')} menu={menu}/>;
}

/** Every Places primitive in every state, plus a live strip that reports each callback. Specimen only: the names, counts and
 * times below are fixtures for the Design system page and are never shown as a person's places or chats. */
export function PlacesComponentsSpecimen() {
  const [log, setLog] = useState<string[]>([]);
  const say = (line: string) => setLog(previous => [...previous.slice(-7), line]);
  const entries = (prefix: string): MenuEntry[] => [
    { id: 'go', label: 'Go to', shortcut: '↵', onSelect: () => say(`${prefix}:menu:go`) },
    { id: 'rename', label: 'Rename', onSelect: () => say(`${prefix}:menu:rename`) },
    { kind: 'separator', id: 'sep' },
    { id: 'delete', label: 'Delete…', danger: true, onSelect: () => say(`${prefix}:menu:delete`) },
  ];

  const [selected, setSelected] = useState<string>('specimen-reading');
  const [dropOn, setDropOn] = useState<string | undefined>();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [tint, setTint] = useState<TintName>('iris');
  const [renaming, setRenaming] = useState(false);
  const [title, setTitle] = useState('codeaf');
  const tiles = [
    { id: 'specimen-software', name: 'Software', tint: 'tide' as const, meta: '28 chats', status: 'waiting' as const },
    { id: 'specimen-reading', name: 'Reading', tint: 'iris' as const, meta: '3 places · 19 chats' },
    { id: 'specimen-marketing', name: 'Marketing', tint: 'rose' as const, meta: '14 chats', tintSource: 'own' as const },
    { id: 'specimen-release', name: 'Release', tint: 'tide' as const, meta: '6 chats · also in Software', tintSource: 'inherited' as const },
  ];

  return <div className="places-specimen" data-testid="places-specimen">
    <section className="places-specimen-section" aria-labelledby="ps-swatch">
      <SectionLabel id="ps-swatch">Swatch · six tints, seven roles</SectionLabel>
      <div className="places-specimen-matrix">
        {placeTints.map(tintName => <div key={tintName} className="places-specimen-cell" data-tint-row={tintName}>
          <span className="places-specimen-caption">{tintLabel[tintName]}</span>
          {swatchRoles.map(role => <PlaceSwatch key={role} tint={tintName} role={role}/>)}
        </div>)}
      </div>
      <div className="places-specimen-cell"><span className="places-specimen-caption">Picker</span>
        {placeTints.filter(item => item !== 'graphite').map(item => <PlaceSwatch key={item} tint={item} role="choice" selected={item === 'iris'} dimmed={item !== 'iris'} label={tintLabel[item]}/>)}
        {placeTints.filter(item => item !== 'graphite').map(item => <PlaceSwatch key={item} tint={item} role="menu" selected={item === 'rose'} label={tintLabel[item]}/>)}
      </div>
    </section>

    <section className="places-specimen-section" aria-labelledby="ps-tile">
      <SectionLabel id="ps-tile">Place tile · rest, hover, pressed, focus, selected, drop target, dragging, disabled, new, creating</SectionLabel>
      <div className="places-specimen-states">
        {looks.map(look => <div key={look.label} className="places-specimen-state"><PlaceTileGrid label={`${look.label} tile`}><PlaceTile id={`state-${look.label}`} name="Software" tint="tide" meta="28 chats" status="waiting" appearance={look.appearance} onGoTo={noop}/></PlaceTileGrid><span className="places-specimen-caption">{look.label}</span></div>)}
        <div className="places-specimen-state"><PlaceTileGrid label="Selected tile"><PlaceTile id="state-selected" name="Reading" tint="iris" meta="3 places · 19 chats" selected onGoTo={noop}/></PlaceTileGrid><span className="places-specimen-caption">Selected</span></div>
        <div className="places-specimen-state"><PlaceTileGrid label="Drop target tile"><PlaceTile id="state-drop" name="Marketing" tint="rose" meta="14 chats" dropTarget onGoTo={noop}/></PlaceTileGrid><span className="places-specimen-caption">Drop target</span></div>
        <div className="places-specimen-state"><PlaceTileGrid label="Dragging tile"><PlaceTile id="state-drag" name="Release" tint="tide" meta="6 chats · also in Software" dragging onGoTo={noop}/></PlaceTileGrid><span className="places-specimen-caption">Dragging</span></div>
        <div className="places-specimen-state"><PlaceTileGrid label="Disabled tile"><PlaceTile id="state-disabled" name="Personal" tint="sand" meta="8 chats" disabled onGoTo={noop}/></PlaceTileGrid><span className="places-specimen-caption">Disabled</span></div>
        <div className="places-specimen-state"><PlaceTileGrid label="Failed child tile"><PlaceTile id="state-failed" name="Reports" tint="sage" meta="47 places · 212 chats" status="failed" onGoTo={noop}/></PlaceTileGrid><span className="places-specimen-caption">Failed child</span></div>
        <div className="places-specimen-state"><PlaceTileGrid label="New place tile"><PlaceTile mode="new" onCreate={noop}/></PlaceTileGrid><span className="places-specimen-caption">New place</span></div>
        <div className="places-specimen-state"><PlaceTileGrid label="Creating tile"><PlaceTile mode="creating" name="Talks" tint="iris" onNameChange={noop} onTintChange={noop} onSubmit={noop} onCancel={noop}/></PlaceTileGrid><span className="places-specimen-caption">Creating</span></div>
      </div>
    </section>

    <section className="places-specimen-section" aria-labelledby="ps-live">
      <SectionLabel id="ps-live">Live · Space is Quick Look, ⌘ click opens a new window, drag a chat onto a tile</SectionLabel>
      <PlaceTileGrid label="Places in codeaf">
        {tiles.map(tile => <SpecimenTile key={tile.id} tile={tile} selected={selected === tile.id} dropOn={dropOn === tile.id} setSelected={setSelected} setDropOn={setDropOn} say={say} menu={entries(tile.id)}/>)}
        {creating
          ? <PlaceTile mode="creating" name={name} tint={tint} onNameChange={setName} onTintChange={setTint} invalid={name.trim().toLowerCase() === 'software'}
              onSubmit={() => { say(`create:${name.trim()}:${tint}`); setCreating(false); setName(''); }} onCancel={() => { say('create:cancel'); setCreating(false); }}/>
          : <PlaceTile mode="new" onCreate={() => { setCreating(true); say('new:open'); }}/>}
      </PlaceTileGrid>
    </section>

    <section className="places-specimen-section" aria-labelledby="ps-attention">
      <SectionLabel id="ps-attention">Attention row · needs you, running, failed</SectionLabel>
      <AttentionList label="Needing attention">
        <AttentionRow id="specimen-a1" title="Port fix to v1 branch" placeName="Config parser" status="waiting" onOpen={() => say('a1:open')} onOpenInNewTab={() => say('a1:newTab')} menu={entries('a1')}/>
        <AttentionRow id="specimen-a2" title="Update fixtures" placeName="Config parser" status="running" statusText="running · 2m" onOpen={() => say('a2:open')}/>
        <AttentionRow id="specimen-a3" title="Nightly benchmark" status="failed" onOpen={() => say('a3:open')}/>
        {looks.slice(1).map(look => <AttentionRow key={look.label} id={`specimen-${look.label}`} title={`${look.label} look`} status="waiting" appearance={look.appearance} onOpen={noop}/>)}
        <AttentionRow id="specimen-a4" title="Disabled row" status="running" disabled onOpen={noop}/>
      </AttentionList>
    </section>

    <section className="places-specimen-section" aria-labelledby="ps-chat">
      <SectionLabel id="ps-chat">Chat row · Home list</SectionLabel>
      <ChatList label="Chats">
        <SpecimenChat say={say} menu={entries('c1')}/>
        <ChatRow id="specimen-c2" title="Naming: codeaf vs CodeAF" excerpt="Decided lower-case everywhere" timeLabel="Mon" onOpen={() => say('c2:open')}/>
        <ChatRow id="specimen-c3" title="Pricing for teams" excerpt="Discussed seat vs usage. No decision yet" timeLabel="Last wk" selected onOpen={noop}/>
        <ChatRow id="specimen-c4" title="Release v2.4 waits on you for the tag, and this title is long enough to need the mask at the column's edge" excerpt="Waiting on you: Tag v2.4.1? A long recap that also runs past the edge of its column and fades under the mask" status="waiting" timeLabel="Open" onOpen={noop}/>
        <ChatRow id="specimen-c5" title="Provider failed during the last turn" status="failed" timeLabel="Tue" onOpen={noop}/>
        {looks.slice(1).map(look => <ChatRow key={look.label} id={`specimen-${look.label}`} title={`${look.label} look`} excerpt="The fill the row takes" timeLabel="11:40" appearance={look.appearance} onOpen={noop}/>)}
        <ChatRow id="specimen-c6" title="Disabled row" excerpt="Not reachable" timeLabel="Mon" disabled onOpen={noop}/>
        <ChatRow id="specimen-c7" title="Dragging row" excerpt="Being filed into a place" timeLabel="Mon" dragging onOpen={noop}/>
      </ChatList>
      <SectionLabel>Chat row · History</SectionLabel>
      <ChatList label="History" variant="history">
        <ChatRow variant="history" id="specimen-h1" title="Does JSON5 handle this?" excerpt="Yes, but it also allows comments, so it was ruled out" timeLabel="11:40" onOpen={noop}/>
        <ChatRow variant="history" id="specimen-h2" title="Fix it in the lexer" excerpt="Decided to keep strict mode as the default and fix it in the lexer" timeLabel="14:02" selected onOpen={noop}
          details={<span className="places-specimen-caption">lexer.go · 2 decisions</span>}/>
        <ChatRow variant="history" id="specimen-h3" title="Release v2.4" excerpt="Waiting on you: Tag v2.4.1?" status="waiting" timeLabel="Open" onOpen={noop}/>
      </ChatList>
    </section>

    <section className="places-specimen-section" aria-labelledby="ps-heading">
      <SectionLabel id="ps-heading">Place heading · breadcrumb, title, menu, rename</SectionLabel>
      <PlaceHeading title={title} tint="tide" renaming={renaming}
        breadcrumb={[{ id: 'root', label: 'All places', onGo: () => say('crumb:root'), onGoInNewWindow: () => say('crumb:root:newWindow') }]}
        menu={[...entries('heading'), { id: 'rename-inline', label: 'Rename inline', onSelect: () => setRenaming(true) }]}
        onRename={next => { setTitle(next); setRenaming(false); say(`rename:${next}`); }} onRenameCancel={() => setRenaming(false)}/>
      <PlaceHeading title="Marketing" tint="rose"/>
      <PlaceHeading title="A place whose name is long enough that the title has to fade under the mask before it reaches the menu button" tint="sage" menu={entries('long')}
        breadcrumb={[{ id: 'root', label: 'All places', onGo: noop }, { id: 'software', label: 'Software', onGo: noop }]}/>
      <Text>The title above is the shared 28px role. It is never the old page heading.</Text>
    </section>

    <section className="places-specimen-section" aria-labelledby="ps-log">
      <SectionLabel id="ps-log">Callbacks</SectionLabel>
      <ol className="places-specimen-log" aria-label="Callback log">{log.map((line, index) => <li key={index}>{line}</li>)}</ol>
    </section>
  </div>;
}
