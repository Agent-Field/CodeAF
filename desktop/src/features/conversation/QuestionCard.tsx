import { useState } from 'react';
import { Button, TextArea } from '../../components/ui';
import type { EngineAnswer, EngineQuestion } from '../chat/engine-client';
import './notice.css';

type QuestionCardProps = {
  question: EngineQuestion;
  busy: boolean;
  onAnswer: (answer: EngineAnswer) => Promise<boolean>;
};

type Option = NonNullable<EngineQuestion['options']>[number];

// Same canonical answer EngineDecision sends: the option key is the answer,
// `picked` repeats it, `change` carries the person's words, `ref` echoes the engine's.
function buildAnswer(question: EngineQuestion, key: string, change: string): EngineAnswer {
  const answer: EngineAnswer = { kind: question.kind, id: question.id, key };
  if (question.ref) {
    answer.ref = question.ref;
  }
  if (key) {
    answer.picked = [key];
  }
  if (change) {
    answer.change = change;
  }
  return answer;
}

function isPrimary(option: Option, index: number, options: Option[]) {
  const anySafe = options.some((candidate) => candidate.safe);
  return anySafe ? Boolean(option.safe) : index === 0;
}

export function QuestionCard({ question, busy, onAnswer }: QuestionCardProps) {
  const [change, setChange] = useState('');
  const [failed, setFailed] = useState(false);
  const [sending, setSending] = useState(false);
  const options = question.options ?? [];
  const locked = busy || sending;

  async function send(key: string) {
    if (locked) {
      return;
    }
    setSending(true);
    setFailed(false);
    try {
      const accepted = await onAnswer(buildAnswer(question, key, change.trim()));
      setFailed(!accepted);
    } catch {
      setFailed(true);
    } finally {
      setSending(false);
    }
  }

  return (
    <section className="question-card" aria-label={question.head} aria-busy={locked || undefined}>
      <h3 className="question-head">{question.head}</h3>
      <p className="question-ask">{question.ask}</p>
      {question.reason && <p className="question-reason">{question.reason}</p>}
      {options.length > 0 && (
        <ul className="question-options">
          {options.map((option, index) => (
            <li key={option.key} className="question-option">
              <Button
                variant={isPrimary(option, index, options) ? 'primary' : 'secondary'}
                disabled={locked}
                onClick={() => void send(option.key)}
              >
                {option.label}
              </Button>
              {option.body && <span className="question-caption">{option.body}</span>}
              {option.consequence && <span className="question-caption">{option.consequence}</span>}
            </li>
          ))}
        </ul>
      )}
      {question.input && (
        <div className="question-input">
          <TextArea
            rows={2}
            aria-label={question.input.prompt || 'Your answer'}
            placeholder={question.input.prompt || 'Your answer'}
            value={change}
            disabled={locked}
            onChange={(event) => setChange(event.target.value)}
          />
          <Button variant="primary" disabled={locked || !change.trim()} onClick={() => void send('')}>
            Submit
          </Button>
        </div>
      )}
      {failed && (
        <p className="question-reason" role="status">
          The answer could not be sent. Your words are kept.
        </p>
      )}
    </section>
  );
}
