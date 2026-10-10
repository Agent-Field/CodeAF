import { SectionHeading, Text } from '../../../components/ui';
import type { EngineQuestion } from '../../chat/engine-client';
import type { TabSummary } from '../../conversation/tabSummary';
import type { Tab } from '../model';
import { OverviewCard } from '../OverviewCard';
import './overview-specimen.css';

const tab = (id: string, title: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title, draft: '', pinned: false, ...over });
const now = Date.now();
// Specimen fixtures only: nothing here is a live session.
const permission = (id: number, head: string): EngineQuestion => ({ id, kind: 'consent', ask: 'permission', head, options: [{ key: 'y', label: 'allow' }, { key: 'n', label: 'deny', safe: true }], blocking: { tasks: ['b'] } });
const summaries: Record<string, TabSummary> = {
  a: { title: '', firstLine: '', digest: 'I split it into two groups. The fixtures and the v1 port run now, and the changelog waits on the fixtures.', mark: 'working', running: 4, updatedAt: now },
  b: { title: '', firstLine: '', digest: '', mark: 'waiting', updatedAt: now - 120_000, questions: [permission(1, 'checkout release/v1'), permission(2, 'cherry-pick 3f2a1c'), permission(3, 'push origin')], taskState: { b: 'Running' } },
  c: { title: '', firstLine: '', digest: 'The build failed on the lexer tests.', mark: 'failed', updatedAt: now - 3_600_000 },
};
const noop = () => {};

/** Design 3h cards in their states: active ring, working, needs you, failed, empty and a split. */
export function OverviewSpecimen() {
  const cards: { tab: Tab; active?: boolean; cursor?: boolean }[] = [
    { tab: tab('a', 'Config stack'), active: true },
    { tab: tab('b', 'Port fix to v1 branch', { kind: 'task', route: { taskId: 'b', back: [''], forward: [] } }), cursor: true },
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
