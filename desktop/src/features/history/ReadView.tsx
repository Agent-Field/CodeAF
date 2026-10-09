import { useEffect, useRef, useState } from 'react';
import { Button, Icon, Markdown, Text } from '../../components/ui';
import { UserMessage } from '../conversation/UserMessage';
import { readHistory, readHistoryMessages } from './client';
import { RecapPane } from './RecapPane';
import type { HistoryDetail, HistoryMessage } from './types';

const PAGE = 120;
/** Messages after the one a search result names, so the reader lands with what came next in view. */
const TRAIL = 40;

type Props = {
  id: string; now: number; at?: number;
  onBack: () => void;
  onContinue: (detail: HistoryDetail, event: { newTab: boolean }) => void;
};

/**
 * "Read conversation" (design 4d): the conversation in the same History tab, read-only, with the recap
 * pinned on top. Messages come from the saved journal; no engine is attached and nothing can be sent.
 */
export function ReadView({ id, now, at, onBack, onContinue }: Props) {
  const [detail, setDetail] = useState<HistoryDetail>();
  const [messages, setMessages] = useState<HistoryMessage[]>([]);
  const [total, setTotal] = useState(0);
  const [failed, setFailed] = useState('');
  const [earlier, setEarlier] = useState(false);
  const target = useRef<HTMLDivElement>(null);
  const scroller = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const stop = new AbortController();
    setDetail(undefined); setMessages([]); setFailed('');
    void Promise.all([readHistory(id, stop.signal), readHistoryMessages(id, { limit: PAGE, before: at === undefined ? undefined : at + TRAIL + 1, signal: stop.signal })])
      .then(([read, page]) => { setDetail(read); setMessages(page.messages); setTotal(page.total); })
      .catch(error => { if (!stop.signal.aborted) setFailed(error instanceof Error ? error.message : 'This conversation could not be read.'); });
    return () => stop.abort();
  }, [id, at]);

  // Land on the message a result named; otherwise the end of the conversation.
  useEffect(() => {
    if (!messages.length || earlier) return;
    if (target.current) target.current.scrollIntoView({ block: 'center' });
    else if (scroller.current) scroller.current.scrollTop = scroller.current.scrollHeight;
  }, [messages.length === 0, at]);

  async function loadEarlier() {
    const first = messages[0]?.index ?? 0;
    setEarlier(true);
    const page = await readHistoryMessages(id, { limit: PAGE, before: first });
    setMessages(current => [...page.messages, ...current]);
  }

  const hasEarlier = (messages[0]?.index ?? 0) > 0;
  return <div className="history-read">
    <div className="history-read-bar"><Button variant="ghost" className="history-back" onClick={onBack}><Icon name="back" size="xs"/>History</Button></div>
    {failed && <p className="history-empty" role="alert">{failed}</p>}
    {detail && <div className="history-read-recap"><RecapPane detail={detail} now={now} onContinue={event => onContinue(detail, event)}/></div>}
    <div ref={scroller} className="history-read-scroll" tabIndex={0} aria-label="Conversation, read only">
      <div className="history-read-column">
        {hasEarlier && <Button variant="ghost" className="history-earlier" onClick={() => void loadEarlier()}>Earlier messages</Button>}
        {messages.map(message => <div key={message.index} ref={message.index === at ? target : undefined} className="history-message" data-role={message.role} data-target={message.index === at || undefined}>
          {message.role === 'user' ? <UserMessage text={message.text}/> : <div className="history-message-reply"><Markdown>{message.text}</Markdown></div>}
        </div>)}
        {!messages.length && !failed && detail && total === 0 && <Text>No messages were saved for this conversation.</Text>}
      </div>
    </div>
  </div>;
}
