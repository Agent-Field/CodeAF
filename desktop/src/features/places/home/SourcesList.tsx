import { useRef, useState } from 'react';
import { Button, Icon, IconButton, Row, RowActions, SectionLabel } from '../../../components/ui';
import type { HomeSource } from '../home-model';
import type { PlaceActions } from '../place-actions';

export type SourcesListProps = {
  placeId: string;
  sources: readonly HomeSource[];
  actions: Pick<PlaceActions, 'removeSource' | 'addSources'>;
  readOnly?: boolean;
  showAdd?: boolean;
};

const sourceWords: Record<NonNullable<HomeSource['state']>, string> = { ok: '', missing: 'missing', unreadable: 'unreadable', unknown: '' };

/** The engine owns the list and the shell owns receipt Undo, so a pending removal never hides a source locally. */
export function SourcesList({ placeId, sources, actions, readOnly, showAdd }: SourcesListProps) {
  const pending = useRef(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  if (!sources.length) return null;
  const remove = !readOnly ? actions.removeSource : undefined;
  const add = showAdd && actions.addSources && !readOnly;
  const run = async (job: () => void | Promise<void>) => {
    // The synchronous guard also covers a second press before React renders disabled controls.
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setError(undefined);
    try { await job(); }
    catch (failure) { setError(failure instanceof Error && failure.message ? failure.message : 'Could not change these sources. Try again.'); }
    finally { pending.current = false; setBusy(false); }
  };
  return <section className="home-section home-sources" aria-label="Sources" aria-busy={busy}>
    <SectionLabel>Sources</SectionLabel>
    <ul className="home-source-list" aria-label="Sources">
      {sources.map(source => <li key={source.id}><Row className="home-source" data-source-id={source.id} data-state={source.state}>
        <span className="home-source-label">{source.label}</span>
        <span className="home-source-note">{[source.kind, source.state ? sourceWords[source.state] : ''].filter(Boolean).join(' · ')}</span>
        {remove && <RowActions><IconButton size="row" icon="close" iconSize="xs" label={`Remove ${source.label}`} disabled={busy}
          onClick={() => void run(() => remove(placeId, source.id))}/></RowActions>}
      </Row></li>)}
    </ul>
    {error && <p className="home-quiet" role="alert">{error}</p>}
    {add && <div className="home-empty-actions"><Button variant="quiet" disabled={busy} onClick={() => void run(() => actions.addSources?.(placeId))}>
      <Icon name="attach" size="xs"/>Add files or links</Button></div>}
  </section>;
}
