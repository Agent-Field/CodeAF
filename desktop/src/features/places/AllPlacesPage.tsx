import { useState, type ReactNode } from 'react';
import { Button, HomeTitle, Icon, TextInput } from '../../components/ui';
import { PlaceTile } from './components/PlaceTile';
import { HomeAttentionSection, HomeChatsSection, HomePlacesSection, type DragState, type Runner } from './HomeSections';
import { searchPlaces, totalsLine, type HomeChild, type HomeView } from './home-model';
import type { PlaceActions } from './place-actions';

type AllPlacesPageProps = {
  view: HomeView;
  actions: PlaceActions;
  readOnly?: boolean;
  runner: Runner;
  drag: DragState;
  onDelete?: (place: { id: string; name: string }) => void;
  now: Date;
  /** The page's connection notice, action error and delete confirmation, drawn under the title. */
  notices: ReactNode;
};

/** All places: the root Home (Places 8c, 8e, 8f). Top-level places as tiles, a search over every place, archived places behind one toggle, and
 * the chats that are in no place with the engine's one line offering to move a cluster of them. */
export function AllPlacesPage({ view, actions, readOnly, runner, drag, onDelete, now, notices }: AllPlacesPageProps) {
  const [query, setQuery] = useState('');
  const [showArchived, setShowArchived] = useState(false);
  const searching = query.trim().length > 0;
  const pool: readonly HomeChild[] = view.allPlaces ?? view.children;
  const matches = searching ? searchPlaces(pool, query) : [];
  const first = view.children.length === 0 && !view.archivedChildren?.length && !view.allPlaces?.length;
  const archived = view.archivedChildren ?? [];
  const unplacedTotal = view.unplaced?.total ?? view.chats.length;
  const count = totalsLine(view.totals);
  const siblings = view.children.map(child => child.name);

  return <>
    <header className="home-heading all-places-heading">
      <div className="all-places-title-row">
        <HomeTitle>All places</HomeTitle>
        {!first && <label className="all-places-search">
          <Icon name="search" size="xs"/>
          <TextInput appearance="field" type="search" className="all-places-search-input" aria-label="Search places" placeholder="Search places" autoComplete="off" spellCheck={false}
            value={query} onChange={event => setQuery(event.target.value)} onKeyDown={event => { if (event.key === 'Escape' && query) { event.preventDefault(); event.stopPropagation(); setQuery(''); } }}/>
        </label>}
      </div>
      {count && !first && <p className="home-quiet" data-testid="all-places-count">{count}</p>}
    </header>
    {notices}

    {first && <section className="home-empty" aria-label="First launch">
      <p className="home-empty-sentence">Places hold work that belongs together, with what the AI should know about it. Open a folder or repo to make one, or just name one.</p>
    </section>}

    {searching
      ? <section className="home-section" aria-label="Search results">
          {matches.length === 0
            ? <div className="home-search-empty">
                <p className="home-quiet" role="status">No place called “{query.trim()}”.</p>
                {actions.create && !readOnly && <Button variant="quiet" loading={runner.busy} onClick={() => void runner.run(() => actions.create?.({ name: query.trim() })).then(ok => { if (ok) setQuery(''); })}>Create “{query.trim()}”</Button>}
              </div>
            : <HomePlacesSection label={`${matches.length} ${matches.length === 1 ? 'match' : 'matches'}`} places={matches} actions={actions} readOnly={readOnly} siblings={siblings}
                runner={runner} drag={drag} onDelete={onDelete} allowNew={false}/>}
        </section>
      : <>
          <HomeAttentionSection items={view.attention} actions={actions} readOnly={readOnly}/>
          <HomePlacesSection label="Places" places={view.children} actions={actions} readOnly={readOnly} siblings={siblings} runner={runner} drag={drag} onDelete={onDelete}
            newLabel={first ? 'Name a place' : undefined}
            extraTiles={first && actions.openFolderAsPlace ? <PlaceTile mode="new" label="Open a folder or repo" disabled={readOnly} onCreate={() => actions.openFolderAsPlace?.()}/> : undefined}/>
          {archived.length > 0 && <section className="home-section" aria-label="Archived places">
            <Button variant="ghost" className="home-archived-toggle" aria-expanded={showArchived} onClick={() => setShowArchived(open => !open)}>
              <Icon name={showArchived ? 'chevron' : 'chevronRight'} size="micro"/>Archived · {archived.length}
            </Button>
            {showArchived && <HomePlacesSection label="Archived" places={archived} actions={actions} readOnly={readOnly} siblings={siblings} runner={runner} drag={drag} allowNew={false} showLabel={false} restore/>}
          </section>}
        </>}

    {!searching && view.chats.length > 0 && <HomeChatsSection label={`Not in any place · ${unplacedTotal}`} chats={view.chats} truncated={view.chatsTruncated} actions={actions} readOnly={readOnly} drag={drag} now={now}/>}
    {!searching && view.suggestion && actions.acceptSuggestion && !readOnly && <div className="home-suggestion">
      <Icon name="sparkles" size="micro"/>{view.suggestion.text} · <Button variant="ghost" className="home-suggestion-action" onClick={() => void runner.run(() => actions.acceptSuggestion?.())}>{view.suggestion.action}</Button>
    </div>}
  </>;
}
