import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { Text } from '../../components/ui';
import { formatBytes } from '../conversation/assets/paths';
import type { EngineFile, EngineFileDiff, EngineTextFile } from '../chat/engine-client';
import { DiffBody } from './DiffBody';
import { FileHeader, type FileView } from './FileHeader';
import { FileLines, linesOf } from './FileLines';
import { splitFilePath } from './fileTarget';
import { imageVerdict, isRasterImagePath } from './imageFile';
import type { Handoff, Load } from './useWorkView';
import './files.css';

/** The sentence the designer set for a diff whose start commit is gone from history (Shell Q24). */
export const baseGoneNote = 'Compared with the latest commit. The commit this conversation started on is no longer in history.';
/** A diff measured from the latest commit because no start was ever recorded. The design draws only the gone case;
 * this is its first sentence alone, so the counts are not read as "since this conversation began" (FILES-FOLLOWUP-QUESTIONS FF1). */
export const baseHeadNote = 'Compared with the latest commit.';

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
  /** The file as a picture, for a raster image path only. Read only once the File view asks via `onNeedImage`. */
  image?: Load<EngineFile>;
  onNeedImage?: () => void;
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

const imageRefusal = (file: EngineFile): string | null => {
  const verdict = imageVerdict(file);
  if (verdict.ok) return null;
  return verdict.reason === 'too-large' ? `Too large to show · ${formatBytes(file.size)}` : 'Binary file';
};

/** One raster picture, fitted inside the body with its aspect ratio kept. A picture that will not decode says so in one line. */
function Picture({ url, name }: { url: string; name: string }) {
  const [broken, setBroken] = useState<string | null>(null);
  if (broken === url) return <Message>This image cannot be shown.</Message>;
  return <img className="file-picture" src={url} alt={name} onError={() => setBroken(url)}/>;
}

function ImageView({ image, name, deleted }: { image?: Load<EngineFile>; name: string; deleted: boolean }) {
  if (!image || image.status === 'loading') return <Message>Loading…</Message>;
  if (image.status === 'failed') return <Message>{deleted ? 'This file was deleted.' : plain(image.message)}</Message>;
  const refusal = imageRefusal(image.value);
  const verdict = imageVerdict(image.value);
  if (refusal || !verdict.ok) return <Message>{refusal}</Message>;
  return <Picture url={verdict.url} name={name}/>;
}

/**
 * The file or diff tab: changes first, a toggle to the whole file, a handoff to the editor (Shell 3e).
 * Presentational: the live pane feeds it engine reads, the Design system page feeds it fixtures.
 */
export function FileSurface({ path, workspace, view, onView, diff, text, onNeedText, image, onNeedImage, handoff, keys }: FileSurfaceProps) {
  const { name, dir } = splitFilePath(path);
  const change = diff?.status === 'ready' ? diff.value : undefined;
  const outsideGit = !!change && !change.git;
  const git = !!change?.git;
  const folder = outsideGit ? (dir ? `${dir} · not in git` : 'not in git') : dir;
  const shown: FileView = change && !git ? 'file' : view;
  const picture = isRasterImagePath(path) && !!onNeedImage;
  const refused = shown === 'file' && (picture
    ? image?.status === 'ready' && !!imageRefusal(image.value)
    : text?.status === 'ready' && !!text.value.refusal);
  useEffect(() => { if (shown === 'file') (picture ? onNeedImage : onNeedText)?.(); }, [shown, picture, path]);
  const showing = shown === 'file' && picture && image?.status === 'ready' && !imageRefusal(image.value);
  const counts = git ? change : undefined;
  return <div className="file-surface">
    <FileHeader name={name} dir={folder} added={counts?.added} deleted={counts?.deleted} view={git ? shown : null} onView={onView} path={path} workspace={workspace} handoff={handoff} refused={refused} keys={keys}/>
    {shown === 'changes' && change?.base?.kind === 'head' && <Text className="file-base">{change.base.startGone ? baseGoneNote : baseHeadNote}</Text>}
    <div className="file-body" data-scroll-key="file-body" data-picture={showing || undefined} role="region" tabIndex={0} aria-label={shown === 'changes' ? `Changes to ${name}` : `Contents of ${name}`}>
      {shown === 'changes' && (!diff || diff.status === 'loading') && <Message>Loading…</Message>}
      {shown === 'changes' && diff?.status === 'failed' && <Message>{plain(diff.message)}</Message>}
      {shown === 'changes' && change && <ChangesView diff={change} text={text} onNeedText={onNeedText}/>}
      {shown === 'file' && !picture && <FileView text={text} deleted={change?.status === 'deleted'}/>}
      {shown === 'file' && picture && <ImageView image={image} name={name} deleted={change?.status === 'deleted'}/>}
    </div>
  </div>;
}
