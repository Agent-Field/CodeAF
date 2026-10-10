import { useContext, useEffect, useSyncExternalStore } from 'react';
import { Button, Icon, IconButton, Text } from '../../components/ui';
import { openUrl } from '../../design/native';
import type { WebFailure, WebNotice } from '../../design/nativeWeb';
import { LoadingLine } from '../tabs/LoadingLine';
import type { PaneRenderProps } from '../tabs/kinds/slots';
import { refusalSentence, siteOf } from './address';
import { TabsApiContext } from '../tabs/context';
import { useWebFavicons } from './favicons';
import { AddressField } from './AddressField';
import { webHost, subscribeWebHost } from './host';
import { capture } from './shots';
import { useWebPane } from './useWebPane';
import { navigate, retry, step } from './views';
import './web.css';

const noticeLine: Record<WebNotice, string> = {
  download: 'Downloads do not open in web tabs',
  blocked: 'That page tried to open something a web tab does not open',
  permission: 'Web tabs do not get the camera, microphone or location',
};

function failureText(failure: WebFailure, url: string): { title: string; detail: string } {
  if (failure.kind === 'unreachable') return { title: 'This page did not load', detail: `codeaf could not reach ${siteOf(url)}.` };
  return { title: 'This address does not open here', detail: `${refusalSentence[failure.reason]}.` };
}

/**
 * A web tab's pane (design 3d): a 48px header (back, forward, reload or stop,
 * the address, start a conversation with the page, open in the browser) over
 * a sheet inset in the card. The page itself is a native view placed over the
 * sheet by views.ts; this component draws only what is not the page: the
 * loading line, a failure, and outside the desktop app an honest line with
 * Open in browser. While a menu or other overlay holds the window the sheet
 * is blank: the native view is hidden, and nothing is written in its place.
 */
export function WebPane({ pane, actions }: PaneRenderProps) {
  const url = pane.target?.url;
  const api = useContext(TabsApiContext);
  const favicons = useWebFavicons(api?.workspaceKey, [pane]);
  const web = useWebPane(pane.id, url);
  const host = useSyncExternalStore(subscribeWebHost, webHost);
  const state = web.state;
  const shown = state?.url || url;
  const title = state?.title.trim() || (shown ? siteOf(shown) : '');

  // The tab is named by its page, and remembers the page it is on.
  useEffect(() => { if (title) actions.onSummary({ title, firstLine: '', digest: shown ?? '' }); }, [title]);
  useEffect(() => { if (state?.url && state.url !== url && /^https?:/i.test(state.url)) actions.onView({ target: { url: state.url } }); }, [state?.url]);

  function go(next: string) {
    actions.onView({ target: { url: next } });
    navigate(pane.id, next);
  }
  async function talkAboutPage() {
    if (!shown || !host?.startConversationWithPage) return;
    const shot = web.native ? await capture(pane.id, true) : null;
    host.startConversationWithPage({ url: shown, title, shot: shot?.image });
  }

  const live = web.native && !!state;
  const loading = !!state?.loading && !state.failure;
  const historyOpen = (flag: boolean | undefined) => live && (!state!.historyKnown || !!flag);
  const failure = state?.failure ? failureText(state.failure, shown ?? '') : web.openError ? { title: 'This page did not open', detail: `${web.openError}.` } : null;
  return <div className="web-pane" data-pane={pane.id} data-loading={loading || undefined}>
    <LoadingLine active={loading}/>
    <div className="web-header" role="toolbar" aria-label="Page">
      <IconButton icon="back" iconSize="sm" label="Back" disabled={!historyOpen(state?.canBack)} onClick={() => step(pane.id, 'back')}/>
      <IconButton icon="forward" iconSize="sm" label="Forward" disabled={!historyOpen(state?.canForward)} onClick={() => step(pane.id, 'forward')}/>
      {loading
        ? <IconButton icon="close" iconSize="sm" label="Stop loading" onClick={() => step(pane.id, 'stop')}/>
        : <IconButton icon="reload" iconSize="sm" label="Reload" disabled={!live} onClick={() => step(pane.id, 'reload')}/>}
      <AddressField favicon={favicons.get(pane.id)} url={shown} status={state?.notice ? noticeLine[state.notice] : undefined} onGo={go}/>
      {host?.startConversationWithPage && shown && <IconButton icon="chatPlus" iconSize="sm" label="Start a conversation with this page" onClick={() => void talkAboutPage()}/>}
      <IconButton icon="external" iconSize="sm" label="Open in browser" disabled={!shown} onClick={() => shown && void openUrl(shown)}/>
    </div>
    <div ref={web.sheetRef} className="web-sheet" data-covered={web.covered || undefined} data-blank={web.blank || undefined}>
      {web.blank ? null : <>
        {!web.native && shown && <div className="web-state">
          <Icon name="web" size="lg"/>
          <Text tone="default">Web pages open in the desktop app</Text>
          <Text>This window cannot show {siteOf(shown)}.</Text>
          <Button variant="raised" onClick={() => void openUrl(shown)}>Open in browser</Button>
        </div>}
        {web.native && failure && <div className="web-state" role="alert">
          <Icon name="warn" size="lg"/>
          <Text tone="default">{failure.title}</Text>
          <Text>{failure.detail}</Text>
          <div className="web-state-actions">
            <Button variant="raised" onClick={() => retry(pane.id)}>Try again</Button>
            {shown && <Button onClick={() => void openUrl(shown)}>Open in browser</Button>}
          </div>
        </div>}
        {web.native && !failure && web.covered && typeof web.shot === 'object' && <img className="web-frozen" src={web.shot.image} alt=""/>}
      </>}
    </div>
  </div>;
}
