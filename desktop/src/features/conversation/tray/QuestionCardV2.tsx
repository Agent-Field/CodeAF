import { useState, type ReactNode } from 'react';
import { Button, Text, TextArea, InlineMarkdown, Tag } from '../../../components/ui';
import type { EngineAnswer } from '../../chat/engine-client';
import { answerFor, canPress, canSend, decideAnswer, initialDraft, submitAnswer, type Draft } from './answers';
import { CardEvidence, CompareTable, type RenderImage } from './CardEvidence';
import { CardFooter } from './CardFooter';
import { ClarifyForm } from './ClarifyForm';
import { ChoiceForm, wantsCards } from './ChoiceForm';
import { deadlineAt } from './clock';
import { formOf, hasWordsField, isIrreversible, needsWords, type Option, type Question } from './form';
import { IrreversibleAnswers, PermissionAnswers, type WhyToggle } from './AnswerForms';
import { BlankFields, CheckList, DialField, Declines, PairRows, WordsField, type FormProps } from './InputForms';
import { NonBlockingNote, doesNotBlock } from './NonBlockingNote';
import { Proposal, isProposal } from './Proposal';
import { OptionList, ScopeChoice, WordsPanel, hasButtonRow } from './OptionActions';

export type QuestionCardProps = {
  question: Question;
  busy: boolean;
  now: number;
  held: boolean;
  onAnswer: (answer: EngineAnswer) => Promise<boolean>;
  onHold: () => void;
  onLater?: () => void;
  onSkip?: () => void;
  /** The only question waiting: no pager, so Enter chooses the primary. */
  single?: boolean;
  renderImage?: RenderImage;
};

type Panel = { kind: 'scope' } | { kind: 'words'; option: Option } | null;

const INPUT_FORMS = new Set(['checklist', 'blanks', 'pairs', 'dial']);

function Fields({ form, props, send }: { form: string; props: FormProps; send: () => void }) {
  if (form === 'checklist') return <CheckList {...props} />;
  if (form === 'blanks') return <BlankFields {...props} />;
  if (form === 'pairs') return <PairRows {...props} />;
  if (form === 'dial') return <DialField {...props} />;
  return <WordsField {...props} onEnter={send} />;
}

type AnswersProps = { question: Question; locked: boolean; single?: boolean; why: WhyToggle; note?: ReactNode; onPress: (option: Option) => void; renderImage?: RenderImage };

/** Permission and irreversible questions have their own rows; everything else lists its options. */
function Answers({ question, locked, single, why, note, onPress, renderImage }: AnswersProps) {
  const form = formOf(question);
  const risky = isIrreversible(question) && (form === 'permission' || form === 'choice');
  if (risky) return <IrreversibleAnswers question={question} locked={locked} note={note} onPress={onPress} />;
  if (form === 'permission') return <PermissionAnswers question={question} locked={locked} single={single} why={why} note={note} onPress={onPress} />;
  return <OptionList question={question} locked={locked} note={note} onPress={onPress} renderImage={renderImage} />;
}

function Heading({ question }: { question: Question }) {
  const permission = formOf(question) === 'permission';
  const risk = permission && (question.stakes === 'reversible' || question.stakes === 'irreversible') ? question.stakes : undefined;
  const from = question.asker?.kind === 'task' ? question.asker.name : '';
  const policyPattern = question.kind === 'consent' && question.reason?.startsWith('critical command ')
    ? question.reason.slice('critical command '.length) : undefined;
  const reason = policyPattern ? `Safety policy matched critical-command pattern ${policyPattern}. Allow once applies only to this request.` : question.reason;
  return (
    <header className="tray-card-head">
      <div className="tray-heading-row">
        <h3 className="tray-head"><InlineMarkdown>{question.head}</InlineMarkdown></h3>
        {risk && <Tag tone={risk === 'irreversible' ? 'danger' : 'neutral'}>{risk === 'irreversible' ? 'Irreversible' : 'Reversible'}</Tag>}
      </div>
      {from && <Text className="tray-caption">{`From ${from}`}</Text>}
      {reason && <Text className="tray-reason">{reason}</Text>}
      {question.kind === 'consent' && question.subject?.name === 'bash' && !question.attach?.some(block => block.kind === 'code') &&
        <Text className="tray-caption">Full command details are unavailable from this engine. Review the tool row before answering.</Text>}
    </header>
  );
}

export function QuestionCardV2({ question, busy, now, held, onAnswer, onHold, onLater, single, renderImage, onSkip }: QuestionCardProps) {
  const [draft, setDraft] = useState<Draft>(() => initialDraft(question));
  const [panel, setPanel] = useState<Panel>(null);
  const [whyOpen, setWhyOpen] = useState(false);
  const [state, setState] = useState<'idle' | 'sending' | 'failed'>('idle');
  const form = formOf(question);
  const locked = busy || state === 'sending';
  const edit = (change: Partial<Draft>) => setDraft((before) => ({ ...before, ...change }));
  const secret = Boolean(question.input?.secret);

  async function send(answer: EngineAnswer) {
    if (locked) return;
    setState('sending');
    if (secret) setDraft(initialDraft(question)); // a secret is never kept on screen
    try {
      setState((await onAnswer(answer)) ? 'idle' : 'failed');
    } catch {
      setState('failed');
    }
  }

  function press(option: Option) {
    if (form === 'permission' && option.widening) return setPanel(panel?.kind === 'scope' ? null : { kind: 'scope' });
    if (needsWords(question, option) && !canPress(question, draft, option)) return setPanel({ kind: 'words', option });
    void send(answerFor(question, draft, option));
  }

  if (isProposal(question)) return <Proposal question={question} busy={locked} now={now} held={held} onAnswer={onAnswer} onHold={onHold} onSkip={onSkip} />;
  const decide = decideAnswer(question);
  const props: FormProps = { question, draft, edit, locked };
  const always = (question.options ?? []).find((option) => option.widening);
  const sendForm = () => canSend(question, draft) && void send(submitAnswer(question, draft));
  const decline = (key: string) => press((question.options ?? []).find((option) => option.key === key)!);
  const clocked = deadlineAt(question) !== null;
  const clarifying = form === 'text';
  const cards = wantsCards(question);
  const nonBlocking = doesNotBlock([question]) ? <NonBlockingNote /> : undefined;
  const note = onSkip ? <>{nonBlocking}<Button className="nextup-walk-skip" disabled={locked} onClick={onSkip}>Skip</Button></> : nonBlocking;
  // The note rides the card's main row of answers when it has one; otherwise the footer row carries it.
  const risky = isIrreversible(question) && (form === 'permission' || form === 'choice');
  const rowHostsNote = cards || (form !== 'text' && (INPUT_FORMS.has(form) || risky || form === 'permission' || hasButtonRow(question)));
  const stopClock = () => clocked && !held && onHold();
  const source = (
    <>
      <Heading question={question} />
      <CardEvidence blocks={question.attach} renderImage={renderImage} />
      <CompareTable question={question} />
      {form === 'proposal' && question.input?.blanks?.length ? <BlankFields {...props} /> : null}
      {INPUT_FORMS.has(form) ? <Fields form={form} props={props} send={sendForm} /> : null}
      {hasWordsField(question) && form === 'choice' && !cards ? <WordsField {...props} onEnter={() => undefined} /> : null}
    </>
  );
  const extra = (
    <>
      {form === 'permission' && !isIrreversible(question) && (whyOpen || draft.change) && (
        <TextArea
          className="tray-field"
          rows={1}
          aria-label="Say why (optional)"
          placeholder="Say why (optional)"
          disabled={locked}
          value={draft.change}
          onChange={(event) => edit({ change: event.target.value })}
        />
      )}
      {hasWordsField(question) && form === 'choice' && cards ? <WordsField {...props} onEnter={() => undefined} /> : null}
      {panel?.kind === 'scope' && always && (
        <ScopeChoice question={question} locked={locked} onPick={(scope) => void send(answerFor(question, { ...draft, scope }, always))} />
      )}
      {panel?.kind === 'words' && (
        <WordsPanel
          prompt={question.input?.prompt || 'What should it know?'}
          value={draft.change}
          locked={locked}
          onChange={(change) => edit({ change })}
          onSend={() => void send(answerFor(question, draft, panel.option))}
        />
      )}
      {state === 'failed' && (
        <Text className="tray-caption" role="status">
          That did not go through. Try again.
        </Text>
      )}
    </>
  );
  const footer = (
    <CardFooter
      question={question}
      now={now}
      held={held}
      locked={locked}
      canDecide={Boolean(decide) && !clarifying}
      clock={!cards}
      note={rowHostsNote ? undefined : note}
      onHold={onHold}
      onLater={clarifying ? undefined : onLater}
      onDecide={() => decide && void send(decide)}
    />
  );
  const frame = (scroll: ReactNode, pin: ReactNode) => (
    <section className="tray-card" aria-label={question.head} aria-busy={locked || undefined} onFocusCapture={stopClock}>
      <div className="tray-scroll" tabIndex={0}>{scroll}</div>
      <div className="tray-pinned">{pin}</div>
    </section>
  );

  if (cards) {
    return (
      <ChoiceForm
        question={question}
        now={now}
        held={held}
        locked={locked}
        note={note}
        onChoose={press}
        onHold={onHold}
        renderImage={renderImage}
        render={({ cards: list, actions }) => frame(<>{source}{list}</>, <>{actions}{extra}{footer}</>)}
      />
    );
  }
  const answers = form === 'text' ? (
    <ClarifyForm {...props} onSend={sendForm} onLater={clocked ? undefined : onLater} onDecide={decide ? () => void send(decide) : undefined} />
  ) : INPUT_FORMS.has(form) ? (
    <div className="tray-actions">
      <Button variant="primary" disabled={locked || !canSend(question, draft)} onClick={sendForm}>Send</Button>
      <Declines question={question} locked={locked} onPress={decline} />
      {note}
    </div>
  ) : (
    <Answers question={question} locked={locked} single={single} why={{ open: whyOpen, toggle: () => setWhyOpen((open) => !open) }} note={note} onPress={press} renderImage={renderImage} />
  );
  return frame(source, <>{answers}{extra}{footer}</>);
}
