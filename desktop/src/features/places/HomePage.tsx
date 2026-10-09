import { useState, type ReactNode } from 'react';
import { PlaceHeading, type Crumb } from './components/PlaceHeading';
import { AllPlacesPage } from './AllPlacesPage';
import { HomeAttentionSection, HomeBanner, HomeChatsSection, HomeDeleteConfirm, HomeEmptyPlace, HomeFrame, HomeNotice, HomePlacesSection, HomeRecap, HomeSourcesSection, useDeleteFlow, useDragState, useRunner } from './HomeSections';
import type { HomeConnection, HomeView } from './home-model';
import { homeMenu, type PlaceActions } from './place-actions';

export type HomePageProps = {
  /** The read for this page. Absent while loading, after a failed first read, or while offline with nothing yet loaded. */
  view?: HomeView;
  connection?: HomeConnection;
  /** Every verb the owner has wired. A verb that is not here has no control on the page. */
  actions: PlaceActions;
  /** The composer that starts a chat in this place (↵ new tab after Home, ⌘↵ background): the conversation feature owns it. */
  composer?: ReactNode;
  suggestion?: ReactNode;
  /** Injected so tests and the specimen are deterministic; the live page uses the real time. */
  now?: Date;
  /** The host's label for Open in new window (⌘↵ on Mac, Ctrl ↵ elsewhere). */
  newWindowHint?: string;
};

/** A place's Home, the root's All places, or Now (Places 8a to 8e). One page; `view.kind` says which. Everything it shows is in `view`; every
 * control it draws exists because the owner wired the verb behind it. Loading, error and offline are states of this page, not other pages. */
export function HomePage({ view, connection = { state: 'ready' }, actions, composer, suggestion, now, newWindowHint }: HomePageProps) {
  const runner = useRunner();
  const drag = useDragState();
  const deletion = useDeleteFlow(actions, runner);
  const [renaming, setRenaming] = useState(false);
  const readOnly = connection.state === 'offline';
  const clock = now ?? new Date();

  if (!view) {
    return <HomeFrame label="Place" composer={composer}><HomeNotice connection={connection} hasView={false} onRetry={actions.retry}/></HomeFrame>;
  }
  const notices = <>
    <HomeNotice connection={connection} hasView onRetry={actions.retry}/>
    <HomeBanner message={runner.error} onDismiss={runner.clear}/>
    {deletion.state && <HomeDeleteConfirm state={deletion.state} busy={runner.busy} onConfirm={() => void deletion.confirm()} onCancel={deletion.cancel}/>}
  </>;
  const parentId = view.breadcrumb.length ? view.breadcrumb[view.breadcrumb.length - 1].id : 'root';
  const onUp = view.kind === 'place' && actions.goTo ? () => void runner.run(() => actions.goTo?.(parentId)) : undefined;

  if (view.kind === 'root') {
    return <HomeFrame label="All places" composer={composer}>
      <AllPlacesPage suggestion={suggestion} view={view} actions={actions} readOnly={readOnly} runner={runner} drag={drag} onDelete={deletion.start} now={clock} notices={notices}/>
    </HomeFrame>;
  }

  const crumb = (id: string, label: string): Crumb => ({ id, label, onGo: () => void runner.run(() => actions.goTo?.(id)), onGoInNewWindow: actions.goToInNewWindow && (() => void runner.run(() => actions.goToInNewWindow?.(id))) });
  // Without a way to go, a breadcrumb would be a row of dead buttons, so it is not drawn.
  const breadcrumb = view.kind === 'place' && actions.goTo ? [crumb('root', 'All places'), ...view.breadcrumb.map(item => crumb(item.id, item.name))] : [];
  const isPlace = view.kind === 'place';
  const menu = isPlace ? homeMenu({ id: view.id, name: view.title, tint: view.tint, pinned: view.pinned }, actions, {
    readOnly, canRename: !!actions.rename, startRename: () => setRenaming(true), startDelete: deletion.start && (() => deletion.start?.({ id: view.id, name: view.title })), newWindowHint }) : [];
  const nothingYet = isPlace && !view.children.length && !view.chats.length && !view.attention.length;
  const siblings = view.children.map(child => child.name);

  return <HomeFrame label={view.title} composer={composer} onUp={onUp}>
    <PlaceHeading title={view.title} tint={view.tint} breadcrumb={breadcrumb} menu={menu} menuLabel={`${view.title} actions`}
      renaming={renaming} onRenameCancel={() => setRenaming(false)}
      onRename={name => void runner.run(() => actions.rename?.(view.id, name)).then(ok => { if (ok) setRenaming(false); })}/>
    {notices}
    {view.recap && <HomeRecap label={view.recap.label} text={view.recap.text}/>}
    <HomeAttentionSection items={view.attention} actions={actions} readOnly={readOnly}/>
    {isPlace && !nothingYet && <HomePlacesSection label="Places" places={view.children} parentId={view.id} parentName={view.title} parentTint={view.tintSource === 'own' ? view.tint : undefined}
      actions={actions} readOnly={readOnly} siblings={siblings} runner={runner} drag={drag} onDelete={deletion.start}/>}
    <HomeChatsSection label={isPlace ? 'Chats' : 'Not in any place'} chats={view.chats} truncated={view.chatsTruncated} actions={actions} readOnly={readOnly}
      inPlaceId={isPlace ? view.id : undefined} drag={drag} now={clock}/>
    {isPlace && <HomeSourcesSection placeId={view.id} sources={view.sources ?? []} actions={actions} readOnly={readOnly} showAdd={!nothingYet}/>}
    {nothingYet && <HomeEmptyPlace placeId={view.id} contextLine={view.contextLine} actions={actions} readOnly={readOnly}/>}
    {isPlace && !nothingYet && view.contextLine && <p className="home-quiet home-context">{view.contextLine}</p>}
  </HomeFrame>;
}
