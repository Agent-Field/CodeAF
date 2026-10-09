import type { EngineTaskRow } from '../chat/engine-client';
import { goBack, goForward, navigate, type TabRoute } from '../tabs/view-state';
import { Breadcrumb, type BreadcrumbSegment } from './Breadcrumb';
import { taskTrail } from './taskTree';
import type { ReadFile } from './tasks/LogStep';
import type { RenderFile } from './tasks/TaskSections';
import { TaskView } from './TaskView';
import type { OpenTask } from './TaskNotice';

type Props = {
  sessionId?: string;
  taskId: string;
  tasks: EngineTaskRow[];
  rootLabel: string;
  route: TabRoute;
  onRoute: (route: TabRoute) => void;
  renderFile?: RenderFile;
  readFile?: ReadFile;
  onOpenTask: OpenTask;
};

function segmentsFor(tasks: EngineTaskRow[], taskId: string): BreadcrumbSegment[] {
  const trail = taskTrail(tasks, taskId).map((row) => ({ id: row.ID, label: row.Title }));
  const own = trail.length ? [] : [{ id: taskId, label: 'Task' }];
  // The design's trail starts at the parent task: Back, not a root crumb, returns to the conversation.
  return [...trail, ...own];
}

/** The pane's top row in task view: Back and the trail, across the whole pane (design 1c). */
export function TaskRouteBar({ taskId, tasks, route, onRoute }: Pick<Props, 'taskId' | 'tasks' | 'route' | 'onRoute'>) {
  return (
    <div className="task-view-bar">
      <Breadcrumb
        segments={segmentsFor(tasks, taskId)}
        onNavigate={(id) => onRoute(navigate(route, id ?? undefined))}
        canBack={route.back.length > 0}
        canForward={route.forward.length > 0}
        onBack={() => onRoute(goBack(route))}
        onForward={() => onRoute(goForward(route))}
      />
    </div>
  );
}

/** A task drawn as a page: title, result, the work, the notes, the brief, and a composer for notes. */
export function TaskRoute(props: Pick<Props, 'sessionId' | 'taskId' | 'tasks' | 'route' | 'onRoute' | 'renderFile' | 'readFile' | 'onOpenTask'>) {
  const { sessionId, taskId, route, onRoute } = props;
  if (!sessionId) return null;
  return (
    <div className="task-route">
      <TaskView
        sessionId={sessionId}
        taskId={taskId}
        liveRow={props.tasks.find((row) => row.ID === taskId)}
        onOpenTask={props.onOpenTask}
        renderFile={props.renderFile}
        readFile={props.readFile}
        onMessageConversation={() => onRoute(navigate(route, undefined))}
      />
    </div>
  );
}
