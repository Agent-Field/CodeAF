import { useEffect, useState } from 'react';
import { connectEngine, findEngineFiles } from '../../../chat/engine-client';
import type { FileHit } from './rows';

export const fileSearchDelay = 120;
/** One attach per saved conversation however often the field searches. */
const sessions = new Map<string, Promise<string>>();
function sessionIdOf(sessionFile: string): Promise<string> {
  let id = sessions.get(sessionFile);
  if (!id) {
    id = connectEngine(sessionFile).then(snapshot => snapshot.id);
    id.catch(() => sessions.delete(sessionFile));
    sessions.set(sessionFile, id);
  }
  return id;
}

/**
 * Files in the workspace that match the query, best first. The field has no session of its own (a new tab
 * makes no engine call), so it asks through a conversation that is already saved; with none, or with the
 * engine away, there are no file rows. Older answers never overwrite newer ones.
 */
export function useFileMatches(query: string, sessionFile: string | undefined): FileHit[] {
  const [hits, setHits] = useState<FileHit[]>([]);
  useEffect(() => {
    const text = query.trim();
    if (!text || !sessionFile) { setHits([]); return; }
    let current = true;
    const timer = window.setTimeout(() => {
      sessionIdOf(sessionFile).then(id => findEngineFiles(id, text, 8)).then(found => { if (current) setHits(found.files ?? []); }).catch(() => { if (current) setHits([]); });
    }, fileSearchDelay);
    return () => { current = false; window.clearTimeout(timer); };
  }, [query, sessionFile]);
  return hits;
}
