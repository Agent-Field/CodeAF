import { useState } from 'react';
import { Button, Text, TextArea } from '../../../components/ui';
import type { EngineAnswer } from '../../chat/engine-client';
import { answerFor, canPress, canSend, decideAnswer, initialDraft, submitAnswer, type Draft } from './answers';
import { CardEvidence, CompareTable, type RenderImage } from './CardEvidence';
import { CardFooter } from './CardFooter';
import { ClarifyForm } from './ClarifyForm';
import { deadlineAt } from './clock';
import { formOf, hasWordsField, isIrreversible, needsWords, type Option, type Question } from './form';
import { IrreversibleAnswers, PermissionAnswers } from './AnswerForms';
import { BlankFields, CheckList, DialField, Declines, PairRows, WordsField, type FormProps } from './InputForms';
import { OptionList, ScopeChoice, WordsPanel } from './OptionActions';

export type QuestionCardProps = {
  question: Question;
  busy: boolean;
  now: number;
  held: boolean;
  onAnswer: (answer: EngineAnswer) => Promise<boolean>;
  onHold: () => void;
  onLater?: () => void;
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

type AnswersProps = { question: Question; locked: boolean; single?: boolean; onPress: (option: Option) => void; renderImage?: RenderImage };

/** Permission and irreversible questions have their own rows; everything else lists its options. */
function Answers({ question, locked, single, onPress, renderImage }: AnswersProps) {
  const form = formOf(question);
  const risky = isIrreversible(question) && (form === 'permission' || form === 'choice');
  if (risky) return <IrreversibleAnswers question={question} locked={locked} onPress={onPress} />;
  if (form === 'permission') return <PermissionAnswers question={question} locked={locked} single={single} onPress={onPress} />;
  return <OptionList question={question} locked={locked} onPress={onPress} renderImage={renderImage} />;
}

function Heading({ question }: { question: Question }) {
  const from = question.asker?.kind === 'task' ? question.asker.name : '';
  return (
    <header className="tray-card-head">
      <h3 className="tray-head">{question.head}</h3>
      {from && <Text className="tray-caption">{`From ${from}`}</Text>}
      {question.reason && <Text className="tray-reason">{question.reason}</Text>}
    </header>
  );
}

export function QuestionCardV2({ question, busy, now, held, onAnswer, onHold, onLater, single, renderImage }: QuestionCardProps) {
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
    if (needsWords(question, option) && !canPress(question, draft, option)) return setPanel({ kind: 'words', option });
    void send(answerFor(question, draft, option));
  }

  const decide = decideAnswer(question);
  const props: FormProps = { question, draft, edit, locked };
  const always = (question.options ?? []).find((option) => option.widening);
  const sendForm = () => canSend(question, draft) && void send(submitAnswer(question, draft));
  const decline = (key: string) => press((question.options ?? []).find((option) => option.key === key)!);
  const clocked = deadlineAt(question) !== null;
  const clarifying = form === 'text';
  const stopClock = () => clocked && !held && onHold();

  return (
    <section
      className="tray-card"
      aria-label={question.head}
      aria-busy={locked || undefined}
      onFocusCapture={stopClock}
    >
      <Heading question={question} />
      <CardEvidence blocks={question.attach} renderImage={renderImage} />
      <CompareTable question={question} />
      {form === 'proposal' && question.input?.blanks?.length ? <BlankFields {...props} /> : null}
      {form === 'text' ? (
        <ClarifyForm
          {...props}
          onSend={sendForm}
          onLater={clocked ? undefined : onLater}
          onDecide={decide ? () => void send(decide) : undefined}
        />
      ) : INPUT_FORMS.has(form) ? (
        <>
          <Fields form={form} props={props} send={sendForm} />
          <div className="tray-actions">
            <Button variant="primary" disabled={locked || !canSend(question, draft)} onClick={sendForm}>Send</Button>
            <Declines question={question} locked={locked} onPress={decline} />
          </div>
        </>
      ) : (
        <Answers question={question} locked={locked} single={single} onPress={press} renderImage={renderImage} />
      )}
      {form === 'permission' && (
        <TextArea
          className="tray-field"
          rows={1}
          aria-label="Say why, if you say no"
          placeholder="Say why, if you say no"
          disabled={locked}
          value={draft.change}
          onChange={(event) => edit({ change: event.target.value })}
        />
      )}
      {hasWordsField(question) && form === 'choice' && (
        <WordsField {...props} onEnter={() => undefined} />
      )}
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
      <CardFooter
        question={question}
        now={now}
        held={held}
        locked={locked}
        canDecide={Boolean(decide) && !clarifying}
        onHold={onHold}
        onLater={clarifying ? undefined : onLater}
        onDecide={() => decide && void send(decide)}
      />
    </section>
  );
}
