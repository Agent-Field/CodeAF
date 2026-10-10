import { useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { Breadcrumb, useHistoryKeys } from '../../../src/features/conversation/Breadcrumb';
import { TaskRouteBar } from '../../../src/features/conversation/TaskRoute';
import { createFocusWire, FocusHistoryProvider } from '../../../src/features/focus-history/useFocusHistory';
import { navigate, rootRoute, type TabRoute } from '../../../src/features/tabs/view-state';
import { taskRows } from './scenarios';
import '../../../src/styles/tokens.css';
import '../../../src/styles/ui.css';

/** The shell owns observation; the header and shortcut must travel its stack without adding a step. */
export function mountInTabBack(theme: string) {
  localStorage.setItem('codeaf-theme', theme);
  document.body.dataset.theme = theme;
  const host = document.createElement('main');
  document.body.replaceChildren(host);
  const wire = createFocusWire({ load: () => null, save: () => {}, navigate: entry => {
    host.dataset.destination = entry.drillPath.at(-1) ?? entry.tabId;
    host.dataset.cursor = String(wire.getSnapshot().history.cursor);
    host.dataset.steps = String(wire.getSnapshot().history.entries.length);
  } });
  wire.setTabs('now', ['chat', 'other']);
  for (const [tabId, drillPath] of [['chat', []], ['other', []], ['chat', ['task']]] as const) {
    wire.observe({ windowPlace: 'now', tabId, drillPath });
  }
  function Header() {
    useHistoryKeys(() => { host.dataset.destination = 'local-back'; }, () => { host.dataset.destination = 'local-forward'; });
    return <Breadcrumb segments={[{ id: null, label: 'Config stack' }, { id: 'task', label: 'Update fixtures' }]}
      canBack canForward={false} onNavigate={() => {}} onBack={() => { host.dataset.destination = 'local-back'; }} onForward={() => {}} />;
  }
  createRoot(host).render(<ThemeProvider><FocusHistoryProvider wire={wire}><Header /></FocusHistoryProvider></ThemeProvider>);
}

/** Standalone conversation routing remains usable while the shell history integration is absent. */
export function mountLocalTaskBack(theme: string) {
  localStorage.setItem('codeaf-theme', theme);
  document.body.dataset.theme = theme;
  const host = document.createElement('main');
  document.body.replaceChildren(host);
  function Header() {
    const [route, setRoute] = useState<TabRoute>(() => navigate(rootRoute, 'task'));
    useEffect(() => { host.dataset.route = route.taskId ?? 'chat'; host.dataset.back = JSON.stringify(route.back); }, [route]);
    return <TaskRouteBar taskId={route.taskId ?? 'task'} tasks={[{ ...taskRows[0], ID: 'task', Title: 'Update fixtures' }]} rootLabel="Config stack" route={route} onRoute={setRoute} />;
  }
  createRoot(host).render(<ThemeProvider><Header /></ThemeProvider>);
}
