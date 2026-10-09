// A bare window on one place: the real useWorkspaceSync hook, the real client and the real bridge behind the dev
// proxy. Tests drive it with real WorkspaceActions (window.__ws.dispatch) and by typing into the draft field.
// The key comes from ?key=…; with no key saved anywhere, `now` imports the v1 localStorage tab set exactly as the
// app would, and any other key starts from one tab named after the key.
import { useEffect, useLayoutEffect, useRef } from 'react';
import { createRoot } from 'react-dom/client';
import { readWorkspace, visibleTabs, type WorkspaceAction, type WorkspaceState } from '../../../src/features/tabs/model';
import { newTab } from '../../../src/features/tabs/helpers';
import { isWorkspaceKey, type WorkspaceKey } from '../../../src/features/workspace-sync/client';
import { useWorkspaceSync } from '../../../src/features/workspace-sync/useWorkspaceSync';

const params = new URLSearchParams(location.search);
const raw = params.get('key') ?? 'now';
const key: WorkspaceKey = isWorkspaceKey(raw) ? raw : 'now';
const focus = params.get('focus') ?? undefined;
const initial = (): WorkspaceState => {
  if (key === 'now') return readWorkspace();
  const tab = newTab({ id: `${key}-home`, title: 'Home', pinned: true });
  return { tabs: [tab], groups: [], closed: [], activeId: tab.id, nextNumber: 2, recentIds: [tab.id] };
};

declare global {
  interface Window { __ws?: { dispatch: (a: WorkspaceAction) => void; state: () => WorkspaceState; handoff: (id: string) => unknown; retry: () => void } }
}

function Window() {
  const sync = useWorkspaceSync({ key, initial, focus });
  const { state, status } = sync;
  const live = useRef(sync);
  live.current = sync;
  useEffect(() => {
    window.__ws = { dispatch: a => live.current.dispatch(a), state: () => live.current.state, handoff: id => live.current.handoff(id), retry: () => live.current.retry() };
  }, []);
  const active = state.tabs.find(t => t.id === state.activeId) ?? state.tabs[0];
  const scroller = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const top = sync.scrollOf(active.id);
    if (scroller.current) scroller.current.scrollTop = top ?? 0;
  }, [active.id]);
  return (
    <main>
      <p data-testid="status" data-phase={status.phase} data-overtaken={status.overtaken} data-unsaved={status.unsaved}>{status.phase}{status.error ? ` · ${status.error}` : ''}</p>
      <ol data-testid="strip">
        {visibleTabs(state).map(tab => (
          <li key={tab.id} data-testid="tab" data-id={tab.id} data-active={tab.id === state.activeId} data-pinned={tab.pinned} data-group={state.groups.find(g => g.id === tab.groupId)?.title ?? ''} data-draft={tab.draft} data-panes={tab.split?.panes.map(p => p.id).join(',') ?? ''}>
            {tab.title}
          </li>
        ))}
      </ol>
      <p data-testid="closed">{state.closed.map(t => t.id).join(',')}</p>
      <textarea aria-label="Draft" value={active.draft} onChange={e => sync.dispatch({ type: 'draft', id: active.id, draft: e.target.value })} />
      <div ref={scroller} data-testid="scroller" style={{ height: 120, overflow: 'auto' }} onScroll={e => sync.setScroll(active.id, e.currentTarget.scrollTop)}>
        <div style={{ height: 3000 }}>{active.title}</div>
      </div>
    </main>
  );
}

createRoot(document.getElementById('root')!).render(<Window />);
