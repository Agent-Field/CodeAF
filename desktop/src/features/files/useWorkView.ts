// The reads behind a file or diff tab. Every byte comes from the engine through the tab's session,
// because the engine may be on another machine (docs/ENGINE.md "File and diff tabs").
import { useEffect, useRef, useState } from 'react';
import { connectEngine, engineChanges, engineEditors, engineEditorTarget, engineFileDiff, EngineError, readEngine, readEngineFile, readEngineText, statEnginePaths, watchEngine, type EngineEditor, type EngineEditorTarget, type EngineFile, type EngineFileDiff, type EngineTextFile } from '../chat/engine-client';
import { hostName } from '../../design/native';
import { fileEventTouches, fileRefreshDelay, fileStatInterval, fileVersion, sameWorkspaceFile } from './fileRefresh';

export type Load<T> = { status: 'loading' } | { status: 'ready'; value: T } | { status: 'failed'; message: string };

const loading = { status: 'loading' } as const;
const messageOf = (reason: unknown) => (reason instanceof EngineError || reason instanceof Error ? reason.message : 'The engine did not answer.');

/**
 * Runs `read` when `key` changes, ignoring answers that arrive after the key moved on. `key` null waits.
 * `refresh` reads again without the loading state, so a conversation's edit keeps the scroll position.
 */
function useLoad<T>(key: string | null, read: () => Promise<T>, refresh = 0): Load<T> {
  const [state, setState] = useState<Load<T>>(loading);
  const keyRef = useRef<string | null>(null);
  useEffect(() => {
    if (key === null) return;
    let live = true;
    if (keyRef.current !== key) setState(loading);
    keyRef.current = key;
    read().then(
      value => live && setState({ status: 'ready', value }),
      reason => live && setState({ status: 'failed', message: messageOf(reason) }),
    );
    return () => { live = false; };
  }, [key, refresh]);
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
export function useFileDiff(session: string | undefined, path: string, refresh = 0): Load<EngineFileDiff> {
  return useLoad(session ? `${session}\0${path}` : null, () => engineFileDiff(session!, path), refresh);
}

/** The whole file as text. `enabled` is false until a view needs it. */
export function useFileText(session: string | undefined, path: string, enabled: boolean, refresh = 0): Load<EngineTextFile> {
  return useLoad(session && enabled ? `${session}\0${path}` : null, () => readEngineText(session!, path), refresh);
}

/**
 * The file as a picture, through the existing confined read. `enabled` is false until the File view of a
 * picture asks. The answer carries the path it was read for, so a late answer for a path the tab has since
 * left shows loading instead of the wrong picture.
 */
export function useFileImage(session: string | undefined, path: string, enabled: boolean, refresh = 0): Load<EngineFile> {
  const state = useLoad(session && enabled ? `${session}\0${path}` : null, async () => ({ path, file: await readEngineFile(session!, path) }), refresh);
  if (state.status !== 'ready') return state;
  return state.value.path === path ? { status: 'ready', value: state.value.file } : loading;
}

const streamBackoff = [1000, 2000, 5000, 10000];

/**
 * Re-reads when this file changes. Two signals, neither of which calls a model:
 * the session stream (an edit or write of the path, or a finished turn whose changes list names it),
 * and the file's own version from the engine's stat (size and modification time), which is what
 * catches a shell command or a save in another editor. The stat asks about this one path, every
 * `fileStatInterval` and when the window comes back to the front. While the tab or the window is
 * hidden both signals stop, and they stop for good when the tab closes.
 */
export function useFileRefresh(session: string | undefined, path: string, shown: boolean): number {
  const [tick, setTick] = useState(0);
  // A read the stream asked for also moves the version; the stat takes that as its new baseline
  // instead of asking for a second read of the same bytes.
  const rebase = useRef(false);
  // The last version seen survives a hidden spell, so a file saved while the tab was in the back is read on return.
  const seen = useRef<{ key: string; version: string } | null>(null);
  useEffect(() => {
    if (!session || !shown) return;
    const key = `${session}\0${path}`;
    let stopped = false;
    let timer = 0;
    let busy = false;
    const check = async () => {
      if (busy || stopped || document.hidden) return;
      busy = true;
      try {
        const [fact] = await statEnginePaths(session, [path]);
        if (stopped || !fact) return;
        const version = fileVersion(fact);
        const known = seen.current?.key === key ? seen.current.version : null;
        if (known !== null && version !== known && !rebase.current) {
          window.clearTimeout(timer);
          timer = window.setTimeout(() => { if (!stopped) setTick(n => n + 1); }, fileRefreshDelay);
        }
        rebase.current = false;
        seen.current = { key, version };
      } catch {
        /* An engine that cannot stat leaves the stream as the only signal. */
      } finally {
        busy = false;
      }
    };
    void check();
    const every = window.setInterval(() => void check(), fileStatInterval);
    const front = () => void check();
    window.addEventListener('focus', front);
    document.addEventListener('visibilitychange', front);
    return () => {
      stopped = true;
      window.clearTimeout(timer);
      window.clearInterval(every);
      window.removeEventListener('focus', front);
      document.removeEventListener('visibilitychange', front);
    };
  }, [session, path, shown]);
  useEffect(() => {
    if (!session || !shown) return;
    let stopped = false;
    let attempts = 0;
    let timer = 0;
    let retry = 0;
    const controller = new AbortController();
    const bump = () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => { if (!stopped) { rebase.current = true; setTick(n => n + 1); } }, fileRefreshDelay);
    };
    const watch = async () => {
      while (!stopped) {
        try {
          const snapshot = await readEngine(session);
          if (stopped) return;
          await watchEngine(snapshot, () => {}, event => {
            const args = typeof event.raw?.Args === 'string' ? event.raw.Args : '';
            if ((event.kind === 'toolEnd' || event.kind === 'toolFailed') && fileEventTouches(event.tool, args, path)) bump();
            if (event.kind !== 'turnDone') return;
            void engineChanges(session, [path]).then(list => {
              if (!stopped && list.files?.some(file => sameWorkspaceFile(file.path, path))) bump();
            }, () => undefined);
          }, controller.signal);
        } catch {
          if (stopped || controller.signal.aborted) return;
          if (attempts >= streamBackoff.length) return;
          const delay = streamBackoff[attempts];
          attempts += 1;
          await new Promise<void>(resolve => {
            const done = () => resolve();
            retry = window.setTimeout(done, delay);
            controller.signal.addEventListener('abort', done, { once: true });
          });
          continue;
        }
        if (stopped) return;
        // A stream that closed cleanly can be opened again. A stream that keeps failing stops above.
        attempts = 0;
      }
    };
    void watch();
    return () => {
      stopped = true;
      window.clearTimeout(timer);
      window.clearTimeout(retry);
      controller.abort();
    };
  }, [session, path, shown]);
  return tick;
}

export type Handoff = {
  abs?: string;
  /** The desktop shell may hand the path to the operating system's default opener. */
  canOpen: boolean;
  editors?: EngineEditor[];
  /** The engine itself can start one of `editors`. False for a remote or headless engine, and when the list never arrived. */
  canLaunch?: boolean;
  /** True once GET /editors answered, including an honest empty list. */
  listed?: boolean;
  session?: string;
};

/**
 * Where "Open in" can go. The legacy opener needs this machine's name. The editor list is the
 * engine's own, and a failed list leaves the legacy opener in place rather than inventing one.
 */
export function useHandoff(session: string | undefined, path: string): Handoff {
  const [handoff, setHandoff] = useState<Handoff>({ canOpen: false });
  useEffect(() => {
    if (!session) return;
    let live = true;
    Promise.all([engineEditorTarget(session, path), hostName()]).then(
      async ([target, host]: [EngineEditorTarget, string | undefined]) => {
        const editors = await engineEditors(session, path).catch(() => null);
        if (!live) return;
        setHandoff({
          abs: target.abs,
          canOpen: target.local && host !== undefined && host === target.host,
          editors: editors?.editors,
          canLaunch: editors !== null && editors.local && editors.open,
          listed: editors !== null,
          session,
        });
      },
      () => live && setHandoff({ canOpen: false }),
    );
    return () => { live = false; };
  }, [session, path]);
  return handoff;
}
