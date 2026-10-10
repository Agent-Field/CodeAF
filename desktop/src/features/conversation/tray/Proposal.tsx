import { useState } from 'react';
import { Button, InlineMarkdown, KeyboardShortcut, Text, TextInput } from '../../../components/ui';
import type { EngineAnswer } from '../../chat/engine-client';
import { answerFor, initialDraft } from './answers';
import { clockText } from './clock';
import { optionLabel, visibleOptions, type Option, type Question } from './form';
import './proposal.css';

export type ProposalProps = {
  question: Question;
  busy: boolean;
  now: number;
  held: boolean;
  onAnswer: (answer: EngineAnswer) => Promise<boolean>;
  onSkip?: () => void;
  onHold: () => void;
};

/** A place that is still learning proposes its answer instead of deciding it: the question carries `proposal` (or the older `learning` field), a pick that is one of its choices, and a second choice to overrule with. */
export function isProposal(question: Question): boolean {
  const options = visibleOptions(question);
  return Boolean((question.proposal ?? question.learning) && question.kind === 'ask' && options.length >= 2 && options.some((o) => o.key === question.pick?.key));
}

/** "Would choose · 92% · matches what Marketing knows"; every part the engine did not measure is left out. */
export function wouldChoose(question: Question): string {
  const place = (question.proposal ?? question.learning)?.place;
  const percent = question.pick?.percent;
  return ['Would choose', percent ? `${percent}%` : '', place ? `matches what ${place} knows` : ''].filter(Boolean).join(' · ');
}

/** The knows lines and receipts the pick was weighed against, named on hover. */
const basisHint = (question: Question) => (question.pick?.basis?.length ? `Relied on ${question.pick.basis.join(', ')}` : undefined);

/** The tray's learning-mode card: the proposed choice is lit, Agree costs one click, and taking the other answer is recorded as an overrule. */
export function Proposal({ question, busy, now, held, onAnswer, onHold, onSkip }: ProposalProps) {
  const options = visibleOptions(question);
  const proposed = options.find((o) => o.key === question.pick?.key) as Option;
  const others = options.filter((o) => o !== proposed);
  // The proposed choice starts selected; picking another only retargets the Choose button.
  const [chosen, setChosen] = useState(proposed.key);
  const other = others.find((o) => o.key === chosen) ?? others[0];
  const [failed, setFailed] = useState(false);
  const [sending, setSending] = useState(false);
  const locked = busy || sending;
  const clock = clockText(question, now);

  async function send(answer: EngineAnswer) {
    if (locked) return;
    setSending(true);
    try { setFailed(!(await onAnswer(answer))); } catch { setFailed(true); } finally { setSending(false); }
  }
  const draft = initialDraft(question);
  const agree = () => void send(answerFor(question, draft, proposed));
  const overrule = () => void send({ ...answerFor(question, draft, other), overruled: true });
  // Typing or tabbing into the card means the person is reading it: the countdown holds, as on every card.
  const stopClock = () => clock && !held && onHold();

  return (
    <section
      className="tray-card proposal"
      aria-label={question.head}
      aria-busy={locked || undefined}
      onFocusCapture={stopClock}
      onKeyDown={(event) => {
        if (event.key === 'Enter' && !event.defaultPrevented && !(event.target as HTMLElement).closest('button')) agree();
      }}
    >
      <h3 className="tray-head"><InlineMarkdown>{question.head}</InlineMarkdown></h3>
      {question.reason && <Text className="tray-reason">{question.reason}</Text>}
      <div className="proposal-choices" role="radiogroup" aria-label={question.head}>
        {options.map((option) => {
          const isProposed = option === proposed;
          return (
            <label key={option.key} className="proposal-choice" data-proposed={isProposed || undefined} data-chosen={option.key === chosen || undefined} title={isProposed ? basisHint(question) : undefined}>
              <TextInput type="radio" className="proposal-radio" name={`proposal-${question.id}`} disabled={locked} checked={option.key === chosen} onChange={() => setChosen(option.key)} />
              <span className="proposal-label">{optionLabel(question, option)}</span>
              {isProposed && <span className="proposal-knows">{wouldChoose(question)}</span>}
              {option.body && <span className="proposal-knows">{option.body}</span>}
            </label>
          );
        })}
      </div>
      <div className="tray-actions">
        <Button variant="primary" disabled={locked} onClick={agree}>Agree<span className="proposal-key" aria-hidden="true"><KeyboardShortcut label="↵" /></span></Button>
        <Button variant="raised" disabled={locked} onClick={overrule}>{`Choose ${optionLabel(question, other)}`}</Button>
        {onSkip && <Button className="nextup-walk-skip" disabled={locked} onClick={onSkip}>Skip</Button>}
        {clock && <Text className="proposal-clock" role="timer">{held ? 'On hold — take your time' : clock}</Text>}
      </div>
      {failed && <Text className="tray-caption" role="status">That did not go through. Try again.</Text>}
    </section>
  );
}
