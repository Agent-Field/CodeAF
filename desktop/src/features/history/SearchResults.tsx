import type { ReactNode } from 'react';
import { Button, Icon, type IconName } from '../../components/ui';
import { FileChips } from './FileChip';
import { fullStamp, highlight, queryTerms, resultStamp } from './model';
import type { BestMatch, DecisionHit, DiscussHit, FileHit, HistoryItem, SearchResult, TaskHit } from './types';

/** Text with the search terms wrapped in the soft accent mark. */
export function Marked({ text, terms }: { text: string; terms: readonly string[] }) {
  return <>{highlight(text, terms).map((part, index) => part.mark ? <mark key={index} className="history-mark">{part.text}</mark> : <span key={index}>{part.text}</span>)}</>;
}

type Press = { newTab: boolean };
type Handlers = {
  /** Opens the conversation's recap in the browse layout. */
  onRecap: (id: string, press: Press) => void;
  /** Opens the conversation read-only at a message. */
  onJump: (id: string, messageIndex: number | undefined, press: Press) => void;
  onContinue: (item: HistoryItem, press: Press) => void;
};

export function BestMatchCard({ best, now, terms, onRecap, onJump, onContinue }: Handlers & { best: BestMatch; now: number; terms: readonly string[] }) {
  const press = (act: (how: Press) => void) => (event: { metaKey: boolean; ctrlKey: boolean }) => act({ newTab: event.metaKey || event.ctrlKey });
  return <section className="history-best" aria-label="Best match">
    <div className="history-best-head"><Icon name="sparkles" size="micro"/><span className="history-best-label">Best match</span><span className="history-best-stamp">{fullStamp(Date.parse(best.item.at), now)}</span></div>
    <h3 className="history-best-title">{best.item.title}</h3>
    <p className="history-best-answer"><Marked text={best.answer} terms={terms}/></p>
    <div className="history-best-foot">
      {best.item.files.length > 0 ? <FileChips files={best.item.files}/> : <span/>}
      <span className="history-best-actions">
        <Button variant="primary" className="history-action" onClick={press(how => onRecap(best.item.id, how))}>Read recap</Button>
        {best.messageIndex !== undefined ? <Button variant="quiet" className="history-action" onClick={press(how => onJump(best.item.id, best.messageIndex, how))}>Jump to message</Button>
          : <Button variant="quiet" className="history-action" onClick={press(how => onContinue(best.item, how))}>Continue</Button>}
      </span>
    </div>
  </section>;
}

function Group({ label, count, children }: { label: string; count: number; children: ReactNode }) {
  return <section className="history-results-group" aria-label={label}>
    <h3 className="history-results-heading">{label}<span>{count}</span></h3>
    {children}
  </section>;
}

type HitProps = { icon: IconName; title: ReactNode; line: ReactNode; stamp: string; onOpen: () => void };
function Hit({ icon, title, line, stamp, onOpen }: HitProps) {
  return <div className="history-result" role="button" tabIndex={0} onClick={onOpen} onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onOpen(); } }}>
    <span className="history-lead"><Icon name={icon} size="xs"/></span>
    <div className="history-row-main"><span className="history-result-title">{title}</span><span className="history-row-line">{line}</span></div>
    <span className="history-row-stamp">{stamp}</span>
  </div>;
}

const decisionHit = (hit: DecisionHit, terms: readonly string[], now: number, h: Handlers) =>
  <Hit key={`${hit.id}:${hit.title}`} icon="check" title={<Marked text={hit.title} terms={terms}/>} line={<Marked text={hit.context} terms={terms}/>} stamp={resultStamp(Date.parse(hit.at), now)} onOpen={() => h.onRecap(hit.id, { newTab: false })}/>;
const discussHit = (hit: DiscussHit, terms: readonly string[], now: number, h: Handlers) =>
  <Hit key={`${hit.id}:${hit.messageIndex ?? hit.title}`} icon="tab" title={<Marked text={hit.title} terms={terms}/>} line={<Marked text={hit.snippet} terms={terms}/>} stamp={resultStamp(Date.parse(hit.at), now)} onOpen={() => h.onJump(hit.id, hit.messageIndex, { newTab: false })}/>;
const fileHit = (hit: FileHit, terms: readonly string[], now: number, h: Handlers) =>
  <Hit key={hit.path} icon="fileCode" title={<Marked text={hit.path} terms={terms}/>} line={`changed in ${hit.conversations} ${hit.conversations === 1 ? 'conversation' : 'conversations'} · last ${resultStamp(Date.parse(hit.last), now).toLowerCase()}`} stamp={resultStamp(Date.parse(hit.last), now)} onOpen={() => { if (hit.ids?.[0]) h.onRecap(hit.ids[0], { newTab: false }); }}/>;
const taskHit = (hit: TaskHit, terms: readonly string[], now: number, h: Handlers) =>
  <Hit key={`${hit.id}:${hit.taskId}`} icon="tasks" title={<Marked text={hit.title} terms={terms}/>} line={<><span>Task in {hit.conversationTitle}</span>{hit.snippet && <> · <Marked text={hit.snippet} terms={terms}/></>}</>} stamp={resultStamp(Date.parse(hit.at), now)} onOpen={() => h.onRecap(hit.id, { newTab: false })}/>;

/** Results grouped by what a person came for (design 4b): the best match first, then Decisions, Discussed, Files and Tasks. */
export function SearchResults({ result, now, ...handlers }: Handlers & { result: SearchResult; now: number }) {
  const terms = result.best?.terms?.length ? result.best.terms : queryTerms(result.query);
  const { counts } = result;
  const any = result.best || result.decisions.length || result.discussed.length || result.files.length || result.tasks.length;
  if (!any) return <p className="history-empty">Nothing matches “{result.query}”.</p>;
  return <div className="history-results">
    {result.best && <BestMatchCard best={result.best} now={now} terms={terms} {...handlers}/>}
    {result.decisions.length > 0 && <Group label="Decisions" count={counts.decisions}>{result.decisions.map(hit => decisionHit(hit, terms, now, handlers))}</Group>}
    {result.discussed.length > 0 && <Group label="Discussed" count={counts.discussed}>{result.discussed.map(hit => discussHit(hit, terms, now, handlers))}</Group>}
    {result.files.length > 0 && <Group label="Files" count={counts.files}>{result.files.map(hit => fileHit(hit, terms, now, handlers))}</Group>}
    {result.tasks.length > 0 && <Group label="Tasks" count={counts.tasks}>{result.tasks.map(hit => taskHit(hit, terms, now, handlers))}</Group>}
  </div>;
}
