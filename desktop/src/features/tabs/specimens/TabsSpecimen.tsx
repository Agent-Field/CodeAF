import { Icon, SectionHeading, Surface, Text } from '../../../components/ui';
import { GroupCapsule, MemberSlot } from '../GroupCapsule';
import { allKinds } from '../kinds/registry';
import type { TabKind } from '../kinds/types';
import { LoadingLine } from '../LoadingLine';
import { OverviewSpecimen } from './OverviewSpecimen';
import { SplitTab } from '../SplitTab';
import { Tab, type TabState } from '../Tab';
import './tabs-specimen.css';

type Row = { label: string; kind: TabKind; title: string; states: boolean };
// Design 3j, row for row: Settings and New tab have no needs-you or failed state.
const rows: Row[] = [
  { label: 'Conversation', kind: 'conversation', title: 'Config stack', states: true },
  { label: 'Task', kind: 'task', title: 'Update fixtures', states: true },
  { label: 'File', kind: 'file', title: 'lexer.go', states: true },
  { label: 'Diff', kind: 'diff', title: 'lexer.go', states: true },
  { label: 'Web', kind: 'web', title: 'encoding/json', states: true },
  { label: 'Terminal / job', kind: 'terminal', title: 'nightly-bench', states: true },
  { label: 'Settings', kind: 'settings', title: 'Models', states: false },
  { label: 'New tab', kind: 'newtab', title: 'New tab', states: false },
];
const columns = ['Kind', 'Rest', 'Hover', 'Active', 'Needs you', 'Failed'];
const noop = () => {};

function Specimen({ kind, title, active, hover, state }: { kind: TabKind; title: string; active?: boolean; hover?: boolean; state?: TabState }) {
  return <div className="tabs-specimen-cell"><Tab specimen kind={kind} title={title} active={active} hover={hover} state={state} tabIndex={-1} onClose={noop}/></div>;
}

/** Every tab kind in every state, plus pinned, group, collapsed group, split, compressed, closing and the load line. Specimen only: none of this is live data. */
export function TabsSpecimen() {
  return <Surface direction="column">
    <SectionHeading>Tabs, groups and split</SectionHeading>
    <Text>Specimen. 30px tabs, radius 8, equal width 190px compressing to 112px. Running is silent; amber or red replaces the icon only when a tab needs you or failed.</Text>
    <div className="tabs-specimen" data-tab-specimen>
      <div className="tabs-specimen-grid" role="presentation">
        {columns.map(column => <div key={column} className="tabs-specimen-head">{column}</div>)}
        {rows.map(row => [
          <div key={`${row.label}-name`} className="tabs-specimen-kind">{row.label}</div>,
          <Specimen key={`${row.label}-rest`} kind={row.kind} title={row.title}/>,
          <Specimen key={`${row.label}-hover`} kind={row.kind} title={row.title} hover/>,
          <Specimen key={`${row.label}-active`} kind={row.kind} title={row.title} active/>,
          row.states ? <Specimen key={`${row.label}-waiting`} kind={row.kind} title={row.title} state="waiting"/> : <span key={`${row.label}-w`} className="tabs-specimen-na">n/a</span>,
          row.states ? <Specimen key={`${row.label}-failed`} kind={row.kind} title={row.title} state="failed"/> : <span key={`${row.label}-f`} className="tabs-specimen-na">n/a</span>,
        ])}
      </div>
      <div className="tabs-specimen-rule"/>
      <div className="tabs-specimen-wrap">
        <span className="tabs-specimen-item">Pinned
          <Tab specimen kind="inbox" title="Inbox" pinned tabIndex={-1}/>
          <Tab specimen kind="conversation" title="Config stack" pinned active badge tabIndex={-1}/>
        </span>
        <span className="tabs-specimen-item">Group
          <GroupCapsule title="Trailing commas" count={2} collapsed={false}>
            <MemberSlot hidden={false}><Tab specimen kind="conversation" title="Config stack" active inGroup tabIndex={-1} onClose={noop}/></MemberSlot>
            <MemberSlot hidden={false}><Tab specimen kind="task" title="Fixtures" inGroup tabIndex={-1}/></MemberSlot>
          </GroupCapsule>
        </span>
        <span className="tabs-specimen-item">Collapsed group
          <GroupCapsule title="Release v2.4" count={3} collapsed needsYou><MemberSlot hidden>{null}</MemberSlot></GroupCapsule>
        </span>
        <span className="tabs-specimen-item">Split
          <SplitTab specimen active focus={0} segments={[{ id: 'a', kind: 'conversation', title: 'Config' }, { id: 'b', kind: 'task', title: 'Fixtures' }]}/>
        </span>
        <span className="tabs-specimen-item">Compressed<Tab specimen kind="terminal" title="nightly-bench" compressed tabIndex={-1}/></span>
      </div>
      <div className="tabs-specimen-rule"/>
      <span className="tabs-specimen-label">Long title fades over its last 20px · hover close · Alt turns close into stop</span>
      <div className="tabs-specimen-wrap">
        <div className="tabs-specimen-wide"><Tab specimen kind="conversation" title="Trailing commas across the config stack and env loader" tabIndex={-1} onClose={noop}/></div>
        <div className="tabs-specimen-wide"><Tab specimen kind="conversation" title="Trailing commas across the config stack and env loader" hover tabIndex={-1} onClose={noop}/></div>
        <div className="tabs-specimen-wide"><Tab specimen kind="conversation" title="Config stack" hover closeMode="stop" tabIndex={-1} onClose={noop}/></div>
      </div>
      <div className="tabs-specimen-rule"/>
      <span className="tabs-specimen-label">Web load line: 2px along the top of the card, never on the tab</span>
      <div className="tabs-specimen-card"><LoadingLine progress={0.45}/><Icon name="web" size="lg"/></div>
      <ul className="tabs-specimen-legend">{allKinds().map(def => <li key={def.kind}><Icon name={def.icon} size="sm"/><span>{def.label}</span><span className="tabs-specimen-note">{def.backed ? 'live' : 'specimen only: no engine backing yet'}</span></li>)}</ul>
      <div className="tabs-specimen-rule"/>
      <OverviewSpecimen/>
    </div>
  </Surface>;
}
