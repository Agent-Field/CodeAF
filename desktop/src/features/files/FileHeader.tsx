import { Icon, Segmented } from '../../components/ui';
import { EditorHandoff } from './EditorHandoff';
import { FadeText } from './FadeText';
import { fileTypeIcon } from './fileTarget';
import type { Handoff } from './useWorkView';

export type FileView = 'changes' | 'file';
const views = [{ value: 'changes' as const, label: 'Changes' }, { value: 'file' as const, label: 'File' }];

type Props = {
  name: string;
  dir: string;
  /** Counts of changed lines; both absent for a file with no changes or outside git. */
  added?: number;
  deleted?: number;
  /** Null hides the toggle: a file outside git has no changes to show. */
  view: FileView | null;
  onView: (view: FileView) => void;
  path: string;
  workspace: string;
  handoff: Handoff;
  /** The file cannot be shown, so the handoff is a menu. */
  refused?: boolean;
  /** This pane has the focus, so it answers the copy-path key. */
  keys?: boolean;
};

/** "lexer.go  internal/parse  +12 −3", the Changes / File toggle, and the handoff to the editor. */
export function FileHeader({ name, dir, added, deleted, view, onView, path, workspace, handoff, refused, keys }: Props) {
  return <header className="file-head">
    <div className="file-id">
      <Icon name={fileTypeIcon(name)} size="sm"/>
      <FadeText className="file-name">{name}</FadeText>
      {dir && <FadeText className="file-dir">{dir}</FadeText>}
      {!!added && <span className="file-count" data-sign="add">+{added}</span>}
      {!!deleted && <span className="file-count" data-sign="del">−{deleted}</span>}
    </div>
    <div className="file-actions">
      {view && <Segmented label="View" options={views} value={view} onChange={onView}/>}
      <EditorHandoff path={path} workspace={workspace} handoff={handoff} menu={refused} keys={keys}/>
    </div>
  </header>;
}
