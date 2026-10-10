import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { Button } from '../../../components/ui';
import type { EngineAnswer } from '../../chat/engine-client';
import { BatchCard } from './BatchCard';
import type { RenderImage } from './CardEvidence';
import type { Question } from './form';
import { layoutTabs, questionKey, type Tab } from './layout';
import { QuestionCardV2 } from './QuestionCardV2';
import { ReviewPanel } from './SetPanels';
import { TrayCompact } from './TrayCompact';
import { TrayFold } from './TrayFold';
import { TrayHeader } from './TrayHeader';
import { useMoreBelow } from './useBodyFade';
import './tray.css';
import { nextUpWalk, useNextUpWalkState } from '../../nextup/useNextUpWalk';
import { worldStore } from '../../chat/world-store';
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

/** Free-text fields. Anything else an input can be either ignores horizontal arrows or uses them to change its own value. */
const TEXT_FIELD = new Set(['', 'text', 'search', 'email', 'url', 'tel', 'password', 'number']);

/**
 * ← → belong to the focused control when moving them would edit it.
 * A text field keeps them only once it holds characters, so an empty one still pages the tray.
 * A radio, a dial, a select or a date field keeps them always: those keys change that control.
 */
function arrowsStayWithControl(target: EventTarget | null): boolean {
  const node = target instanceof Element ? target : null;
  const field = node?.closest('input, textarea, select, [contenteditable="true"]');
  if (!field) return false;
  if (field instanceof HTMLSelectElement) return true;
  if (field instanceof HTMLTextAreaElement) return field.value.length > 0;
  if (field instanceof HTMLInputElement) {
    if (TEXT_FIELD.has(field.type)) return field.value.length > 0;
    if (field.type === 'checkbox' || field.type === 'button' || field.type === 'submit' || field.type === 'reset' || field.type === 'file' || field.type === 'image' || field.type === 'hidden' || field.type === 'color') return false;
    return true;
  }
  return (field.textContent ?? '').length > 0;
}

export function DecisionTray({ questions, busyKey, onAnswer, onHold, now, renderImage, focusKey, compact, onReview }: DecisionTrayProps) {
  const walk = useNextUpWalkState();
  const [later, setLater] = useState<Set<string>>(new Set());
  const [expanded] = useState<Set<string>>(new Set());
  const [held, setHeld] = useState<Record<string, EngineAnswer>>({});
  const [clockStopped, setClockStopped] = useState<Set<string>>(new Set());
  const [wanted, setWanted] = useState('');
  const [foldedOpen, setFoldedOpen] = useState(false);
  const bodyRef = useRef<HTMLDivElement>(null);
  const trayRef = useRef<HTMLElement>(null);
  /** Set when a key pages, so focus can return to an arrow if the control that had it unmounts. */
  const returnFocus = useRef<-1 | 1 | 0>(0);
  const { tabs, folded } = useMemo(() => layoutTabs(questions, expanded, later), [questions, expanded, later]);

  useEffect(() => {
    if (!focusKey) return;
    setLater((before) => new Set([...before].filter((key) => key !== focusKey)));
    setWanted(focusKey);
  }, [focusKey]);

  const active = tabs.find((tab) => tab.id === wanted) ?? tabs.find((tab) => tabHolds(tab, wanted)) ?? tabs[0];
  const moreBelow = useMoreBelow(bodyRef, active?.id ?? '');
  const index = tabs.findIndex((tab) => tab.id === active?.id);

  useEffect(() => {
    const step = returnFocus.current;
    if (!step) return;
    returnFocus.current = 0;
    const root = trayRef.current;
    if (!root || root.contains(document.activeElement)) return;
    const label = step > 0 ? 'Next question' : 'Previous question';
    const preferred = root.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]:not(:disabled)`);
    const fallback = root.querySelector<HTMLButtonElement>('.tray-arrow:not(:disabled)');
    (preferred ?? fallback)?.focus();
  }, [index]);

  if (!tabs.length && !folded.length) return null;
  const inSet = new Set(tabs.flatMap((tab) => (tab.kind === 'review' ? [tab.batch] : [])));
  const busy = (key: string) => busyKey === key;

  function page(step: -1 | 1) {
    setWanted(tabs[Math.max(0, Math.min(index + step, tabs.length - 1))]?.id ?? '');
  }

  // The pager arrows are the keys too (Interactions, tray pager: ← → while the tray is focused).
  // At either end the press does nothing, and the arrow there is already disabled.
  function onTrayKey(event: KeyboardEvent<HTMLElement>) {
    if (event.defaultPrevented || event.nativeEvent.isComposing) return;
    if (event.altKey || event.metaKey || event.ctrlKey || event.shiftKey) return;
    const step: -1 | 1 | 0 = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0;
    if (step === 0 || tabs.length < 2) return;
    if (arrowsStayWithControl(event.target)) return;
    const next = index + step;
    if (next < 0 || next >= tabs.length) return;
    event.preventDefault();
    returnFocus.current = step;
    page(step);
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
        <BatchCard members={tab.members} busy={busyNow} onSend={sendInOrder} />
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
        single={tabs.length === 1 && folded.length === 0}
        renderImage={renderImage}
        onSkip={walk.item && question.kind === walk.item.kind && question.id === walk.item.id ? () => nextUpWalk.skip(worldStore.getState().items) : undefined}
      />
    );
  }

  const shown = active?.kind === 'question' ? active.question : (active?.members[0] as Question | undefined);
  const standing = tabs.filter((tab) => tab.kind !== 'review').length + folded.length;

  return (
    <>
      <TrayFold shown={Boolean(compact)}>
        <TrayCompact count={standing} summary={(active?.label ?? '').replace(/`/g, '')} onReview={() => onReview?.()} />
      </TrayFold>
      <TrayFold shown={!compact}>
      <section ref={trayRef} className="decision-tray" aria-label="Waiting on you" onKeyDown={onTrayKey}>
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
            {active.kind !== 'question' && walk.item && active.members.some(question => question.kind === walk.item!.kind && question.id === walk.item!.id) && <Button className="nextup-walk-skip" onClick={() => nextUpWalk.skip(worldStore.getState().items)}>Skip</Button>}
          </div>
        )}
      </section>
      </TrayFold>
    </>
  );
}
