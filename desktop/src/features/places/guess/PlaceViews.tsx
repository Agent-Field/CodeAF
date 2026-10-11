import { useEffect, useState } from 'react';
import { Button, SectionLabel, TextInput } from '../../../components/ui';
import { placesTransport, type PlacesTransport } from '../client';
import { readLenses, steerMessage, type Lens } from './model';
import './guess.css';

/** Views filter the existing chat list. They never create places or manufacture chat rows. */
export function ViewsChips({ lenses, selected, onSelect }: { lenses: readonly Lens[]; selected?: Lens; onSelect: (lens: Lens | undefined) => void }) {
  if (!lenses.length) return null;
  return <div className="place-views" aria-label="Views"><SectionLabel>Views</SectionLabel>
    {lenses.map((lens, index) => <Button key={`${lens.axis}-${lens.value}-${index}`} variant="ghost" className="place-view-chip" aria-pressed={selected === lens} onClick={() => onSelect(selected === lens ? undefined : lens)}>{lens.name}</Button>)}
  </div>;
}
export function AlsoIn({ text }: { text?: string }) { return text ? <span className="place-also-in">{text}</span> : null; }

/** A refused sentence stays beside the field; a successful steer reads views again from the engine. */
export function PlaceViews({ placeId, revision, disabled, onFilter, transport = placesTransport }: { placeId: string; revision?: unknown; disabled?: boolean; onFilter: (ids: readonly string[] | undefined) => void; transport?: PlacesTransport }) {
  const [lenses, setLenses] = useState<Lens[]>([]);
  const [selected, setSelected] = useState<Lens>();
  const [sentence, setSentence] = useState('');
  const [reply, setReply] = useState('');
  const [busy, setBusy] = useState(false);
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    const abort = new AbortController();
    setLenses([]); setSelected(undefined); onFilter(undefined);
    if (!disabled) void transport(`/places/${encodeURIComponent(placeId)}/lenses`, { method: 'GET', signal: abort.signal })
      .then(value => { if (!abort.signal.aborted) setLenses(readLenses(value)); }).catch(() => { /* An unavailable view is absent until the next engine reading. */ });
    return () => abort.abort();
  }, [placeId, revision, disabled, refresh, transport, onFilter]);
  const steer = async () => {
    if (!sentence.trim() || busy || disabled) return;
    setBusy(true); setReply('');
    try {
      const message = steerMessage(await transport('/places/steer', { method: 'POST', body: { scope: placeId, sentence: sentence.trim() } }));
      setReply(message);
      if (!message) { setSentence(''); setRefresh(value => value + 1); }
    } catch (failure) { setReply(failure instanceof Error ? failure.message : 'Could not steer this place.'); }
    finally { setBusy(false); }
  };
  return <div className="place-guess-home">
    <ViewsChips lenses={lenses} selected={selected} onSelect={lens => { setSelected(lens); onFilter(lens?.chats); }}/>
    {!disabled && <form className="place-steer" onSubmit={event => { event.preventDefault(); void steer(); }}>
      <TextInput aria-label="Steer this place" placeholder="Organize this by…" value={sentence} onChange={event => setSentence(event.currentTarget.value)} disabled={busy}/>
      <Button variant="ghost" type="submit" disabled={busy || !sentence.trim()}>Apply</Button>
    </form>}
    {reply && <p className="place-steer-reply" role="status">{reply}</p>}
  </div>;
}
