import { useRef } from 'react';
import { Text } from '../../components/ui';
import { Composer } from '../conversation/Composer';
import { PlaceSwatch, type TintName } from '../places/components/PlaceSwatch';
import { usePlaces } from '../places/usePlaces';
import type { PaneRenderProps } from '../tabs/kinds/slots';
import { UserMessage } from '../conversation/UserMessage';
import { CouncilFooter, DecidedCard } from './DecidedCard';
import { COUNCIL_PLACEHOLDER, PlaceMessage } from './PlaceMessage';
import { useCouncilChat } from './useCouncilChat';
import type { Council } from './client';
import '../conversation/conversation-view.css';
import './council.css';

const ENDED = 'This discussion has ended';

/** A council opens as an ordinary conversation tab: the same column and composer, with each line labelled by the place that said it. */
export function CouncilPane({ initial, pane, focused, actions }: PaneRenderProps & { initial: Council }) {
  const { council, setCouncil, messages, refresh, client, ended } = useCouncilChat(initial);
  const { nodes } = usePlaces();
  const tintOf = (name: string): TintName => nodes.find(n => n.name === name)?.effectiveTint ?? 'graphite';
  const names = (id: string) => nodes.find(n => n.id === id)?.name ?? '';
  // Writing pauses the council once, on the first character; clearing the field without sending lets it go on again.
  const holding = useRef(false);

  function onDraft(next: string) {
    actions.onDraft(next);
    if (ended) return;
    const typing = next.trim() !== '';
    if (typing === holding.current) return;
    holding.current = typing;
    // A refused pause or resume is retried by the next keystroke, so the flag follows what the engine last accepted.
    (typing ? client.pause(council.id) : client.resume(council.id)).then(setCouncil, () => { holding.current = !typing; });
  }

  async function send(text: string) {
    try {
      holding.current = false;
      setCouncil(await client.steer(council.id, text));
      void refresh();
      return true;
    } catch {
      holding.current = pane.draft.trim() !== '';
      return false;
    }
  }

  const agreed = council.places.map(names).filter(Boolean);
  return (
    <div className="conversation-view council-view" data-council={council.id}>
      <div className="conversation-main">
        <div className="council-head">
          <span className="council-head-swatches">{council.places.map(id => <PlaceSwatch key={id} tint={tintOf(names(id))} role="title"/>)}</span>
          <Text tone="default" className="council-head-title">{council.label}</Text>
        </div>
        <div className="conversation-scroll">
          <div className="conversation-column council-stack">
            {messages.map((m, i) => m.speaker === 'person'
              ? <UserMessage key={i} text={m.text}/>
              : m.speaker
                ? <PlaceMessage key={i} speaker={{ name: m.speaker, tint: tintOf(m.speaker) }} text={m.text}/>
                : <p key={i} className="council-message-body">{m.text}</p>)}
            {council.state === 'decided' && council.outcome && (
              <DecidedCard decision={{ decision: council.outcome, agreedBy: agreed, turns: council.turns, costUsd: council.spend }}/>
            )}
            <CouncilFooter/>
          </div>
        </div>
        <div className="conversation-dock-layer">
          <div className="conversation-footer conversation-column">
            <Composer draft={pane.draft} onDraft={onDraft} onSend={send} onStop={() => undefined} running={false} docked
              placeholder={COUNCIL_PLACEHOLDER} disabledReason={ended ? ENDED : undefined} autoFocus={focused}/>
          </div>
        </div>
      </div>
    </div>
  );
}
