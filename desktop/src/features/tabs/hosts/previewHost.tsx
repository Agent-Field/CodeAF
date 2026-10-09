// Hover-preview host (owned by the hover-preview lane). Called with the tab's select button; returns it wrapped.
// Hovering an inactive tab for 500ms opens a 300px text card of its kind under the strip (Shell 2h, 3a, 3k).
import { useState, type ReactElement } from 'react';
import { HoverCard } from '../../../components/ui';
import { answerEngine } from '../../chat/engine-client';
import { summarize } from '../../conversation/tabSummary';
import { bulkAnswers } from '../../conversation/tray/answers';
import type { TabsApi } from '../context';
import { kindDef } from '../kinds/registry';
import type { PreviewActions } from '../kinds/slots';
import { focusedPane, type Tab } from '../model';
import { closePreview, keepOpen, scheduleClose, usePreviewShown } from '../preview/previewStore';
import { usePreviewTrigger } from '../preview/usePreviewTrigger';

/** "Act from the preview": answering needs the engine session the tab's summary came from. */
function useActions(api: TabsApi, tab: Tab): PreviewActions {
  const [busy, setBusy] = useState(false);
  const pane = focusedPane(tab);
  const summary = api.summaries[pane.id];
  return {
    busy,
    review: () => { closePreview(tab.id); api.dispatch({ type: 'select', id: tab.id }); },
    allowAll: () => {
      const session = summary?.sessionId;
      if (!session || busy) return;
      setBusy(true);
      void (async () => {
        try {
          for (const answer of bulkAnswers(summary?.questions ?? [], 'allow')) api.receiveSummary(pane.id, summarize(await answerEngine(session, answer)));
          closePreview(tab.id);
        } catch {
          // The card stays, still asking: the answer did not go through and nothing is shown as answered.
        } finally {
          setBusy(false);
        }
      })();
    },
  };
}

function PreviewBody({ api, tab }: { api: TabsApi; tab: Tab }) {
  const act = useActions(api, tab);
  const pane = focusedPane(tab);
  const Body = kindDef(pane.kind).preview;
  return <Body pane={pane} title={tab.title} summary={api.summaries[pane.id]} now={api.now} act={act}/>;
}

function TabPreview({ api, tab, trigger }: { api: TabsApi; tab: Tab; trigger: ReactElement }) {
  const disabled = api.overlayOpen || tab.id === api.state.activeId;
  const { open, swapped } = usePreviewShown(tab.id);
  const handlers = usePreviewTrigger(tab.id, open, disabled);
  return (
    <HoverCard open={open && !disabled} trigger={trigger} triggerProps={handlers} role="group" aria-label={`Preview of ${tab.title}`} className="tab-preview" data-swap={swapped || undefined} onPointerEnter={keepOpen} onPointerLeave={() => scheduleClose(tab.id)}>
      <PreviewBody api={api} tab={tab}/>
    </HoverCard>
  );
}

export function withPreview(api: TabsApi, tab: Tab, trigger: ReactElement): ReactElement {
  return <TabPreview api={api} tab={tab} trigger={trigger}/>;
}
