import { EndCard } from '../nextup/EndCard';
import { useNextUpWalkState } from '../nextup/useNextUpWalk';
import { nextUpWalk } from '../nextup/useNextUpWalk';
import { worldStore } from '../chat/world-store';
import '../nextup/walk.css';
import { blocksComposer } from './tray/layout';
import { useContext, useEffect, useRef, useState } from 'react';
import { FirstTurnContext, NewConversationPlaceContext } from './firstTurn';
import { Text } from '../../components/ui';
import { useMediaQuery } from '../../design/useMediaQuery';
import design from '../../design/tokens.json';
import { ENGINE_MODEL } from '../chat/engine-client';
import { DEFAULT_MODEL_SHORT, useConversationModel } from './composer/useConversationModel';
import type { Pane } from '../tabs/model';
import { peekOfferedFiles, peekOfferedLink, settleOfferedFiles, settleOfferedLink } from '../web/useWorkspaceWeb';
import { goBack, goForward, navigate, rootRoute, routeTask, TASKS_VIEW, toggleFlag, type TabView } from '../tabs/view-state';
import { useHistoryKeys } from './Breadcrumb';
import type { OutgoingFile } from '../chat/engine-client';
import { EngineAssetProvider } from './assets';
import { renderFile } from './blockRenderer';
import type { SendMode } from './Composer';
import type { NextUpProgress } from '../nextup/NextUpChip';
import { ConversationBar } from './ConversationBar';
import { UsingLine, usingVisible } from '../places/UsingLine';
import { useUsing } from '../places/useUsing';
import type { SourceHandoff, UsingApi } from '../places/using-types';
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
import { questionFocus } from './questionFocus';
import { questionKey } from './model/entry';
import { chatIdFromSessionFile } from '../places/client';
import { contentSignature, useStickToBottom } from './useStickToBottom';
import { useFoldAnchor, useTurnJump } from './turnScroll';
import { shortcutLayer } from '../../design/keyboard';
import { useShortcuts } from '../../design/useShortcuts';
import './conversation-view.css';
import { FirstReplyPlaceOffer } from '../places/FirstReplyPlaceOffer';

export type ConversationViewProps = {
  /** The shell walker supplies progress only to the conversation it is walking. */
  nextUp?: NextUpProgress;
  tab: Pane;
  label: string;
  onDraft: (draft: string) => void;
  onView: (change: TabView) => void;
  onSummary: (summary: TabSummary) => void;
  onOpenTaskTab: (taskId: string, title: string) => void;
  /** Renames this conversation's tab by hand (Conversation I-ICV-6). */
  onRename?: (title: string) => void;
  /** Opens another tab on the same saved session, optionally at an anchor (Flows I-IFL-16). */
  onOpenConversationTab?: (sessionFile: string, anchor?: string, background?: boolean) => void;
  /** Whether the composer takes focus on mount. A split passes false for panes that are not focused. */
  autoFocus?: boolean;
  /** True while the pane is one of several in a split. */
  split?: boolean;
  /** Whether this pane holds the split's focus. Only the focused pane shows the full composer; the others a compact field. */
  focused?: boolean;
  /**
   * The Using list's three calls (`createUsingClient()` from the places runtime client). With none, the conversation draws no Using
   * chip: a capability with nothing behind it is absent, not broken.
   */
  usingApi?: UsingApi;
  /** Where a Using source goes when the person opens it; with none the sources are listed without an open control. */
  onOpenSource?: (handoff: SourceHandoff) => void;
  /** Files this conversation in a place; with none the sheet has no "Add to a place…" row. */
  onAddToPlace?: () => void;
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

export function ConversationView({ nextUp, tab, label, onDraft, onView, onSummary, onOpenTaskTab, onRename, onOpenConversationTab, autoFocus = true, split = false, focused = true, usingApi, onOpenSource, onAddToPlace }: ConversationViewProps) {
  const beforeFirstTurn = useContext(FirstTurnContext);
  const newConversationPlace = useContext(NewConversationPlaceContext);
  const conversation = useConversation({ sessionFile: tab.sessionFile, onSessionFile: (sessionFile) => onView({ sessionFile }), beforeFirstTurn, newConversationPlace });
  const { model, snapshot, failed } = conversation;
  // A refused outbox send comes back into an empty composer. Words typed since are left alone.
  const appliedRestore = useRef<string | undefined>(undefined);
  useEffect(() => {
    const text = conversation.draftToRestore;
    if (!text) { appliedRestore.current = undefined; return; }
    if (appliedRestore.current === text) return;
    appliedRestore.current = text;
    if (tab.draft.trim() === '') onDraft(text);
    conversation.ackDraftRestore();
  }, [conversation.draftToRestore]);
  const walk = useNextUpWalkState();
  const [focusKey, setFocusKey] = useState<string>();
  // A tab opened from a web page brings that page's picture; the composer attaches it once and the offer is spent.
  const [droppedFiles, setDroppedFiles] = useState<File[]>();
  const [offered, setOffered] = useState(() => peekOfferedFiles(tab.id));
  const [pageLink, setPageLink] = useState(() => peekOfferedLink(tab.id));
  const route = tab.route ?? rootRoute;
  const scroller = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const { behind, away, unanchored, scrolled, jump } = useStickToBottom(scroller, content, contentSignature(model), route.taskId ?? '');
  const hasTasks = model.tasks.length > 0 || Boolean(model.planError);
  const panel = useTaskPanel(hasTasks, Boolean(tab.tasksClosed), onView);
  const setRoute = (next: typeof route) => onView({ route: next });
  const taskId = routeTask(route);
  const tasksView = route.taskId === TASKS_VIEW;
  // A sheet covers the reading column. The backdrop catches the pointer; this stops the column itself from moving under it.
  const sheetLocksScroll = panel.shown && panel.sheet && !tasksView;

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

  // A clicked system notification names one question of this conversation: bring it forward once the engine has
  // delivered it, and only that question (questionFocus.ts).
  const chatId = tab.sessionFile ? chatIdFromSessionFile(tab.sessionFile) : '';
  useEffect(() => {
    if (!chatId) return;
    const answer = () => {
      const asked = questionFocus.take(chatId, model.questions);
      if (!asked) return;
      jump();
      focusQuestion(questionKey(asked));
    };
    answer();
    return questionFocus.subscribe(answer);
  }, [chatId, model.questions]);

  async function retry() {
    const text = failed?.text;
    const sent = await conversation.retry();
    if (sent && text && tab.draft.trim() === text) onDraft('');
  }

  const holdRow = useFoldAnchor(scroller, `${tab.folded ? JSON.stringify(tab.folded) : ''}${JSON.stringify(tab.open ?? {})}`);
  const control = (action: 'pause' | 'resume' | 'cancel') => (id: string) => void conversation.controlTask(id, action);
  const blocks = {
    sessionId: snapshot?.id,
    tasks: model.tasks,
    waiting: blocksComposer(model.questions),
    open: tab.open ?? {},
    onToggle: (id: string, current?: boolean) => {
      holdRow(id);
      onView({ open: current === undefined ? toggleFlag(tab.open, id) : { ...tab.open, [id]: !current } });
    },
    readFull: conversation.readFull,
    onOpenTask: openTask,
    onPause: control('pause'),
    onResume: control('resume'),
    onStop: control('cancel'),
    onFocusQuestion: focusQuestion,
  };
  const sessionId = snapshot?.id;
  const readFile = sessionId ? (path: string) => readEngineText(sessionId, path) : undefined;
  const folded = tab.folded ?? {};
  const toggleFold = (id: string, next: boolean) => {
    holdRow(id);
    onView({ folded: { ...tab.folded, [id]: next } });
  };
  const inTask = Boolean(taskId);
  // Leaving the expanded view goes back the way the reader came, or to the conversation.
  const closeTasksView = () => setRoute(route.back.length ? goBack(route) : navigate(route, undefined));
  useTurnJump(scroller, !inTask && !tasksView);
  const empty = !inTask && model.turns.length === 0 && model.preface.length === 0 && !failed && conversation.pendingSends.length === 0;
  const { done, total } = taskCounts(model.tasks);
  const barPanel = hasTasks && !inTask ? { label: `Tasks · ${done}/${total}`, shown: panel.shown, onToggle: panel.shown ? panel.close : panel.open } : undefined;
  // ⌘⇧K toggles the task panel exactly like the header button (design Shell 2h, I-IKY-16). Only the focused pane answers, so a split's
  // other panes do not race for the key; a conversation without tasks has no panel, so the key is left alone.
  useShortcuts(shortcutLayer.surface, shortcut => { if (shortcut.id !== 'tasks-panel' || !barPanel) return false; barPanel.onToggle(); return true; }, !tasksView && focused);
  const barCounts = { running: taskProgress(model.tasks).running, needsYou: model.questions.length };
  // The Using list is re-read when a turn lands or the work starts and stops: a place changed meanwhile applies from the next turn.
  const using = useUsing(usingApi, sessionId, `${model.turns.length}:${model.running}`);
  const barTitle = tab.titleSource ? label : '';
  const showBar = Boolean(nextUp) || model.questions.length > 0 || inTask || Boolean(barTitle) || hasTasks || usingVisible(using);
  const modelLabel = !snapshot || snapshot.model === ENGINE_MODEL ? MODEL_LABEL : snapshot.model;
  const modelShort = modelLabel === MODEL_LABEL ? DEFAULT_MODEL_SHORT : undefined;
  const conversationModel = useConversationModel(snapshot?.model);
  const showJump = unanchored && (behind || model.running) && !inTask;
  const showFooter = !inTask;
  const folder = snapshot?.workingFolder;
  const folderNote = folder?.note ?? (folder?.skipped?.length ? `Skipped ${folder.skipped.length} unavailable ${folder.skipped.length === 1 ? 'place folder' : 'place folders'}. This chat works in ${folder.label || folder.path || snapshot?.workspace}.` : undefined);

  return (
    <EngineAssetProvider sessionId={sessionId} workspace={snapshot?.workspace ?? ''}>
      <div className="conversation-view">
        {tasksView && (
          <ExpandedTasks
            sessionId={sessionId}
            tasks={model.tasks}
            questions={model.questions}
            filter={tab.tasksFilter ?? 'all'}
            onFilter={(tasksFilter) => onView({ tasksFilter })}
            selectedId={tab.tasksSelected}
            onSelect={(id) => onView({ tasksSelected: id })}
            onClose={closeTasksView}
            onOpenTask={openTask}
            busyKey={conversation.busyKey}
            onAnswer={conversation.answer}
            onHold={conversation.hold}
          />
        )}
        {/* Now uses graphite for the empty start without changing the surrounding shell palette. */}
        <div className="conversation-main" data-empty={empty || undefined} data-tint={empty && !newConversationPlace ? 'graphite' : undefined} data-sheet={sheetLocksScroll || undefined} hidden={tasksView}>
          {showBar && (
            <ConversationBar nextUp={nextUp} onTasksFilter={(tasksFilter) => { if (panel.sheet) panel.close(); onView({ tasksFilter, route: navigate(route, TASKS_VIEW) }); }} lead={inTask ? 'trail' : 'title'} title={barTitle || undefined} onRename={barTitle ? onRename : undefined} counts={barCounts} panel={barPanel} using={<UsingLine control={using} onOpenSource={onOpenSource} onAddToPlace={onAddToPlace} onDropFiles={setDroppedFiles} />}>
              {taskId ? <TaskRouteBar rootLabel={label} taskId={taskId} tasks={model.tasks} route={route} onRoute={setRoute} /> : null}
            </ConversationBar>
          )}
          {/* Under the header, including when the header itself is absent: the line is the pane's top notice (I-IFL-3). */}
          <EngineNotice active={conversation.unreachable} onProbe={() => conversation.reprobe()} />
          <div ref={scroller} className="conversation-scroll" data-scroll-key={inTask ? 'task-page' : 'conversation'} data-scrolled={scrolled || undefined} data-task={inTask || undefined}>
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
                  onOpenConversationTab={tab.sessionFile && onOpenConversationTab ? (anchor, background) => onOpenConversationTab?.(tab.sessionFile!, anchor, background) : undefined}
                  firstReplyAside={tab.sessionFile && model.turns[0]?.state === 'done' && model.turns[0].blocks.some(block => block.kind === 'answer') ? <FirstReplyPlaceOffer key={tab.sessionFile} sessionFile={tab.sessionFile} focused={focused}/> : undefined}
                  folded={folded}
                  onToggleFold={toggleFold}
                  failed={conversation.unreachable ? undefined : failed}
                  sending={conversation.pendingSends}
                  onRetry={() => void retry()}
                />
              )}
            </div>
          </div>
          {showFooter && (
            <div className="conversation-dock-layer">
              {showJump && <LatestPill working={model.running} since={liveSince(model)} onJump={jump} />}
              <div className="conversation-footer conversation-column">
                {focused && walk.phase === 'clear' && walk.origin && <EndCard answeredCount={walk.answered} conversations={worldStore.getState().rows.map(row => ({ tasksRunning: row.tasks.running }))} originLabel={walk.origin.label} onReturn={() => nextUpWalk.exit()} />}
                {folderNote && <Text role="status">{folderNote}</Text>}
                {!inTask && (
                  <ConversationDock
                    compact={split && !focused ? { label } : undefined}
                    tray={{ questions: model.questions, busyKey: conversation.busyKey, onAnswer: conversation.answer, onHold: conversation.hold, focusKey, compact: away, onReview: jump }}
                    queue={{ items: snapshot?.queue ?? [], onRemove: conversation.removeQueue, onEdit: conversation.editQueue, onMove: conversation.moveQueue, onSendNow: conversation.sendQueueNow }}
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
                      sessionId,
                      offeredFiles: offered,
                      droppedFiles,
                      onDroppedFiles: () => setDroppedFiles(undefined),
                      onOfferedFiles: () => { settleOfferedFiles(tab.id); setOffered([]); },
                      offeredLink: pageLink,
                      onOfferedLink: () => { settleOfferedLink(tab.id); setPageLink(undefined); },
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
