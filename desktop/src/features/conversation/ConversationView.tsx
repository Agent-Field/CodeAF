import { useEffect, useRef, useState } from 'react';
import { Button, Icon } from '../../components/ui';
import { useMediaQuery } from '../../design/useMediaQuery';
import design from '../../design/tokens.json';
import { ENGINE_MODEL } from '../chat/engine-client';
import type { Tab } from '../tabs/model';
import { goBack, goForward, navigate, rootRoute, toggleFlag, type TabView } from '../tabs/view-state';
import { useHistoryKeys } from './Breadcrumb';
import { Composer } from './Composer';
import { ConversationTranscript } from './ConversationTranscript';
import { EngineNotice } from './EngineNotice';
import { itemRenderer } from './itemRenderer';
import { summarize, type TabSummary } from './tabSummary';
import { TaskPanel } from './TaskPanel';
import { TaskRoute } from './TaskRoute';
import { taskCounts } from './taskTree';
import { useConversation } from './useConversation';
import { contentSignature, useStickToBottom } from './useStickToBottom';
import './conversation-view.css';

export type ConversationViewProps = {
  tab: Tab;
  label: string;
  onDraft: (draft: string) => void;
  onView: (change: TabView) => void;
  onSummary: (summary: TabSummary) => void;
  onOpenTaskTab: (taskId: string, title: string) => void;
};

const MODEL_LABEL = 'DeepSeek v4.1 Flash';
const TASK_REASON = 'Message the main conversation to change this task';

function folderName(path?: string): string {
  return path?.split(/[\\/]/).filter(Boolean).pop() ?? '';
}

/** The task panel: a column on wide panes, a sheet the person opens on narrow ones. */
function useTaskPanel(hasTasks: boolean, closed: boolean, onView: ConversationViewProps['onView']) {
  const sheet = useMediaQuery(`(max-width: ${design.breakpoints.planSplit}px)`);
  const [sheetOpen, setSheetOpen] = useState(false);
  const shown = hasTasks && (sheet ? sheetOpen : !closed);
  const close = () => (sheet ? setSheetOpen(false) : onView({ tasksClosed: true }));
  const open = () => (sheet ? setSheetOpen(true) : onView({ tasksClosed: false }));
  return { sheet, shown, close, open };
}

export function ConversationView({ tab, label, onDraft, onView, onSummary, onOpenTaskTab }: ConversationViewProps) {
  const conversation = useConversation({ sessionFile: tab.sessionFile, onSessionFile: (sessionFile) => onView({ sessionFile }) });
  const { model, snapshot, failed } = conversation;
  const route = tab.route ?? rootRoute;
  const scroller = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const { behind, jump } = useStickToBottom(scroller, content, contentSignature(model));
  const hasTasks = model.tasks.some((row) => !row.Archived) || Boolean(model.planError);
  const panel = useTaskPanel(hasTasks, Boolean(tab.tasksClosed), onView);
  const setRoute = (next: typeof route) => onView({ route: next });

  useEffect(() => {
    if (snapshot) onSummary(summarize(snapshot, Boolean(failed?.text)));
  }, [snapshot, failed]);
  useHistoryKeys(() => setRoute(goBack(route)), () => setRoute(goForward(route)));

  function openTask(taskId: string, background: boolean) {
    if (background) {
      const row = model.tasks.find((candidate) => candidate.ID === taskId);
      onOpenTaskTab(taskId, row?.Title ?? label);
      return;
    }
    if (panel.sheet) panel.close();
    setRoute(navigate(route, taskId));
  }

  async function retry() {
    const text = failed?.text;
    const sent = await conversation.retry();
    if (sent && text && tab.draft.trim() === text) onDraft('');
  }

  const renderItem = itemRenderer({
    open: tab.open ?? {},
    onToggle: (id) => onView({ open: toggleFlag(tab.open, id) }),
    readFull: conversation.readFull,
    onOpenTask: openTask,
  });
  const folded = tab.folded ?? {};
  const toggleFold = (id: string) => onView({ folded: toggleFlag(tab.folded, id) });
  const inTask = Boolean(route.taskId);
  const empty = !inTask && model.turns.length === 0 && model.preface.length === 0 && !failed;
  const { done, total } = taskCounts(model.tasks);
  const tasksToggle = hasTasks && !panel.shown ? { label: `Tasks · ${done}/${total}`, onClick: panel.open } : undefined;
  const modelLabel = !snapshot || snapshot.model === ENGINE_MODEL ? MODEL_LABEL : snapshot.model;
  const greeting = empty ? folderName(snapshot?.workspace) : '';

  return (
    <div className="conversation-view">
      <div className="conversation-main" data-empty={empty || undefined}>
        <div ref={scroller} className="conversation-scroll">
          <div ref={content} className="conversation-column">
            {greeting && <p className="conversation-greeting">{greeting}</p>}
            {route.taskId ? (
              <TaskRoute
                sessionId={snapshot?.id}
                taskId={route.taskId}
                tasks={model.tasks}
                rootLabel={label}
                route={route}
                onRoute={setRoute}
                folded={folded}
                onToggleFold={toggleFold}
                renderItem={renderItem}
                onOpenTask={openTask}
              />
            ) : (
              <ConversationTranscript
                model={model}
                folded={folded}
                onToggleFold={toggleFold}
                renderItem={renderItem}
                failed={conversation.unreachable ? undefined : failed}
                onRetry={() => void retry()}
                answering={conversation.answering}
                onAnswer={conversation.answer}
              />
            )}
          </div>
        </div>
        <div className="conversation-footer conversation-column">
          {behind && (
            <Button className="conversation-jump" onClick={jump}>
              <Icon name="arrowDown" size="xs" />
              <span>New messages</span>
            </Button>
          )}
          {conversation.unreachable && <EngineNotice onRetry={() => void retry()} />}
          <Composer
            draft={inTask ? '' : tab.draft}
            onDraft={onDraft}
            onSend={conversation.send}
            onStop={() => void conversation.stop()}
            running={model.running}
            docked={!empty}
            disabledReason={inTask ? TASK_REASON : undefined}
            reasonAction={inTask ? { label: 'Back to conversation', onClick: () => setRoute(navigate(route)) } : undefined}
            modelLabel={modelLabel}
            tasksToggle={tasksToggle}
            recallLast={() => model.turns[model.turns.length - 1]?.user}
            autoFocus={!inTask}
          />
        </div>
      </div>
      {panel.shown && (
        <TaskPanel
          tasks={model.tasks}
          planError={model.planError}
          currentTaskId={route.taskId}
          onOpenTask={openTask}
          onClose={panel.close}
          variant={panel.sheet ? 'sheet' : 'column'}
        />
      )}
    </div>
  );
}
