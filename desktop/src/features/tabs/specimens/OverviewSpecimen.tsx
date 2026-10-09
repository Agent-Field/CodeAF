import { SectionHeading, Text } from '../../../components/ui';
import type { TabSummary } from '../../conversation/tabSummary';
import type { Tab } from '../model';
import { OverviewCard } from '../OverviewCard';
import './overview-specimen.css';

const tab = (id: string, title: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title, draft: '', pinned: false, ...over });
const now = Date.now();
// Specimen fixtures only: nothing here is a live session.
const summaries: Record<string, TabSummary> = {
  a: { title: '', firstLine: '', digest: 'I split it into two groups. The fixtures and the v1 port run now, and the changelog waits on the fixtures.', mark: 'working', updatedAt: now },
  b: { title: '', firstLine: '', digest: 'Allow 3 git actions? checkout release/v1, cherry-pick 3f2a1c, push origin.', mark: 'waiting', updatedAt: now - 120_000 },
  c: { title: '', firstLine: '', digest: 'The build failed on the lexer tests.', mark: 'failed', updatedAt: now - 3_600_000 },
};
const noop = () => {};

/** Design 3h cards in their states: active ring, working, needs you, failed, empty and a split. */
export function OverviewSpecimen() {
  const cards: { tab: Tab; active?: boolean; cursor?: boolean }[] = [
    { tab: tab('a', 'Config stack'), active: true },
    { tab: tab('b', 'Port fix to v1 branch', { kind: 'task' }), cursor: true },
    { tab: tab('c', 'Update fixtures') },
    { tab: tab('d', 'Models') },
    { tab: tab('e', 'Config · Fixtures', { split: { layout: '1x2', focus: 0, panes: [tab('e1', 'Config'), tab('e2', 'Fixtures', { kind: 'task' })] } }) },
  ];
  return <div className="overview-specimen">
    <SectionHeading>Overview cards</SectionHeading>
    <Text>Readable cards, never miniature screenshots. Active tab: 2px accent ring. Cursor and hover: a fill. Close shows on hover.</Text>
    <div className="overview-specimen-grid">
      {cards.map(({ tab: item, active, cursor }) => <OverviewCard key={item.id} tab={item} summaries={summaries} now={now} active={!!active} cursor={!!cursor} onOpen={noop} onBackground={noop} onClose={noop}/>)}
    </div>
  </div>;
}
