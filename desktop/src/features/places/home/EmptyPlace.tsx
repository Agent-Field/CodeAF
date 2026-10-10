import { useEffect, useState } from 'react';
import { Button, Icon } from '../../../components/ui';
import { createPlacesClient } from '../client';
import type { PlaceActions } from '../place-actions';

const client = createPlacesClient();

/** The empty Home names only sources returned by the engine, through the same two ancestor levels used by chat context. */
export function EmptyPlace({ placeId, contextLine, contextParents, revision, actions, readOnly, onWriteInstructions }: {
  placeId: string; contextLine?: string; contextParents?: readonly string[]; revision?: unknown;
  actions: PlaceActions; readOnly?: boolean; onWriteInstructions?: () => void;
}) {
  const [inherited, setInherited] = useState<{ revision: unknown; lines: string[] }>();
  useEffect(() => {
    if (!contextParents?.length || readOnly) return;
    const abort = new AbortController();
    const read = async () => {
      const seen = new Set([placeId]);
      let pending = [...contextParents];
      const lines: string[] = [];
      for (let level = 0; level < 2 && pending.length; level++) {
        const ids = [...new Set(pending)].filter(id => !seen.has(id));
        ids.forEach(id => seen.add(id));
        const parents = await Promise.all(ids.map(id => client.home(id, abort.signal)));
        pending = [];
        for (const parent of parents) {
          if (!parent.place) continue;
          const sources = parent.place.sources.map(source => source.label || source.check.title || source.ref);
          if (sources.length) lines.push(`Uses ${parent.place.name}'s context: ${sources.join(', ')}`);
          pending.push(...parent.place.parents);
        }
      }
      if (!abort.signal.aborted) setInherited({ revision, lines });
    };
    // An unavailable read supplies no evidence for an inherited line. A later Home revision reads it again.
    void read().catch(() => { if (!abort.signal.aborted) setInherited(undefined); });
    return () => abort.abort();
  }, [placeId, contextParents, revision, readOnly]);
  const lines = contextParents ? (inherited && inherited.revision === revision ? inherited.lines : []) : contextLine?.trim() ? [contextLine] : [];
  const add = actions.addSources && !readOnly, write = !!onWriteInstructions && !readOnly;
  return <>
    <section className="home-empty" aria-label="Empty place">
      <p className="home-empty-sentence">Nothing here yet. Start a chat below, drag chats in from anywhere, or drop in what this place should know.</p>
      {(add || write) && <div className="home-empty-actions">
        {add && <Button variant="quiet" onClick={() => actions.addSources?.(placeId)}><Icon name="attach" size="xs"/>Add files or links</Button>}
        {write && <Button variant="quiet" onClick={onWriteInstructions}><Icon name="pencil" size="xs"/>Write instructions</Button>}
      </div>}
    </section>
    {lines.map(line => <p key={line} className="home-quiet home-context">{line}</p>)}
  </>;
}
