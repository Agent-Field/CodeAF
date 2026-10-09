import { useEffect, useRef, useState } from 'react';
import { useMediaQuery } from '../../design/useMediaQuery';
import design from '../../design/tokens.json';
import { ENGINE_MODEL } from '../chat/engine-client';
import { DEFAULT_MODEL_SHORT, useConversationModel } from './composer/useConversationModel';
import type { Pane } from '../tabs/model';
import { goBack, goForward, navigate, rootRoute, routeTask, TASKS_VIEW, toggleFlag, type TabView } from '../tabs/view-state';
import { useHistoryKeys } from './Breadcrumb';
import type { OutgoingFile } from '../chat/engine-client';
import { EngineAssetProvider } from './assets';
import { renderFile } from './blockRenderer';
import type { SendMode } from './Composer';
import { ConversationBar } from './ConversationBar';
import { ConversationDock } from './ConversationDock';
import { ConversationTranscript } from './ConversationTranscript';
import { EngineNotice } from './EngineNotice';
import { ExpandedTasks } from './ExpandedTasks';
import { LatestPill, liveSince } from './LatestPill';
import { summarize, type TabSummary } from './tabSummary';
import { TaskPanel } from './TaskPanel';
import { TaskRoute, TaskRouteBar } from './TaskRoute';
import { taskCounts, taskProgress } from './taskTree';
import { useConversation } from './useConversation';
import { readEngineText } from './tasks/readText';
import { contentSignature, useStickToBottom } from './useStickToBottom';
import { useFoldAnchor, useTurnJump } from './turnScroll';
import { shortcutLayer } from '../../design/keyboard';
import { useShortcuts } from '../../design/useShortcuts';
import './conversation-view.css';

export type ConversationViewProps = {
  tab: Pane;
  label: string;
  onDraft: (draft: string) => void;
  onView: (change: TabView) => void;
  onSummary: (summary: TabSummary) => void;
  onOpenTaskTab: (taskId: string, title: string) => void;
  /** Whether the composer takes focus on mount. A split passes false for panes that are not focused. */
  autoFocus?: boolean;
  /** True while the pane is one of several in a split. */
  split?: boolean;
  /** Whether this pane holds the split's focus. Only the focused pane shows the full composer; the others a compact field. */
  focused?: boolean;
};

const MODEL_LABEL = 'DeepSeek v4.1 Flash';
/** A fresh tab has asked the engine nothing yet, so its chip reads the default model's pinned label from the start. */

/** The task panel: a column on wide panes, a sheet the person opens on narrow ones. */
function useTaskPanel(hasTasks: boolean, closed: boolean, onView: ConversationViewProps['onView']) {
  const sheet = useMediaQuery(`(max-width: ${design.breakpoints.planSplit}px)`);
  const [sheetOpen, setSheetOpen] = useState(false);
  const shown = hasTasks && (sheet ? sheetOpen : !closed);
  const close = () => (sheet ? setSheetOpen(false) : onView({ tasksClosed: true }));
  const open = () => (sheet ? setSheetOpen(true) : onView({ tasksClosed: false }));
  return { sheet, shown, close, open };
}

export function ConversationView({ tab, label, onDraft, onView, onSummary, onOpenTaskTab, autoFocus = true, split = false, focused = true }: ConversationViewProps) {
  const conversation = useConversation({ sessionFile: tab.sessionFile, onSessionFile: (sessionFile) => onView({ sessionFile }) });
  const { model, snapshot, failed } = conversation;
  const [focusKey, setFocusKey] = useState<string>();
  const route = tab.route ?? rootRoute;
  const scroller = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const { behind, away, unanchored, scrolled, jump } = useStickToBottom(scroller, content, contentSignature(model), route.taskId ?? '');
  const hasTasks = model.tasks.length > 0 || Boolean(model.planError);
  const panel = useTaskPanel(hasTasks, Boolean(tab.tasksClosed), onView);
  const setRoute = (next: typeof route) => onView({ route: next });
  const taskId = routeTask(route);
  const tasksView = route.taskId === TASKS_VIEW;

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

  // The person's own message always brings the end of the conversation into view.
  async function sendAndFollow(text: string, mode: SendMode, files?: OutgoingFile[]) {
    jump();
    return conversation.send(text, mode, files);
  }

  // A receipt brings its question forward; clearing first lets the same receipt work twice.
  function focusQuestion(key: string) {
    setFocusKey(undefined);
    window.setTimeout(() => setFocusKey(key), 0);
  }

  async function retry() {
    const text = failed?.text;
    const sent = await conversation.retry();
    if (sent && text && tab.draft.trim() === text) onDraft('');
  }

  const holdRow = useFoldAnchor(scroller, `${tab.folded ? JSON.stringify(tab.folded) : ''}${JSON.stringify(tab.open ?? {})}`);
  const blocks = {
    tasks: model.tasks,
    open: tab.open ?? {},
    onToggle: (id: string) => {
      holdRow(id);
      onView({ open: toggleFlag(tab.open, id) });
    },
    readFull: conversation.readFull,
    onOpenTask: openTask,
    onFocusQuestion: focusQuestion,
  };
  const sessionId = snapshot?.id;
  const readFile = sessionId ? (path: string) => readEngineText(sessionId, path) : undefined;
  const control = (action: 'pause' | 'resume' | 'cancel') => (taskId: string) => void conversation.controlTask(taskId, action);
  const folded = tab.folded ?? {};
  const toggleFold = (id: string, next: boolean) => {
    holdRow(id);
    onView({ folded: { ...tab.folded, [id]: next } });
  };
  const inTask = Boolean(taskId);
  // Leaving the expanded view goes back the way the reader came, or to the conversation.
  const closeTasksView = () => setRoute(route.back.length ? goBack(route) : navigate(route, undefined));
  useTurnJump(scroller, !inTask && !tasksView);
  const empty = !inTask && model.turns.length === 0 && model.preface.length === 0 && !failed;
  const { done, total } = taskCounts(model.tasks);
  const barPanel = hasTasks && !inTask ? { label: `Tasks · ${done}/${total}`, shown: panel.shown, onToggle: panel.shown ? panel.close : panel.open } : undefined;
  // ⌘⇧K toggles the task panel (design Shell 2h); a conversation without tasks has no panel, so the key is left alone.
  useShortcuts(shortcutLayer.surface, shortcut => { if (shortcut.id !== 'tasks' || !barPanel) return false; barPanel.onToggle(); return true; }, !tasksView);
  const barCounts = { running: taskProgress(model.tasks).running, needsYou: model.questions.length };
  const barTitle = tab.titleSource ? label : '';
  const showBar = inTask || Boolean(barTitle) || hasTasks;
  const modelLabel = !snapshot || snapshot.model === ENGINE_MODEL ? MODEL_LABEL : snapshot.model;
  const modelShort = modelLabel === MODEL_LABEL ? DEFAULT_MODEL_SHORT : undefined;
  const conversationModel = useConversationModel(snapshot?.model);
  const showJump = unanchored && (behind || model.running) && !inTask;
  const showFooter = !inTask || conversation.unreachable;

  return (
    <EngineAssetProvider sessionId={sessionId} workspace={snapshot?.workspace ?? ''}>
      <div className="conversation-view">
        {tasksView && (
          <ExpandedTasks
            sessionId={sessionId}
            tasks={model.tasks}
            questions={model.questions}
            selectedId={tab.tasksSelected}
            onSelect={(id) => onView({ tasksSelected: id })}
            onClose={closeTasksView}
            onOpenTask={openTask}
            busyKey={conversation.busyKey}
            onAnswer={conversation.answer}
            onHold={conversation.hold}
          />
        )}
        <div className="conversation-main" data-empty={empty || undefined} hidden={tasksView}>
          {showBar && (
            <ConversationBar lead={inTask ? 'trail' : 'title'} counts={barCounts} panel={barPanel}>
              {taskId ? <TaskRouteBar taskId={taskId} tasks={model.tasks} route={route} onRoute={setRoute} /> : <span className="conversation-bar-title">{barTitle}</span>}
            </ConversationBar>
          )}
          <div ref={scroller} className="conversation-scroll" data-scrolled={scrolled || undefined} data-task={inTask || undefined}>
            <div ref={content} className="conversation-column">
              {taskId ? (
                <TaskRoute
                  sessionId={sessionId}
                  taskId={taskId}
                  tasks={model.tasks}
                  route={route}
                  onRoute={setRoute}
                  renderFile={(path) => renderFile(path)}
                  readFile={readFile}
                  onOpenTask={openTask}
                />
              ) : (
                <ConversationTranscript
                  {...blocks}
                  model={model}
                  folded={folded}
                  onToggleFold={toggleFold}
                  failed={conversation.unreachable ? undefined : failed}
                  sending={conversation.sending}
                  onRetry={() => void retry()}
                />
              )}
            </div>
          </div>
          {showFooter && (
            <div className="conversation-dock-layer">
              {showJump && <LatestPill working={model.running} since={liveSince(model)} onJump={jump} />}
              <div className="conversation-footer conversation-column">
                {conversation.unreachable && <EngineNotice onRetry={() => void retry()} />}
                {!inTask && (
                  <ConversationDock
                    compact={split && !focused ? { label } : undefined}
                    tray={{ questions: model.questions, busyKey: conversation.busyKey, onAnswer: conversation.answer, onHold: conversation.hold, focusKey, compact: away, onReview: jump }}
                    queue={{ items: snapshot?.queue ?? [], onRemove: conversation.removeQueue, onEdit: conversation.editQueue, onMove: conversation.moveQueue }}
                    composer={{
                      draft: tab.draft,
                      onDraft,
                      onSend: sendAndFollow,
                      onStop: () => void conversation.stop(),
                      running: model.running,
                      docked: !empty,
                      modelLabel,
                      modelShort,
                      model: conversationModel,
                      recallLast: () => model.turns[model.turns.length - 1]?.user,
                      autoFocus,
                    }}
                  />
                )}
              </div>
            </div>
          )}
        </div>
        {panel.shown && panel.sheet && !tasksView && <div className="task-panel-backdrop" aria-hidden="true" onClick={panel.close} />}
        {panel.shown && !tasksView && (
          <TaskPanel
            tasks={model.tasks}
            planError={model.planError}
            currentTaskId={taskId}
            onOpenTask={openTask}
            onClose={panel.close}
            onExpand={() => {
              if (panel.sheet) panel.close();
              setRoute(navigate(route, TASKS_VIEW));
            }}
            variant={panel.sheet ? 'sheet' : 'column'}
            onPause={control('pause')}
            onResume={control('resume')}
            onStop={control('cancel')}
          />
        )}
      </div>
    </EngineAssetProvider>
  );
}
