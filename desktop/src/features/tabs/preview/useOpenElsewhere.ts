import { useEffect, useState } from 'react';
import { fetchEngine } from '../../chat/engine-client';
import type { Pane } from '../types';

type OpenPlace = { id: string; name: string };
const placesFrom = (value: unknown): OpenPlace[] => {
 if (!value || typeof value !== 'object' || !Array.isArray((value as { places?: unknown }).places)) return [];
 const seen = new Set<string>();
 return (value as { places: unknown[] }).places.slice(0, 2000).flatMap(row => {
  if (!row || typeof row !== 'object') return [];
  const p = row as Partial<OpenPlace>;
  if (typeof p.id !== 'string' || !/^pl_[0-9a-f]{16}$/.test(p.id) || typeof p.name !== 'string' || !p.name.trim() || seen.has(p.id)) return [];
  seen.add(p.id); return [{ id: p.id, name: p.name }];
 });
};

/** Only a visible conversation preview reads this metadata. A bounded refresh
 * notices another Place closing its view without adding a new world record or
 * keeping another persistent native connection. Closing aborts the read. */
export function useOpenElsewhere(workspace: string | undefined, pane: Pane, enabled: boolean): readonly OpenPlace[] {
 const identity = `${workspace ?? ""}:${pane.id}:${pane.kind}:${pane.sessionFile ?? ""}`;
 const [result, setResult] = useState<{ identity: string; places: OpenPlace[] }>({ identity, places: [] });
 const setPlaces = (places: OpenPlace[]) => setResult({ identity, places });
 useEffect(() => {
  setPlaces([]);
  if (!enabled || !workspace || pane.kind !== 'conversation' || !pane.sessionFile) return;
  const controller = new AbortController();
  let gone = false, loading = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const load = async () => {
   if (loading || gone) return;
   loading = true;
   try {
    const response = await fetchEngine(`/workspaces/${encodeURIComponent(workspace)}/open-elsewhere?pane=${encodeURIComponent(pane.id)}`, { signal: controller.signal });
    const rows = response.ok ? placesFrom(await response.json()) : [];
    if (!gone) setPlaces(rows.filter(place => place.id !== workspace));
   } catch { if (!gone) setPlaces([]); }
   finally { loading = false; if (!gone) timer = setTimeout(() => { void load(); }, 2000); }
  };
  const refresh = () => { clearTimeout(timer); void load(); };
  void load(); window.addEventListener('focus', refresh);
  return () => { gone = true; controller.abort(); clearTimeout(timer); window.removeEventListener('focus', refresh); };
 }, [workspace, pane.id, pane.kind, pane.sessionFile, enabled]);
 return result.identity === identity && enabled ? result.places : [];
}
