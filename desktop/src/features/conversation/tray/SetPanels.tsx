import { useState } from 'react';
import { Button, Icon, Text } from '../../../components/ui';
import type { EngineAnswer } from '../../chat/engine-client';
import { summarize } from './answers';
import type { Question } from './form';
import { questionKey } from './layout';

type ReviewProps = {
  members: Question[];
  held: Record<string, EngineAnswer>;
  onGo: (id: string) => void;
  onSendAll: (answers: EngineAnswer[]) => Promise<boolean>;
};

/** The last tab of a set: every held answer in one list, sent in one go. */
export function ReviewPanel({ members, held, onGo, onSendAll }: ReviewProps) {
  const [state, setState] = useState<'idle' | 'sending' | 'failed'>('idle');
  const ready = members.every((member) => held[questionKey(member)]);
  async function sendAll() {
    setState('sending');
    const ok = await onSendAll(members.map((member) => held[questionKey(member)]));
    setState(ok ? 'idle' : 'failed');
  }
  return (
    <section className="tray-card" aria-label="Review your answers">
      <header className="tray-card-head">
        <h3 className="tray-head">Review your answers</h3>
      </header>
      <ul className="tray-review">
        {members.map((member) => {
          const answer = held[questionKey(member)];
          return (
            <li key={questionKey(member)} className="tray-review-row">
              <Icon name={answer ? 'check' : 'queued'} size="xs" />
              <span className="tray-review-head">{member.head}</span>
              <Text className="tray-caption">{answer ? summarize(member, answer) : 'Not answered yet'}</Text>
              <Button onClick={() => onGo(questionKey(member))}>{answer ? 'Change' : 'Answer'}</Button>
            </li>
          );
        })}
      </ul>
      {state === 'failed' && (
        <Text className="tray-caption" role="status">Not every answer went through. Nothing was lost; try again.</Text>
      )}
      <div className="tray-actions">
        <Button variant="primary" disabled={!ready || state === 'sending'} onClick={() => void sendAll()}>
          {`Send ${members.length} answers`}
        </Button>
      </div>
    </section>
  );
}
