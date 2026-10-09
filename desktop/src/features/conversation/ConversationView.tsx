import { useEffect, useRef, useState } from 'react';
import { Button, Icon } from '../../components/ui';
import { useMediaQuery } from '../../design/useMediaQuery';
import design from '../../design/tokens.json';
import { ENGINE_MODEL } from '../chat/engine-client';
import type { Tab } from '../tabs/model';
import { goBack, goForward, navigate, rootRoute, toggleFlag, type TabView } from '../tabs/view-state';
import { useHistoryKeys } from './Breadcrumb';
import type { OutgoingFile } from '../chat/engine-client';
import { EngineAssetProvider } from './assets';
import { renderFile } from './blockRenderer';
import type { SendMode } from './Composer';
import { ConversationDock } from './ConversationDock';
import { ConversationTranscript } from './ConversationTranscript';
import { EngineNotice } from './EngineNotice';
import { summarize, type TabSummary } from './tabSummary';
import { TaskPanel } from './TaskPanel';
import { TaskRoute } from './TaskRoute';
import { taskCounts } from './taskTree';
import { useConversation } from './useConversation';
import { readEngineText } from './tasks/readText';
import { useQueued } from './useQueued';
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
  const queued = useQueued(snapshot?.entries ?? []);
  const [focusKey, setFocusKey] = useState<string>();
  const route = tab.route ?? rootRoute;
  const scroller = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const { behind, jump } = useStickToBottom(scroller, content, contentSignature(model), route.taskId ?? '');
  const hasTasks = model.tasks.length > 0 || Boolean(model.planError);
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

  // The person's own message always brings the end of the conversation into view.
  async function sendAndFollow(text: string, mode: SendMode, files?: OutgoingFile[]) {
    jump();
    const sent = await conversation.send(text, mode, files);
    if (sent && mode === 'queue') queued.add(text);
    return sent;
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

  const blocks = {
    tasks: model.tasks,
    open: tab.open ?? {},
    onToggle: (id: string) => onView({ open: toggleFlag(tab.open, id) }),
    readFull: conversation.readFull,
    onOpenTask: openTask,
    onFocusQuestion: focusQuestion,
  };
  const sessionId = snapshot?.id;
  const readFile = sessionId ? (path: string) => readEngineText(sessionId, path) : undefined;
  const control = (action: 'pause' | 'resume' | 'cancel') => (taskId: string) => void conversation.controlTask(taskId, action);
  const folded = tab.folded ?? {};
  const toggleFold = (id: string) => onView({ folded: toggleFlag(tab.folded, id) });
  const inTask = Boolean(route.taskId);
  const empty = !inTask && model.turns.length === 0 && model.preface.length === 0 && !failed;
  const { done, total } = taskCounts(model.tasks);
  const tasksToggle = hasTasks && !panel.shown ? { label: `Tasks · ${done}/${total}`, onClick: panel.open } : undefined;
  const modelLabel = !snapshot || snapshot.model === ENGINE_MODEL ? MODEL_LABEL : snapshot.model;
  const showJump = behind && !inTask;
  const showFooter = !inTask || conversation.unreachable;
  const greeting = empty ? folderName(snapshot?.workspace) : '';

  return (
    <EngineAssetProvider sessionId={sessionId} workspace={snapshot?.workspace ?? ''}>
      <div className="conversation-view">
        <div className="conversation-main" data-empty={empty || undefined}>
          <div ref={scroller} className="conversation-scroll">
            <div ref={content} className="conversation-column">
              {greeting && <p className="conversation-greeting">{greeting}</p>}
              {route.taskId ? (
                <TaskRoute
                  sessionId={sessionId}
                  taskId={route.taskId}
                  tasks={model.tasks}
                  rootLabel={label}
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
                  onRetry={() => void retry()}
                />
              )}
            </div>
          </div>
          {showFooter && (
            <div className="conversation-footer conversation-column">
              {showJump && (
                <Button className="conversation-jump" onClick={jump}>
                  <Icon name="arrowDown" size="xs" />
                  <span>New messages</span>
                </Button>
              )}
              {conversation.unreachable && <EngineNotice onRetry={() => void retry()} />}
              {!inTask && (
                <ConversationDock
                  tray={{ questions: model.questions, busyKey: conversation.busyKey, onAnswer: conversation.answer, onHold: conversation.hold, focusKey }}
                  queue={{ items: queued.items, onRemove: queued.remove, removedHere: queued.removedHere }}
                  composer={{
                    draft: tab.draft,
                    onDraft,
                    onSend: sendAndFollow,
                    onStop: () => void conversation.stop(),
                    running: model.running,
                    docked: !empty,
                    modelLabel,
                    tasksToggle,
                    recallLast: () => model.turns[model.turns.length - 1]?.user,
                    autoFocus: true,
                  }}
                />
              )}
            </div>
          )}
        </div>
        {panel.shown && panel.sheet && <div className="task-panel-backdrop" aria-hidden="true" onClick={panel.close} />}
        {panel.shown && (
          <TaskPanel
            tasks={model.tasks}
            planError={model.planError}
            currentTaskId={route.taskId}
            onOpenTask={openTask}
            onClose={panel.close}
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
