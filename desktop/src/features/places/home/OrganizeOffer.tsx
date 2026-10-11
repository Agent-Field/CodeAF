import { useCallback, useEffect, useState } from 'react';
import { requestOpenKind } from '../../shell/shellState';
import { OrganizeError, organizeClient } from '../../settings/placesOrganizeClient';
import { offerLine } from '../../settings/placesOrganizeModel';
import { PlacesOrganize } from '../../settings/PlacesOrganize';
import { Suggestion } from './Suggestion';

type Phase = 'idle' | 'started' | 'gone';

/**
 * The first-run offer to organize chats that already exist (PO-8). The engine decides when it is due and names
 * the count; this draws the design's sparkles line for it and hands a yes to the same run, progress and plan
 * review Settings uses. Nothing is applied here: the person's yes is the Accept in the review sheet.
 */
export function OrganizeOffer() {
  const [unplaced, setUnplaced] = useState(0);
  const [phase, setPhase] = useState<Phase>('idle');
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const abort = new AbortController();
    organizeClient.read(abort.signal).then(snap => {
      if (abort.signal.aborted) return;
      setUnplaced(snap.offer?.unplaced ?? 0);
      // A run already going or a plan waiting is the offer already taken up; Settings and this page show the same run.
      if (snap.running || snap.plan) setPhase('started');
    // A missing route or an unreachable engine means there is nothing to offer, so nothing is drawn.
    }).catch((error: unknown) => { if (!(error instanceof OrganizeError)) setPhase('gone'); });
    return () => abort.abort();
  }, []);
  const done = useCallback(() => setPhase('gone'), []);
  const run = async (job: () => void | Promise<void>) => {
    setBusy(true);
    // A refused verb leaves the offer where it was, so the person can press it again.
    try { await job(); } catch { /* the line stays */ } finally { setBusy(false); }
  };
  if (phase === 'gone') return null;
  if (phase === 'started') return <PlacesOrganize onFinished={done}/>;
  if (unplaced <= 0) return null;
  return <Suggestion layout="line" label="Organize your chats" text={offerLine(unplaced)} busy={busy} run={run}
    actions={[
      { id: 'organize', label: 'Organize', onSelect: async () => { await organizeClient.start('add-on'); setPhase('started'); } },
      { id: 'not-now', label: 'Not now', onSelect: async () => { await organizeClient.dismissOffer(); setPhase('gone'); } },
      { id: 'options', label: 'Options', onSelect: () => requestOpenKind('settings') },
    ]}/>;
}
