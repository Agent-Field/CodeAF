import { SectionHeading, Surface, Text } from '../../../components/ui';
import type { PlaceRowModel } from '../../places/shell/contracts';
import { PlaceRail, type PlaceRailActions } from '../PlaceRail';
import { RailToggle } from '../RailToggle';
import './rail-specimen.css';

const noop = () => {};
const actions: PlaceRailActions = { go: noop, newWindow: noop, quickLook: noop, pin: noop, unpin: noop, rename: noop, setTint: noop, close: noop, closeAll: noop, closeOthers: noop, fileChats: noop };
// Specimen data, the designer's names from Places 6a / 10a. Never shown in the live rail.
const place = (id: string, name: string, tint: PlaceRowModel['tint'], over: Partial<PlaceRowModel> = {}): PlaceRowModel => ({ id, name, tint, pinned: false, archived: false, ...over });
const pinned = [place('specimen-codeaf', 'codeaf', 'tide', { pinned: true }), place('specimen-personal', 'Personal', 'sand', { pinned: true })];
const open = [
  place('specimen-config', 'Config parser', 'tide', { parentName: 'codeaf', status: 'waiting', statusLabel: '2 need you in Config parser' }),
  place('specimen-marketing', 'Marketing', 'rose', { parentName: 'codeaf' }),
  place('specimen-q3', 'Q3 report', 'sage', { parentName: 'Reports', closedButBusy: true }),
];

/** The Places rail as drawn in the window (232px on the frame), and the toggle in both places. Specimen only: a picture of the rail, inert and hidden from assistive technology so the real rail is the only one that is named. */
export function RailSpecimen() {
  return <Surface direction="column">
    <SectionHeading>Rail</SectionHeading>
    <Text>Specimen. 232px, no fill of its own (the frame shows through). Now, then Pinned and Open places with their tint square and one status dot, then All places. The open row reads like the active tab; a closed place still running stays muted. The toggle is ink-3, 26px in the rail and 30px with a hairline in the strip.</Text>
    <div className="rail-specimen" inert aria-hidden="true" data-rail-specimen>
      <div className="rail-specimen-frame"><PlaceRail inert={false} peeking={false} onToggle={noop}
        now={{ active: false, shortcut: '⌃0', onGo: noop }}
        sections={{ pinned, open }} current="specimen-config"
        allPlaces={{ active: false, shortcut: '⌘⇧P', onOpen: noop }} actions={actions} slotShortcut={index => `⌃${index}`} closeShortcut="⌘⇧W" newWindowShortcut="⌘↵"/></div>
      <div className="rail-specimen-strip"><RailToggle placement="strip" collapsed onClick={noop}/></div>
    </div>
  </Surface>;
}
