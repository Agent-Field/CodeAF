import { SectionHeading, Surface, Text } from '../../../components/ui';
import { NowRow } from '../NowRow';
import '../rail.css';
import '../place-rail.css';
import './rail-specimen.css';

const noop = () => {};
const never = () => false;
// Specimen data (Shell 3a: "Now 3"). Never shown in the live rail.
const states = [
  { label: 'Rest', now: { active: false, count: 3 } },
  { label: 'Hover', now: { active: false, count: 3, hover: true } },
  { label: 'Selected', now: { active: true, count: 3 } },
  { label: 'Needs you', now: { active: false, count: 3, status: 'waiting' as const, statusLabel: '2 need you in Now' } },
  { label: 'Nothing running', now: { active: false } },
];

/** The Now row in each state it is drawn in, on the frame. Specimen only: inert and hidden from assistive technology. There is no Inbox row to show (Iteration 2 I2.1). */
export function RailRowsSpecimen() {
  return <Surface direction="column">
    <SectionHeading>Rail rows</SectionHeading>
    <Text>Specimen. A 32px row, 13px ink-2 with a 14px ink-3 glyph. Hover is the tab-hover fill and ink; selected is the tab fill with sh-1, ink and medium weight; focus is a ring on keyboard only. Now shows its count in ink-3 and nothing at 0; an amber dot, with a tooltip in words, shows when something needs you.</Text>
    <div className="rail-specimen" inert aria-hidden="true" data-rail-rows-specimen>
      {states.map(({ label, now }) => <div key={label} className="rail-specimen-frame rail">
        <small>{label}</small>
        <NowRow now={{ shortcut: '⌃0', onGo: noop, ...now }} primaryClick={never}/>
      </div>)}
    </div>
  </Surface>;
}
