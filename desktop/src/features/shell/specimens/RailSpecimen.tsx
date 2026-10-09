import { SectionHeading, Surface, Text } from '../../../components/ui';
import { Rail, type RailItem } from '../Rail';
import { RailToggle } from '../RailToggle';
import './rail-specimen.css';

const noop = () => {};
const items: RailItem[] = [
  { label: 'Workspace', icon: 'code', active: true, onSelect: noop },
  { label: 'Activity', icon: 'activity', active: false, hover: true, onSelect: noop },
  { label: 'Settings', icon: 'sliders', active: false, onSelect: noop },
  { label: 'Design system', icon: 'grid', active: false, onSelect: noop },
];

/** The rail as drawn in the window (232px on the frame), and the toggle in both places. Specimen only: a picture of the rail, inert and hidden from assistive technology so the real rail is the only one that is named. */
export function RailSpecimen() {
  return <Surface direction="column">
    <SectionHeading>Rail</SectionHeading>
    <Text>Specimen. 232px, no fill of its own (the frame shows through). Rows are tab-shaped: rest, hover and the open one, which reads like the active tab. The toggle is ink-3, 26px in the rail and 30px with a hairline in the strip.</Text>
    <div className="rail-specimen" inert aria-hidden="true" data-rail-specimen>
      <div className="rail-specimen-frame"><Rail items={items} inert={false} paletteOpen={false} peeking={false} onToggle={noop} onSearch={noop}/></div>
      <div className="rail-specimen-strip"><RailToggle placement="strip" collapsed onClick={noop}/></div>
    </div>
  </Surface>;
}
