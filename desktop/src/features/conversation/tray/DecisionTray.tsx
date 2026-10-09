import { useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '../../../components/ui';
import type { EngineAnswer } from '../../chat/engine-client';
import { bulkAnswers } from './answers';
import type { RenderImage } from './CardEvidence';
import type { Question } from './form';
import { layoutTabs, questionKey, type Tab } from './layout';
import { QuestionCardV2 } from './QuestionCardV2';
import { BulkCard, ReviewPanel } from './SetPanels';
import { TrayCompact } from './TrayCompact';
import { TrayHeader } from './TrayHeader';
import { useMoreBelow } from './useBodyFade';
import './tray.css';
import './tray-shell.css';

export type DecisionTrayProps = {
  questions: Question[];
  /** questionKey() of the answer being sent right now, or null. */
  busyKey: string | null;
  /** Sends one answer; resolves true when the engine took it. */
  onAnswer: (answer: EngineAnswer) => Promise<boolean>;
  /** Stops the question's clock. Called at most once per question. */
  onHold: (question: { kind: string; id: number; ref?: string }) => void;
  /** Epoch milliseconds, ticked by the host so countdowns are plain props. */
  now: number;
  renderImage?: RenderImage;
  /** Set to a questionKey to bring that question forward (a receipt line was clicked). */
  focusKey?: string;
  /** The reader is scrolled away from the tray: draw the 40px bar instead of the card (1b). */
  compact?: boolean;
  /** Review on the compact bar; the host brings the tray back into view. */
  onReview?: () => void;
};

function tabHolds(tab: Tab | undefined, key: string): boolean {
  if (!tab) return false;
  if (tab.kind === 'question') return questionKey(tab.question) === key;
  return tab.members.some((member) => questionKey(member) === key);
}

export function DecisionTray({ questions, busyKey, onAnswer, onHold, now, renderImage, focusKey, compact, onReview }: DecisionTrayProps) {
  const [later, setLater] = useState<Set<string>>(new Set());
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [held, setHeld] = useState<Record<string, EngineAnswer>>({});
  const [clockStopped, setClockStopped] = useState<Set<string>>(new Set());
  const [wanted, setWanted] = useState('');
  const [foldedOpen, setFoldedOpen] = useState(false);
  const bodyRef = useRef<HTMLDivElement>(null);
  const { tabs, folded } = useMemo(() => layoutTabs(questions, expanded, later), [questions, expanded, later]);

  useEffect(() => {
    if (!focusKey) return;
    setLater((before) => new Set([...before].filter((key) => key !== focusKey)));
    setWanted(focusKey);
  }, [focusKey]);

  const active = tabs.find((tab) => tab.id === wanted) ?? tabs.find((tab) => tabHolds(tab, wanted)) ?? tabs[0];
  const moreBelow = useMoreBelow(bodyRef, active?.id ?? '');

  if (!tabs.length && !folded.length) return null;
  const inSet = new Set(tabs.flatMap((tab) => (tab.kind === 'review' ? [tab.batch] : [])));
  const busy = (key: string) => busyKey === key;

  const index = tabs.findIndex((tab) => tab.id === active?.id);

  function page(step: -1 | 1) {
    setWanted(tabs[Math.max(0, Math.min(index + step, tabs.length - 1))]?.id ?? '');
  }

  function holdClock(question: Question) {
    const key = questionKey(question);
    if (clockStopped.has(key)) return;
    setClockStopped((before) => new Set(before).add(key));
    onHold({ kind: question.kind, id: question.id, ref: question.ref });
  }

  async function sendInOrder(answers: EngineAnswer[]) {
    for (const answer of answers) if (!(await onAnswer(answer))) return false;
    return true;
  }

  function commit(question: Question) {
    return (answer: EngineAnswer) => {
      setHeld((before) => ({ ...before, [questionKey(question)]: answer }));
      page(1);
      return Promise.resolve(true);
    };
  }

  function fold(question: Question) {
    setLater((before) => new Set(before).add(questionKey(question)));
  }

  function unfold(question: Question) {
    const key = questionKey(question);
    setLater((before) => new Set([...before].filter((candidate) => candidate !== key)));
    setWanted(key);
  }

  function body(tab: Tab) {
    if (tab.kind === 'bulk') {
      const busyNow = tab.members.some((member) => busy(questionKey(member)));
      return (
        <BulkCard
          members={tab.members}
          busy={busyNow}
          onAll={(role) => void sendInOrder(bulkAnswers(tab.members, role))}
          onOneByOne={() => setExpanded((before) => new Set(before).add(tab.batch))}
        />
      );
    }
    if (tab.kind === 'review') {
      return <ReviewPanel members={tab.members} held={held} onGo={setWanted} onSendAll={sendInOrder} />;
    }
    const { question } = tab;
    const key = questionKey(question);
    const batched = Boolean(question.batch) && inSet.has(question.batch!);
    return (
      <QuestionCardV2
        key={key}
        question={question}
        busy={busy(key)}
        now={now}
        held={clockStopped.has(key)}
        onAnswer={batched ? commit(question) : onAnswer}
        onHold={() => holdClock(question)}
        onLater={batched ? undefined : () => fold(question)}
        renderImage={renderImage}
      />
    );
  }

  const shown = active?.kind === 'question' ? active.question : (active?.members[0] as Question | undefined);
  const standing = tabs.filter((tab) => tab.kind !== 'review').length + folded.length;
  const nonBlocking = active?.kind === 'question' && !active.question.blocking?.turn;

  return (
    <>
      {compact && (
        <TrayCompact count={standing} summary={(active?.label ?? '').replace(/`/g, '')} onReview={() => onReview?.()} />
      )}
      <section className="decision-tray" aria-label="Waiting on you" hidden={compact}>
        <TrayHeader
          question={shown}
          index={index + 1}
          count={tabs.length}
          laterCount={folded.length}
          laterOpen={foldedOpen}
          onPage={page}
          onToggleLater={() => setFoldedOpen((open) => !open)}
        />
        {foldedOpen && folded.length > 0 && (
          <ul className="tray-folded">
            {folded.map((question) => (
              <li key={questionKey(question)} className="tray-folded-row">
                <span className="tray-review-head">{question.head}</span>
                <Button onClick={() => unfold(question)}>Answer now</Button>
              </li>
            ))}
          </ul>
        )}
        {active && (
          <div className="tray-body" ref={bodyRef} data-more={moreBelow || undefined}>
            {body(active)}
          </div>
        )}
        {nonBlocking && <p className="tray-foot">Doesn't block this reply</p>}
      </section>
    </>
  );
}
