import { useState, type ReactNode } from 'react';
import { Button, Text, InlineMarkdown, Tag } from '../../../components/ui';
import type { EngineAnswer } from '../../chat/engine-client';
import { answerFor, canSend, decideAnswer, initialDraft, submitAnswer, type Draft } from './answers';
import { CardEvidence, CompareTable, type RenderImage } from './CardEvidence';
import { CardFooter } from './CardFooter';
import { ChoiceForm, wantsCards } from './ChoiceForm';
import { deadlineAt } from './clock';
import { formOf, isIrreversible, needsWords, permissionRoles, type Option, type Question } from './form';
import { IrreversibleAnswers, PermissionAnswers, type WhyToggle } from './AnswerForms';
import { BlankFields, CheckList, DialField, Declines, PairRows, WordsField, type FormProps } from './InputForms';
import { NonBlockingNote, doesNotBlock } from './NonBlockingNote';
import { Proposal, isProposal } from './Proposal';
import { OptionList, ScopeChoice } from './OptionActions';

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
  onCompose?: (option?: Option) => void;
};

type Panel = { kind: 'scope' } | null;

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
  const distinctHead = question.asker?.kind === 'task' && question.asker.name !== question.head;
  const policyPattern = question.kind === 'consent' && question.reason?.startsWith('critical command ')
    ? question.reason.slice('critical command '.length) : undefined;
  const reason = policyPattern ? `Safety policy matched critical-command pattern ${policyPattern}. Allow once applies only to this request.` : question.reason?.replace(/nobody could check it/gi, 'Needs your call');
  return (
    <header className="tray-card-head">
      {(distinctHead || risk) && <div className="tray-heading-row">
        {distinctHead && <h3 className="tray-head"><InlineMarkdown>{question.head}</InlineMarkdown></h3>}
        {risk && <Tag tone={risk === 'irreversible' ? 'danger' : 'neutral'}>{risk === 'irreversible' ? 'Irreversible' : 'Reversible'}</Tag>}
      </div>}
      {reason && reason !== question.head && reason !== question.asker?.name && <Text className="tray-reason">{reason}</Text>}
      {question.kind === 'consent' && question.subject?.name === 'bash' && !question.attach?.some(block => block.kind === 'code') &&
        <Text className="tray-caption">Full command details are unavailable from this engine. Review the tool row before answering.</Text>}
    </header>
  );
}

export function QuestionCardV2({ question, busy, now, held, onAnswer, onHold, onLater, single, renderImage, onSkip, onCompose }: QuestionCardProps) {
  const [draft, setDraft] = useState<Draft>(() => initialDraft(question));
  const [panel, setPanel] = useState<Panel>(null);
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
    if (needsWords(question, option) || /^other(?:…|\.\.\.)?$/i.test(option.label.trim())) return onCompose?.(option);
    void send(answerFor(question, draft, option));
  }

  if (isProposal(question)) return <Proposal question={question} busy={locked} now={now} held={held} onAnswer={onAnswer} onHold={onHold} onSkip={onSkip} />;
  const decide = decideAnswer(question);
  const props: FormProps = { question, draft, edit, locked };
  const always = (question.options ?? []).find((option) => option.widening);
  const sendForm = () => canSend(question, draft) && void send(submitAnswer(question, draft));
  const decline = (key: string) => press((question.options ?? []).find((option) => option.key === key)!);
  const clocked = deadlineAt(question) !== null;
  const cards = wantsCards(question);
  const nonBlocking = doesNotBlock([question]) ? <NonBlockingNote /> : undefined;
  const note = onSkip ? <>{nonBlocking}<Button className="nextup-walk-skip" disabled={locked} onClick={onSkip}>Skip</Button></> : nonBlocking;
  const footer = (
    <CardFooter
      question={question}
      now={now}
      held={held}
      locked={locked}
      canDecide={Boolean(decide)}
      clock={!cards}
      note={undefined}
      onHold={onHold}
      onLater={undefined}
      onDecide={() => decide && void send(decide)}
    />
  );
  // Later and the quiet note share the answers' row, keeping short cards to one action line.
  const later = onLater && !clocked ? <Button disabled={locked} onClick={onLater}>Later</Button> : undefined;
  const meta = <div className="tray-action-meta">{note}{later}{footer}</div>;
  const stopClock = () => clocked && !held && onHold();
  const source = (
    <>
      <Heading question={question} />
      <CardEvidence blocks={question.attach} renderImage={renderImage} />
      <CompareTable question={question} />
      {form === 'proposal' && question.input?.blanks?.length ? <BlankFields {...props} /> : null}
      {INPUT_FORMS.has(form) ? <Fields form={form} props={props} send={sendForm} /> : null}
    </>
  );
  const extra = (
    <>
      {panel?.kind === 'scope' && always && (
        <ScopeChoice question={question} locked={locked} onPick={(scope) => void send(answerFor(question, { ...draft, scope }, always))} />
      )}
      {state === 'failed' && (
        <Text className="tray-caption" role="status">
          That did not go through. Try again.
        </Text>
      )}
    </>
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
        note={meta}
        onChoose={press}
        onHold={onHold}
        renderImage={renderImage}
        render={({ cards: list, actions }) => frame(<>{source}{list}</>, <>{actions}{extra}</>)}
      />
    );
  }
  const answers = form === 'text' ? (
    <div className="tray-actions">{meta}</div>
  ) : INPUT_FORMS.has(form) ? (
    <div className="tray-actions">
      <Button variant="primary" disabled={locked || !canSend(question, draft)} onClick={sendForm}>Send</Button>
      <Declines question={question} locked={locked} onPress={decline} />
      {meta}
    </div>
  ) : (
    <Answers question={question} locked={locked} single={single} why={{ toggle: () => onCompose?.(permissionRoles(question).deny) }} note={meta} onPress={press} renderImage={renderImage} />
  );
  return frame(source, <>{answers}{extra}</>);
}
