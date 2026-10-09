import { useState } from 'react';
import { Button, Select, Text, TextArea, TextInput } from '../../components/ui';
import type { EngineAnswer, EngineQuestion } from '../chat/engine-client';
import { QuestionEvidence } from './QuestionEvidence';
import {
  buildAnswer,
  canSubmit,
  hasBlanks,
  initialDraft,
  isChecklist,
  isUndrawable,
  togglePicked,
  type QuestionDraft,
} from './questionAnswer';
import './notice.css';

type QuestionCardProps = {
  question: EngineQuestion;
  busy: boolean;
  onAnswer: (answer: EngineAnswer) => Promise<boolean>;
};

type Option = NonNullable<EngineQuestion['options']>[number];
type Edit = (change: Partial<QuestionDraft>) => void;
type FormProps = { question: EngineQuestion; draft: QuestionDraft; edit: Edit; locked: boolean };

function isPrimary(option: Option, index: number, options: Option[]) {
  const anySafe = options.some((candidate) => candidate.safe);
  return anySafe ? Boolean(option.safe) : index === 0;
}

function Blanks({ question, draft, edit, locked }: FormProps) {
  if (!hasBlanks(question)) return null;
  const set = (label: string, value: string) => edit({ blanks: { ...draft.blanks, [label]: value } });
  return (
    <div className="question-blanks">
      {(question.input?.blanks ?? []).map((blank) => (
        <label key={blank.label} className="question-field">
          <span className="question-caption">{blank.label}</span>
          {blank.kind === 'choice' ? (
            <Select
              label={blank.label}
              disabled={locked}
              value={draft.blanks[blank.label] ?? ''}
              onValueChange={(value) => set(blank.label, value)}
              options={(blank.choices ?? []).map((value) => ({ value, label: value }))}
            />
          ) : (
            <TextInput
              aria-label={blank.label}
              type={blank.kind === 'number' ? 'number' : question.input?.secret ? 'password' : 'text'}
              disabled={locked}
              value={draft.blanks[blank.label] ?? ''}
              onChange={(event) => set(blank.label, event.target.value)}
            />
          )}
        </label>
      ))}
    </div>
  );
}

type OptionsProps = FormProps & { send: (key: string) => void };

function Options({ question, draft, edit, locked, send }: OptionsProps) {
  const options = question.options ?? [];
  if (!options.length) return null;
  const checklist = isChecklist(question);
  const choose = (key: string) => (checklist ? edit({ picked: togglePicked(draft.picked, key) }) : send(key));
  return (
    <ul className="question-options">
      {options.map((option, index) => (
        <li key={option.key} className="question-option">
          <Button
            variant={!checklist && isPrimary(option, index, options) ? 'primary' : 'secondary'}
            disabled={locked}
            aria-pressed={checklist ? draft.picked.includes(option.key) : undefined}
            onClick={() => choose(option.key)}
          >
            {option.label}
          </Button>
          {option.body && <span className="question-caption">{option.body}</span>}
          {option.consequence && <span className="question-caption">{option.consequence}</span>}
          {option.widening && <span className="question-caption">Broader permission</span>}
          {option.blocks?.map((block, blockIndex) => <QuestionEvidence key={blockIndex} block={block} />)}
        </li>
      ))}
    </ul>
  );
}

function Words({ question, draft, edit, locked, send }: OptionsProps) {
  const options = question.options ?? [];
  const needsSubmit = isChecklist(question) || hasBlanks(question) || !options.length;
  if (!question.input && !needsSubmit) return null;
  const prompt = question.input?.prompt || 'Your answer';
  return (
    <div className="question-input">
      <TextArea
        rows={2}
        aria-label={prompt}
        placeholder={prompt}
        value={draft.change}
        disabled={locked}
        onChange={(event) => edit({ change: event.target.value })}
      />
      {needsSubmit && (
        <Button variant="primary" disabled={locked || !canSubmit(question, draft)} onClick={() => send('')}>
          Submit
        </Button>
      )}
    </div>
  );
}

function Scope({ question, draft, edit, locked }: FormProps) {
  const scopes = (question.scope ?? []).filter((value) => value !== 'once');
  if (!scopes.length) return null;
  const options = [{ value: 'once', label: 'This request' }, ...scopes.map((value) => ({ value, label: value }))];
  return <Select label="Answer scope" disabled={locked} value={draft.scope} onValueChange={(scope) => edit({ scope })} options={options} />;
}

export function QuestionCard({ question, busy, onAnswer }: QuestionCardProps) {
  const [draft, setDraft] = useState(() => initialDraft(question));
  const [failed, setFailed] = useState(false);
  const [sending, setSending] = useState(false);
  const undrawable = isUndrawable(question);
  const locked = busy || sending || undrawable;
  const edit: Edit = (change) => setDraft((before) => ({ ...before, ...change }));

  async function send(key: string) {
    if (locked) return;
    setSending(true);
    setFailed(false);
    try {
      setFailed(!(await onAnswer(buildAnswer(question, key, draft))));
    } catch {
      setFailed(true);
    } finally {
      setSending(false);
    }
  }

  const form = { question, draft, edit, locked, send: (key: string) => void send(key) };
  return (
    <section className="question-card" aria-label={question.head} aria-busy={busy || sending || undefined}>
      <h3 className="question-head">{question.head}</h3>
      <p className="question-ask">{question.ask}</p>
      {question.reason && <p className="question-reason">{question.reason}</p>}
      {question.attach?.map((block, index) => <QuestionEvidence key={index} block={block} />)}
      <Blanks {...form} />
      <Options {...form} />
      {!undrawable && <Words {...form} />}
      <Scope {...form} />
      {undrawable && <Text role="status">This question needs the terminal. Open this conversation there to answer it.</Text>}
      {failed && (
        <p className="question-reason" role="status">
          The answer could not be sent. Your words are kept.
        </p>
      )}
    </section>
  );
}
