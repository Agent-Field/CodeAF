import { useState } from 'react';
import { Button, Icon, IconButton, Text } from '../../../components/ui';
import type { EngineAnswer } from '../../chat/engine-client';
import { bulkAnswers } from './answers';
import type { Question } from './form';
import { questionKey } from './layout';
import './batch.css';

type Role = 'allow' | 'deny';
type Decided = Record<string, Role>;

type BatchProps = {
  members: Question[];
  busy: boolean;
  /** Sends the members' answers in order; resolves true when the engine took them all. */
  onSend: (answers: EngineAnswer[]) => Promise<boolean>;
};

/** The batch is one question: it only holds the reply up when one of its members does. */
const blocksReply = (members: Question[]) => members.some((member) => member.blocking?.turn);

const isOpen = (member: Question, decided: Decided) => !decided[questionKey(member)];

/** The next member still waiting after `from`, wrapping round; -1 when every one is decided. */
function nextOpen(members: Question[], decided: Decided, from: number): number {
  const after = members.findIndex((member, at) => at > from && isOpen(member, decided));
  return after >= 0 ? after : members.findIndex((member) => isOpen(member, decided));
}

type PagerProps = { members: Question[]; busy: boolean; onDone: (decided: Decided) => void };

/** "One by one": the same card, a member at a time, with the pager in its header row. */
function Pager({ members, busy, onDone }: PagerProps) {
  const [index, setIndex] = useState(0);
  const [decided, setDecided] = useState<Decided>({});
  const member = members[index];
  const key = questionKey(member);
  const go = (to: number) => setIndex(Math.min(Math.max(to, 0), members.length - 1));

  function decide(role: Role) {
    const next = { ...decided, [key]: role };
    setDecided(next);
    const open = nextOpen(members, next, index);
    if (open < 0) onDone(next);
    else go(open);
  }

  return (
    <>
      <div className="batch-pager">
        <span className="batch-count">{`${index + 1} of ${members.length}`}</span>
        <IconButton label="Previous action" icon="chevronLeft" iconSize="xs" disabled={index === 0} onClick={() => go(index - 1)} />
        <IconButton label="Next action" icon="chevronRight" iconSize="xs" disabled={index === members.length - 1} onClick={() => go(index + 1)} />
      </div>
      <h3 className="batch-title">{member.head}</h3>
      <div className="batch-actions">
        <Button className="batch-allow" disabled={busy} onClick={() => decide('allow')}>{decided[key] === 'allow' ? 'Allowed' : 'Allow'}</Button>
        <Button className="batch-deny" disabled={busy} onClick={() => decide('deny')}>{decided[key] === 'deny' ? 'Denied' : 'Deny'}</Button>
      </div>
    </>
  );
}

/** Several yes-or-no actions asked at once, answered once. */
export function BatchCard({ members, busy, onSend }: BatchProps) {
  const [open, setOpen] = useState(true);
  const [paging, setPaging] = useState(false);
  const title = `Allow ${members.length} ${members.length === 1 ? 'action' : 'actions'}?`;
  const sendAll = (role: Role) => void onSend(bulkAnswers(members, role));
  const sendEach = (decided: Decided) =>
    void onSend(members.flatMap((member) => bulkAnswers([member], decided[questionKey(member)])));

  return (
    <section className="batch-card" aria-label={title} aria-busy={busy || undefined}>
      {paging ? (
        <Pager members={members} busy={busy} onDone={sendEach} />
      ) : (
        <>
          <Button className="batch-head" aria-expanded={open} aria-controls="batch-commands" onClick={() => setOpen(!open)}>
            <h3 className="batch-title">{title}</h3>
            <span className="batch-chevron" data-open={open}><Icon name="chevron" size="xs" /></span>
          </Button>
          {open && (
            <ul className="batch-commands" id="batch-commands">
              {members.map((member) => <li key={questionKey(member)}>{member.head}</li>)}
            </ul>
          )}
          <div className="batch-actions">
            <Button className="batch-allow" disabled={busy} onClick={() => sendAll('allow')}>Allow all</Button>
            <Button className="batch-deny" disabled={busy} onClick={() => sendAll('deny')}>Deny all</Button>
            <Button className="batch-more" disabled={busy} onClick={() => setPaging(true)}>One by one</Button>
            {!blocksReply(members) && <Text className="batch-note">Doesn&rsquo;t block this reply</Text>}
          </div>
        </>
      )}
    </section>
  );
}
