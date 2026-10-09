import { useEffect, useMemo, type ReactNode } from 'react';
import { Text } from '../../components/ui';
import { formatBytes } from '../conversation/assets/paths';
import type { EngineFileDiff, EngineTextFile } from '../chat/engine-client';
import { DiffBody } from './DiffBody';
import { FileHeader, type FileView } from './FileHeader';
import { FileLines, linesOf } from './FileLines';
import { splitFilePath } from './fileTarget';
import type { Handoff, Load } from './useWorkView';
import './files.css';

/** The sentence the designer set for a diff whose start commit is gone from history (Shell Q24). */
export const baseGoneNote = 'Compared with the latest commit. The commit this conversation started on is no longer in history.';

export type FileSurfaceProps = {
  path: string;
  workspace: string;
  /** Which view the tab asks for. A file outside git always shows the file. */
  view: FileView;
  onView: (view: FileView) => void;
  /** Changes against the base. Absent only while the session is still attaching. */
  diff?: Load<EngineFileDiff>;
  /** The whole file. Read only once a view needs it; `onNeedText` asks for it. */
  text?: Load<EngineTextFile>;
  onNeedText: () => void;
  handoff: Handoff;
  /** The pane has the focus: it answers the copy-path key. The specimens leave it off. */
  keys?: boolean;
};

function Message({ children }: { children: ReactNode }) {
  return <Text className="file-message">{children}</Text>;
}

const plain = (message: string) => message.replace(/^engine:\s*/, '');

/** A file the engine will not show: one muted line saying why. The header's Open in menu is the way forward. */
function refusalLine(file: EngineTextFile): string {
  return file.refusal === 'binary' ? 'Binary file' : `Too large to show · ${formatBytes(file.size)}`;
}

function ChangesView({ diff, onNeedText, text }: { diff: EngineFileDiff; onNeedText: () => void; text?: Load<EngineTextFile> }) {
  const lines = useMemo(() => (text?.status === 'ready' ? linesOf(text.value.text) : undefined), [text]);
  if (diff.binary) return <Message>Binary file</Message>;
  if (!diff.hunks.length) return <Message>{diff.status === 'clean' ? 'No changes.' : 'Nothing to show for this change.'}</Message>;
  return <>
    <DiffBody diff={diff} text={lines} onNeedText={onNeedText}/>
    {diff.truncated && <Text className="file-note">The diff is cut at 5000 lines.</Text>}
  </>;
}

function FileView({ text, deleted }: { text?: Load<EngineTextFile>; deleted: boolean }) {
  const lines = useMemo(() => (text?.status === 'ready' ? linesOf(text.value.text) : []), [text]);
  if (!text || text.status === 'loading') return <Message>Loading…</Message>;
  if (text.status === 'failed') return <Message>{deleted ? 'This file was deleted.' : plain(text.message)}</Message>;
  if (text.value.refusal) return <Message>{refusalLine(text.value)}</Message>;
  return <FileLines lines={lines}/>;
}

/**
 * The file or diff tab: changes first, a toggle to the whole file, a handoff to the editor (Shell 3e).
 * Presentational: the live pane feeds it engine reads, the Design system page feeds it fixtures.
 */
export function FileSurface({ path, workspace, view, onView, diff, text, onNeedText, handoff, keys }: FileSurfaceProps) {
  const { name, dir } = splitFilePath(path);
  const change = diff?.status === 'ready' ? diff.value : undefined;
  const outsideGit = !!change && !change.git;
  const git = !!change?.git;
  const folder = outsideGit ? (dir ? `${dir} · not in git` : 'not in git') : dir;
  const shown: FileView = change && !git ? 'file' : view;
  const refused = shown === 'file' && text?.status === 'ready' && !!text.value.refusal;
  useEffect(() => { if (shown === 'file') onNeedText(); }, [shown]);
  const counts = git ? change : undefined;
  return <div className="file-surface">
    <FileHeader name={name} dir={folder} added={counts?.added} deleted={counts?.deleted} view={git ? shown : null} onView={onView} path={path} workspace={workspace} handoff={handoff} refused={refused} keys={keys}/>
    {shown === 'changes' && change?.base?.kind === 'head' && <Text className="file-base">{baseGoneNote}</Text>}
    <div className="file-body" role="region" tabIndex={0} aria-label={shown === 'changes' ? `Changes to ${name}` : `Contents of ${name}`}>
      {shown === 'changes' && (!diff || diff.status === 'loading') && <Message>Loading…</Message>}
      {shown === 'changes' && diff?.status === 'failed' && <Message>{plain(diff.message)}</Message>}
      {shown === 'changes' && change && <ChangesView diff={change} text={text} onNeedText={onNeedText}/>}
      {shown === 'file' && <FileView text={text} deleted={change?.status === 'deleted'}/>}
    </div>
  </div>;
}
