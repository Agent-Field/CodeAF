import { Button, Icon, SectionHeading } from '../../components/ui';
import { FileChips } from './FileChip';
import { decidedBy, metaLine } from './model';
import type { HistoryDetail } from './types';

type Props = {
  detail: HistoryDetail; now: number;
  /** Continue reopens the conversation as a live tab; a command-click asks for a new one. */
  onContinue: (event: { newTab: boolean }) => void;
  /** Absent when the conversation is already being read. */
  onRead?: (event: { newTab: boolean }) => void;
  /** Present only in the narrow layout, where the recap replaces the list. */
  onBack?: () => void;
  /** The recap card's own focus target, so a keyboard user can land on it. */
  headingId?: string;
};

/** The living recap (design 4a): What we discussed, Decided, Outcome, then Continue and Read conversation. */
export function RecapPane({ detail, now, onContinue, onRead, onBack, headingId }: Props) {
  const { item, recap } = detail;
  const files = recap?.files.length ? recap.files : item.files;
  const press = (act: (event: { newTab: boolean }) => void) => (event: { metaKey: boolean; ctrlKey: boolean }) => act({ newTab: event.metaKey || event.ctrlKey });
  return <aside className="history-recap" data-scroll-key="history-recap" aria-label="Recap">
    <div className="history-recap-head">
      {onBack && <Button className="history-back" variant="ghost" onClick={onBack}><Icon name="back" size="xs"/>History</Button>}
      <span className="history-recap-meta">{metaLine(item, now)}</span>
      <SectionHeading id={headingId} className="history-recap-title">{item.title}</SectionHeading>
    </div>
    {recap?.discussed && <section className="history-recap-section" aria-label="What we discussed">
      <h3 className="history-recap-label">What we discussed</h3>
      <p className="history-recap-body">{recap.discussed}</p>
    </section>}
    {recap && recap.decided.length > 0 && <section className="history-recap-section history-recap-decided" aria-label="Decided">
      <h3 className="history-recap-label">Decided</h3>
      <ul className="history-decisions">{recap.decided.map((decision, index) => <li key={`${index}:${decision.text}`} className="history-decision">
        <Icon name="check" size="micro"/>
        <span>{decision.text}<span className="history-decision-by"> · {decidedBy(decision.by, decision.how)}</span></span>
      </li>)}</ul>
    </section>}
    {(recap?.outcome || files.length > 0) && <section className="history-recap-section" aria-label="Outcome">
      <h3 className="history-recap-label">Outcome</h3>
      {recap?.outcome && <p className="history-recap-body history-recap-outcome">{recap.outcome}</p>}
      {files.length > 0 && <FileChips files={files} limit={8}/>}
    </section>}
    <div className="history-recap-actions">
      <Button variant="primary" className="history-action" onClick={press(onContinue)}>Continue</Button>
      {onRead && <Button variant="quiet" className="history-action" onClick={press(onRead)}>Read conversation</Button>}
    </div>
  </aside>;
}
