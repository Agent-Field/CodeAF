import type { ReactNode } from 'react';
import type { EngineTaskRow } from '../chat/engine-client';
import { goBack, goForward, navigate, type TabRoute } from '../tabs/view-state';
import { Breadcrumb, type BreadcrumbSegment } from './Breadcrumb';
import { taskTrail } from './taskTree';
import { TaskView } from './TaskView';
import type { OpenTask } from './itemRenderer';
import type { TurnItem } from './types';

type Props = {
  sessionId?: string;
  taskId: string;
  tasks: EngineTaskRow[];
  rootLabel: string;
  route: TabRoute;
  onRoute: (route: TabRoute) => void;
  renderItem: (item: TurnItem) => ReactNode;
  onOpenTask: OpenTask;
};

function segmentsFor(tasks: EngineTaskRow[], taskId: string, rootLabel: string): BreadcrumbSegment[] {
  const trail = taskTrail(tasks, taskId).map((row) => ({ id: row.ID, label: row.Title }));
  const own = trail.length ? [] : [{ id: taskId, label: 'Task' }];
  return [{ id: null, label: rootLabel }, ...trail, ...own];
}

/** A task drawn as a page: title, result, the work, the brief; no composer. */
export function TaskRoute(props: Props) {
  const { sessionId, taskId, route, onRoute, renderItem } = props;
  return (
    <div className="task-route">
      <Breadcrumb
        segments={segmentsFor(props.tasks, taskId, props.rootLabel)}
        onNavigate={(id) => onRoute(navigate(route, id ?? undefined))}
        canBack={route.back.length > 0}
        canForward={route.forward.length > 0}
        onBack={() => onRoute(goBack(route))}
        onForward={() => onRoute(goForward(route))}
      />
      {sessionId && <TaskView sessionId={sessionId} taskId={taskId} renderItem={renderItem} onOpenTask={props.onOpenTask} />}
    </div>
  );
}
