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
import { routeTask } from '../view-state';
import { questionsFor } from '../preview/content';
import { useOpenElsewhere } from '../preview/useOpenElsewhere';
import { usePreviewShown } from '../preview/usePreviewShown';
import { previewCollisionPadding, usePreviewTrigger } from '../preview/usePreviewTrigger';

/** What went wrong in words a person can act on; the engine's own message when it gave one. */
const failureWords = (error: unknown) => `Not sent. ${error instanceof Error && error.message ? error.message : 'The engine did not answer.'}`;

/**
 * "Act from the preview": answering needs the engine session the tab's summary came from, and only the questions this
 * card shows (a task's card answers its own task's, never the whole conversation's). A failed answer is said, out
 * loud, in the card; the card keeps asking whatever the engine still lists, and nothing is shown as answered.
 */
function useActions(api: TabsApi, tab: Tab): PreviewActions {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const pane = focusedPane(tab);
  const summary = api.summaries[pane.id];
  const taskId = pane.kind === 'task' && pane.route ? routeTask(pane.route) : undefined;
  return {
    busy,
    error,
    review: () => { api.previews.close(tab.id); api.dispatch({ type: 'select', id: tab.id }); },
    allowAll: () => {
      const session = summary?.sessionId;
      if (!session || busy) return;
      setBusy(true);
      setError(undefined);
      void (async () => {
        let latest = summary;
        try {
          for (const answer of bulkAnswers(questionsFor(summary, taskId), 'allow')) {
            latest = summarize(await answerEngine(session, answer));
            api.receiveSummary(pane.id, latest);
          }
          if (questionsFor(latest, taskId).length) setError('Some questions still need you. Review them.');
          else api.previews.close(tab.id);
        } catch (failure) {
          setError(failureWords(failure));
        } finally {
          setBusy(false);
        }
      })();
    },
  };
}

function PreviewBody({ api, tab, enabled }: { api: TabsApi; tab: Tab; enabled: boolean }) {
  const act = useActions(api, tab);
  const pane = focusedPane(tab);
  const elsewhere = useOpenElsewhere(api.workspaceKey, pane, enabled);
  const Body = kindDef(pane.kind).preview;
  return <Body pane={pane} title={tab.title} summary={api.summaries[pane.id]} now={api.now} act={act} openElsewhere={elsewhere}/>;
}

function TabPreview({ api, tab, trigger }: { api: TabsApi; tab: Tab; trigger: ReactElement }) {
  const disabled = api.overlayOpen || tab.id === api.state.activeId;
  const { open, swapped } = usePreviewShown(api.previews, tab.id);
  const handlers = usePreviewTrigger(api.previews, tab.id, open, disabled);
  return (
    <HoverCard open={open && !disabled} collisionPadding={previewCollisionPadding} trigger={trigger} triggerProps={handlers} role="group" aria-label={`Preview of ${tab.title}`} className="tab-preview" data-swap={swapped || undefined} onPointerEnter={api.previews.keepOpen} onPointerLeave={() => api.previews.scheduleClose(tab.id)}>
      <PreviewBody api={api} tab={tab} enabled={open && !disabled}/>
    </HoverCard>
  );
}

export function withPreview(api: TabsApi, tab: Tab, trigger: ReactElement): ReactElement {
  return <TabPreview api={api} tab={tab} trigger={trigger}/>;
}
