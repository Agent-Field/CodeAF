import { useEffect, useRef } from 'react';
import { Button, SectionHeading } from '../../components/ui';
import { ChatList, ChatRow } from './components/ChatRow';
import { PlaceSwatch } from './components/PlaceSwatch';
import { shortTime, type HomeView } from './home-model';
import type { PlaceActions } from './place-actions';
import './home.css';

type HomeQuickLookProps = {
  /** The read for the place being looked at. The sheet shows nothing it was not given. */
  view: HomeView;
  actions: PlaceActions;
  now: Date;
  /** Space, Escape, the scrim and Go to all end here; the owner stops drawing the sheet. */
  onClose: () => void;
  /** At most this many chats are drawn (the design shows two). */
  chatLimit?: number;
};

/** Quick Look (Places 8d): a read-only Home in a sheet. It never switches the window, so what you were working in stays: Space closes it,
 * Go to leaves it and goes, and the rows inside are for reading only. The native dialog traps focus and gives it back to the tile. */
export function HomeQuickLook({ view, actions, now, onClose, chatLimit = 3 }: HomeQuickLookProps) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const element = dialog.current;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : undefined;
    // No close() in the cleanup: the node leaves the page with the sheet, and a close event there would be read as the person's. Focus goes back to the tile by hand.
    if (element && !element.open) element.showModal();
    return () => { if (opener?.isConnected) opener.focus(); };
  }, []);
  // Closing the dialog itself lets the browser give focus back to the tile before the sheet leaves the page; its close event then calls onClose.
  const end = () => { const element = dialog.current; if (element?.open) element.close(); else onClose(); };
  const goTo = () => { end(); void actions.goTo?.(view.id); };
  const newWindow = () => { end(); void actions.goToInNewWindow?.(view.id); };
  const swallowSpace = (event: { key: string; preventDefault: () => void }) => { if (event.key === ' ') event.preventDefault(); };
  return <dialog ref={dialog} className="home-quicklook" aria-label={`Quick Look: ${view.title}`} onClose={onClose}
    onKeyDown={event => { if (event.key === ' ') { event.preventDefault(); end(); } }} onKeyUp={swallowSpace}
    onClick={event => { if (event.target === dialog.current) end(); }}>
    <div className="home-quicklook-body">
      <div className="home-quicklook-head">
        <PlaceSwatch tint={view.tint} role="sheet"/>
        <SectionHeading className="home-quicklook-title">{view.title}</SectionHeading>
        <span className="home-quicklook-hint">Space to close</span>
      </div>
      {view.recap && <p className="home-quicklook-recap">{view.recap.text}</p>}
      {view.chats.length > 0 && <ChatList label={`Chats in ${view.title}`} inert>
        {view.chats.slice(0, chatLimit).map(chat => <ChatRow key={chat.id} id={chat.id} title={chat.title || 'Untitled chat'} excerpt={chat.excerpt} status={chat.status}
          timeLabel={shortTime(chat.at, now)} timeIso={chat.at} onOpen={() => undefined}/>)}
      </ChatList>}
      {view.children.length > 0 && <span className="home-quiet">Places: {view.children.map(child => child.name).join(', ')}</span>}
    </div>
    {(actions.goTo || actions.goToInNewWindow) && <div className="home-quicklook-foot">
      {actions.goTo && <Button variant="primary" autoFocus onClick={goTo}>Go to {view.title}</Button>}
      {actions.goToInNewWindow && <Button variant="quiet" onClick={newWindow}>Open in new window</Button>}
    </div>}
  </dialog>;
}
