import { useReducer, useRef, useState } from 'react';
import { Button, SectionHeading, Text } from '../../../components/ui';
import design from '../../../design/tokens.json';
import { GroupOffer } from '../GroupOffer';
import { offerStorageKey } from '../offerRules.ts';
import { createPreviewStore } from '../preview/previewStore';
import { workspaceReducer, type Tab, type WorkspaceState } from '../model';
import { TabStrip } from '../TabStrip';
import './group-offer-specimen.css';

/** A fixture session name no real conversation has, so deciding the specimen's offer never silences a real one. */
export const specimenSession = 'specimen/group-offer.jsonl';
const tab = (id: string, kind: Tab['kind'], title: string, over: Partial<Tab> = {}): Tab => ({ id, kind, title, titleSource: 'manual', draft: '', pinned: false, sessionFile: specimenSession, ...over });
const fixture = (): WorkspaceState => {
  const tabs = [tab('s-bench', 'conversation', 'Benchmarks', { titleSource: 'engine' }), tab('s-nightly', 'task', 'Nightly run'), tab('s-fixtures', 'task', 'Fixtures')];
  return { tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: 4, recentIds: tabs.map(t => t.id) };
};
const noop = () => {};

/** The group suggestion over the REAL strip and reducer, with a fixture of three tabs from one session. Specimen only: not live data. */
export function GroupOfferSpecimen() {
  const [round, setRound] = useState(0);
  return <Sandbox key={round} onReset={() => { try { localStorage.removeItem(offerStorageKey); } catch { /* No store, nothing remembered. */ } setRound(value => value + 1); }}/>;
}

function Sandbox({ onReset }: { onReset: () => void }) {
  const [state, dispatch] = useReducer(workspaceReducer, undefined, fixture);
  const [previews] = useState(() => createPreviewStore(design.interaction.previewCloseDelay));
  const next = useRef(1);
  const trigger = useRef<HTMLButtonElement>(null);
  const api = { state, dispatch, summaries: {}, now: 0, closeTab: (id: string) => dispatch({ type: 'close', id }), startRename: noop, receiveSummary: noop, overlayOpen: false, previews, closeAndStop: noop, closeMany: noop, isRunning: () => false, background: { running: [], needsYou: [], failed: [] }, markFailedSeen: noop, reopenClosed: noop, actions: { linkFor: () => undefined, copyLink: async () => false, canMove: () => false, moveToNewWindow: async () => false, moveToWindow: async () => false } };
  return <section className="group-offer-specimen" aria-label="Group suggestion specimen">
    <SectionHeading>Group suggestion</SectionHeading>
    <Text>Specimen. Three or more loose tabs from one session get one pill, once per launch; Group and the X are remembered for 30 days.</Text>
    <div className="group-offer-specimen-card tab-workspace">
      <TabStrip api={api} overviewTrigger={trigger} onOverview={noop}/>
      <GroupOffer api={api}/>
    </div>
    <div className="group-offer-specimen-actions">
      <Button variant="raised" onClick={() => dispatch({ type: 'open', tab: tab(`s-extra-${next.current}`, 'task', `Extra ${next.current++}`), background: true })}>Add a tab from this session</Button>
      <Button variant="raised" onClick={onReset}>Start over</Button>
    </div>
  </section>;
}
