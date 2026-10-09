// The one hook a window's tab strip uses to hold its place's tab set: the engine's canonical copy, mirrored live,
// with this window's own focus and scroll. It replaces `useReducer(workspaceReducer, …)` plus the localStorage
// save effect, and keeps their shape — `state` and `dispatch` — so the strip itself does not change.
//
//   const sync = useWorkspaceSync({ key: place ?? 'now', initial: readWorkspace });
//   const { state, dispatch } = sync;
//
// `initial` is read only when neither the engine nor this window holds a tab set for the place: that is the
// one-time import of what the person had in localStorage before the engine kept their tabs. The old key is read,
// never deleted.
import { useEffect, useMemo, useState, useSyncExternalStore } from 'react';
import type { WorkspaceAction, WorkspaceState } from '../tabs/model';
import { createWorkspaceClient, type WorkspaceClient, type WorkspaceKey } from './client';
import { createWorkspaceController, type SyncStatus, type WorkspaceController } from './controller';
import { adoptOrphans, holdWindow, readPersisted, renameWindow, safeLocal, windowWriter, writePersisted } from './windowStore';

export type WorkspaceSyncOptions = {
  /** The window's place: `now` or a place-graph id. A new key is a new tab set. */
  key: WorkspaceKey;
  /** The tab set to import when nothing is saved anywhere for this place. */
  initial: () => WorkspaceState;
  /** A tab handed to this window (Move to new window): shown first. */
  focus?: string;
  /** Injected in tests and the browser harness. */
  client?: WorkspaceClient;
  storage?: Storage;
};

export type WorkspaceSync = {
  state: WorkspaceState;
  dispatch: (action: WorkspaceAction) => void;
  status: SyncStatus;
  /** Try the engine again now. */
  retry: () => void;
  /** The person has seen that another window overtook some of their changes. */
  acknowledge: () => void;
  scrollOf: (paneId: string) => number | undefined;
  setScroll: (paneId: string, top: number) => void;
  /** Same-place Move to new window: moves this window's focus off the tab; answers what the new window should focus. */
  handoff: WorkspaceController['handoff'];
};

function build(options: WorkspaceSyncOptions): WorkspaceController {
  const storage = options.storage ?? safeLocal();
  const writer = windowWriter();
  return createWorkspaceController({
    key: options.key,
    client: options.client ?? createWorkspaceClient(),
    writer,
    initial: options.initial,
    persisted: readPersisted(storage, options.key, writer),
    focus: options.focus,
    persist: state => writePersisted(storage, options.key, writer, state),
  });
}

export function useWorkspaceSync(options: WorkspaceSyncOptions): WorkspaceSync {
  const { key } = options;
  // Built during the first render, so the first paint already shows this window's last tab set for the place.
  const [held, setHeld] = useState(() => ({ key, controller: build(options) }));
  let controller = held.controller;
  if (held.key !== key) {
    controller = build(options);
    setHeld({ key, controller });
  }

  useEffect(() => {
    const active = controller;
    const storage = options.storage ?? safeLocal();
    let live = true;
    let release = () => {};
    void (async () => {
      let writer = active.writer();
      let hold = await holdWindow(writer);
      if (!live) return hold.release();
      if (hold.held === false) {
        // A duplicated browser tab copied this window's name: it takes a new one. The changes it inherited are
        // safe to keep: replays recognise a change already made.
        writer = renameWindow();
        const renamed = writer;
        active.rename(renamed, state => writePersisted(storage, key, renamed, state));
        hold = await holdWindow(writer);
        if (!live) return hold.release();
      }
      release = hold.release;
      const orphans = await adoptOrphans(storage, key, writer);
      if (!live) return orphans.abandon();
      active.adopt(orphans.entries);
      active.persistNow();
      orphans.commit();
      active.start();
    })();
    const retry = () => active.retry();
    const visible = () => { if (document.visibilityState === 'visible') active.retry(); };
    const leaving = () => active.persistNow();
    window.addEventListener('online', retry);
    document.addEventListener('visibilitychange', visible);
    window.addEventListener('pagehide', leaving);
    return () => {
      live = false;
      window.removeEventListener('online', retry);
      document.removeEventListener('visibilitychange', visible);
      window.removeEventListener('pagehide', leaving);
      active.stop();
      release();
    };
    // One lifetime per controller (one per place key); the options are read when it is built.
  }, [controller]);

  const state = useSyncExternalStore(controller.subscribe, controller.getState);
  const status = useSyncExternalStore(controller.subscribe, controller.getStatus);
  return useMemo(() => ({
    state, status,
    dispatch: controller.dispatch, retry: controller.retry, acknowledge: controller.acknowledge,
    scrollOf: controller.scrollOf, setScroll: controller.setScroll, handoff: controller.handoff,
  }), [state, status, controller]);
}
