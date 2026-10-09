import type { EngineTaskRow } from '../../chat/engine-client';
import { branchCounts, type TaskNode } from '../taskTree';
import type { TaskCommands } from './taskMenu';
import { TaskRow } from './TaskRow';
import './task-tree.css';

export type TaskTreeProps = {
  nodes: TaskNode[];
  rows: EngineTaskRow[];
  now: number;
  collapsed: Set<string>;
  currentTaskId?: string;
  onToggle: (id: string) => void;
  commands: TaskCommands;
  /** Levels above this list; the top level is 0. */
  depth?: number;
};

type BranchProps = Omit<TaskTreeProps, 'nodes'> & { node: TaskNode; depth: number };

function Branch(props: BranchProps) {
  const { node, collapsed, currentTaskId, depth } = props;
  const { row, children } = node;
  const isParent = children.length > 0;
  const expanded = !collapsed.has(row.ID);
  return (
    <li className="task-tree-item">
      <TaskRow
        row={row}
        rows={props.rows}
        now={props.now}
        current={currentTaskId === row.ID}
        depth={depth}
        expanded={isParent ? expanded : undefined}
        below={isParent ? branchCounts(node) : undefined}
        onToggle={props.onToggle}
        commands={props.commands}
      />
      {isParent && expanded && <TaskTree {...props} nodes={children} depth={depth + 1} />}
    </li>
  );
}

/** The tasks as an outline: one line each, a quiet guide hanging under every open parent. */
export function TaskTree({ nodes, depth = 0, ...rest }: TaskTreeProps) {
  return (
    <ul className="task-tree" data-depth={Math.min(depth, 2)}>
      {nodes.map((node) => (
        <Branch key={node.row.ID} {...rest} node={node} depth={depth} />
      ))}
    </ul>
  );
}
