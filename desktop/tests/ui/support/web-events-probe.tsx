import { createRoot } from 'react-dom/client';
import type { WebState } from '../../../src/design/nativeWeb';
import type { TabSummary } from '../../../src/features/conversation/tabSummary';
import type { PaneActions } from '../../../src/features/tabs/kinds/slots';
import { useWebEvents } from '../../../src/features/web/useWebEvents';

/** Observes the hook's public workspace callbacks without exposing application state. */
export function mountWebEventsProbe() {
  const node = document.createElement('div');
  document.body.append(node);
  const root = createRoot(node);
  const summaries: TabSummary[] = [];
  const views: Parameters<PaneActions['onView']>[0][] = [];
  function Probe({ state, url }: { state: WebState | null; url?: string }) {
    // A fresh actions object models the real workspace callbacks on each render.
    useWebEvents(state, url, {
      onSummary: summary => summaries.push(summary), onView: view => views.push(view),
      onDraft: () => {}, onOpenTask: () => {}, onOpenConversationTab: () => {}, onOpenFile: () => {},
    });
    return null;
  }
  return {
    summaries, views,
    render: (state: WebState | null, url?: string) => root.render(<Probe state={state} url={url}/>),
    dispose: () => { root.unmount(); node.remove(); },
  };
}
