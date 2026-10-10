import { useState, type ReactNode } from 'react';
import './root-home.css';
import { Button, HomeTitle, Icon, TextInput } from '../../../components/ui';
import { FirstLaunch } from './FirstLaunch';
import { Suggestion } from './Suggestion';
import { HomeAttentionSection, HomeChatsSection, HomePlacesSection, type DragState, type Runner } from '../HomeSections';
import { searchPlaces, totalsLine, type HomeChild, type HomeView } from '../home-model';
import type { PlaceActions } from '../place-actions';
import { staleSuggestion } from '../stale-model';
import { browserSnoozes, idleLine } from '../suggestions';

type RootHomeProps = {
  view: HomeView;
  suggestion?: ReactNode;
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
export function RootHome({ suggestion, view, actions, readOnly, runner, drag, onDelete, now, notices }: RootHomeProps) {
  const [query, setQuery] = useState('');
  const [showArchived, setShowArchived] = useState(false);
  const searching = query.trim().length > 0;
  const pool: readonly HomeChild[] = view.allPlaces ?? view.children;
  const matches = searching ? searchPlaces(pool, query) : [];
  // Chats have no ancestor path here because this list contains only unplaced conversations.
  const chats = searching ? view.chats.filter(chat => chat.title.toLowerCase().includes(query.trim().toLowerCase())) : view.chats;
  const first = view.children.length === 0 && !view.archivedChildren?.length && !view.allPlaces?.length;
  const archived = view.archivedChildren ?? [];
  const unplacedTotal = view.unplaced?.total;
  const unplacedLabel = unplacedTotal && unplacedTotal > 0 ? `Not in any place · ${unplacedTotal}` : 'Not in any place';
  const count = totalsLine(view.totals);
  const siblings = view.children.map(child => child.name);
  // The engine names the candidate; the injected clock and persisted snooze decide whether it is still due.
  const confirmed = view.stale ? idleLine(view.stale, now, browserSnoozes(now)) : undefined;
  const stale = !searching && !readOnly && view.stale && confirmed
    ? staleSuggestion([{ ...view.stale, daysUntouched: confirmed.days }], { merge: actions.chooseMergeTarget, archive: actions.archive, snooze: actions.snoozeStale })
    : undefined;

  return <>
    {first ? <FirstLaunch actions={actions} readOnly={readOnly} runner={runner} drag={drag} notices={notices}/> : <header className="home-heading all-places-heading root-home-heading">
      <div className="all-places-title-row">
        <HomeTitle>All places</HomeTitle>
        {!first && <label className="all-places-search">
          <Icon name="search" size="sm"/>
          <TextInput appearance="field" type="search" className="all-places-search-input" aria-label="Search places" placeholder="Search places" autoComplete="off" spellCheck={false}
            value={query} onChange={event => setQuery(event.target.value)} onKeyDown={event => { if (event.key === 'Escape' && query) { event.preventDefault(); event.stopPropagation(); setQuery(''); } }}/>
        </label>}
      </div>
      {count && !first && <p className="home-quiet" data-testid="all-places-count">{count}</p>}
    </header>}
    {!first && notices}

    {searching
      ? (matches.length > 0 || chats.length === 0) && <section className="home-section" aria-label="Search results">
          {matches.length === 0 && chats.length === 0
            ? <div className="home-search-empty">
                <p className="home-quiet" role="status">No place called “{query.trim()}”.</p>
                {actions.create && !readOnly && <Button variant="quiet" loading={runner.busy} onClick={() => void runner.run(() => actions.create?.({ name: query.trim() })).then(ok => { if (ok) setQuery(''); })}>Create “{query.trim()}”</Button>}
              </div>
            : matches.length > 0 && <HomePlacesSection label={`${matches.length} ${matches.length === 1 ? 'match' : 'matches'}`} places={matches} actions={actions} readOnly={readOnly} siblings={siblings}
                runner={runner} drag={drag} onDelete={onDelete} allowNew={false}/>}
        </section>
      : <>
          <HomeAttentionSection items={view.attention} actions={actions} readOnly={readOnly}/>
          {!first && <HomePlacesSection label="Places" places={view.children} actions={actions} readOnly={readOnly} siblings={siblings} runner={runner} drag={drag} onDelete={onDelete}
            showLabel={false}/>}
          {stale && <Suggestion layout="line" text={stale.text} actions={stale.actions} run={runner.run} busy={runner.busy}/>}
          {archived.length > 0 && <section className="home-section" aria-label="Archived places">
            <Button variant="ghost" className="home-archived-toggle" aria-expanded={showArchived} onClick={() => setShowArchived(open => !open)}>
              <Icon name={showArchived ? 'chevron' : 'chevronRight'} size="micro"/>Archived · {archived.length}
            </Button>
            {showArchived && <HomePlacesSection label="Archived" places={archived} actions={actions} readOnly={readOnly} siblings={siblings} runner={runner} drag={drag} allowNew={false} showLabel={false} restore/>}
          </section>}
        </>}

    {chats.length > 0 && <HomeChatsSection label={unplacedLabel} chats={chats} truncated={view.chatsTruncated} actions={actions} readOnly={readOnly} drag={drag} now={now}/>}
    {!searching && !readOnly && suggestion}
    {!searching && view.suggestion && actions.acceptSuggestion && !readOnly && <div className="home-suggestion">
      <Icon name="sparkles" size="micro"/>{view.suggestion.text} · <Button variant="ghost" className="home-suggestion-action" onClick={() => void runner.run(() => actions.acceptSuggestion?.())}>{view.suggestion.action}</Button>
    </div>}
  </>;
}
