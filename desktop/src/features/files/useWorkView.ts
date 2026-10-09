// The reads behind a file or diff tab. Every byte comes from the engine through the tab's session,
// because the engine may be on another machine (docs/ENGINE.md "File and diff tabs").
import { useEffect, useState } from 'react';
import { connectEngine, engineEditorTarget, engineFileDiff, EngineError, readEngineText, type EngineEditorTarget, type EngineFileDiff, type EngineTextFile } from '../chat/engine-client';
import { hostName } from '../../design/native';

export type Load<T> = { status: 'loading' } | { status: 'ready'; value: T } | { status: 'failed'; message: string };

const loading = { status: 'loading' } as const;
const messageOf = (reason: unknown) => (reason instanceof EngineError || reason instanceof Error ? reason.message : 'The engine did not answer.');

/** Runs `read` when `key` changes, ignoring answers that arrive after the key moved on. `key` null waits. */
function useLoad<T>(key: string | null, read: () => Promise<T>): Load<T> {
  const [state, setState] = useState<Load<T>>(loading);
  useEffect(() => {
    if (key === null) return;
    let live = true;
    setState(loading);
    read().then(
      value => live && setState({ status: 'ready', value }),
      reason => live && setState({ status: 'failed', message: messageOf(reason) }),
    );
    return () => { live = false; };
  }, [key]);
  return state;
}

export type FileSession = { id: string; workspace: string };

/** Attaches the tab's saved session once, only to read through it. It never creates a session or sends. */
export function useFileSession(sessionFile?: string): Load<FileSession> {
  return useLoad(sessionFile ?? null, async () => {
    const snapshot = await connectEngine(sessionFile);
    return { id: snapshot.id, workspace: snapshot.workspace };
  });
}

/** One file's changes against the base. A tab remounts when it is selected, so each visit reads fresh. */
export function useFileDiff(session: string | undefined, path: string): Load<EngineFileDiff> {
  return useLoad(session ? `${session}\0${path}` : null, () => engineFileDiff(session!, path));
}

/** The whole file as text. `enabled` is false until a view needs it. */
export function useFileText(session: string | undefined, path: string, enabled: boolean): Load<EngineTextFile> {
  return useLoad(session && enabled ? `${session}\0${path}` : null, () => readEngineText(session!, path));
}

export type Handoff = { abs?: string; canOpen: boolean };

/**
 * Whether "Open in editor" can reach this file: the engine must be a child of this app's bridge (`local`)
 * and report this machine's name as its host. Anything else offers Copy path only.
 */
export function useHandoff(session: string | undefined, path: string): Handoff {
  const [handoff, setHandoff] = useState<Handoff>({ canOpen: false });
  useEffect(() => {
    if (!session) return;
    let live = true;
    Promise.all([engineEditorTarget(session, path), hostName()]).then(
      ([target, host]: [EngineEditorTarget, string | undefined]) => live && setHandoff({ abs: target.abs, canOpen: target.local && host !== undefined && host === target.host }),
      () => live && setHandoff({ canOpen: false }),
    );
    return () => { live = false; };
  }, [session, path]);
  return handoff;
}
