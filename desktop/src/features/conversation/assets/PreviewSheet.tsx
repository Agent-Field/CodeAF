import { Button, CopyButton, IconButton, Text } from '../../../components/ui';
import { useAssets, useFile, type FileState } from './AssetContext';
import { revealLabel } from './FileActions';
import { Overlay } from './Overlay';
import { absolutePath, formatBytes, previewKind, splitPath } from './paths';

const textByteCap = 1024 * 1024;
const lineCap = 5000;

function Unavailable({ reason, onOpen }: { reason: string; onOpen: () => void }) {
  return (
    <div className="sheet-note">
      <Text>{reason}</Text>
      <Button variant="quiet" onClick={onOpen}>Open</Button>
    </div>
  );
}

function decode(base64: string): string {
  const bytes = Uint8Array.from(atob(base64), char => char.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

function TextBody({ file }: { file: NonNullable<FileState['file']> }) {
  const lines = decode(file.dataBase64).replace(/\n$/, '').split('\n');
  const shown = lines.slice(0, lineCap);
  return (
    <>
      <ol className="sheet-lines" aria-label="File contents">
        {shown.map((line, index) => <li key={index}>{line || ' '}</li>)}
      </ol>
      {lines.length > lineCap && <Text className="sheet-cap">Showing the first {lineCap} of {lines.length} lines.</Text>}
    </>
  );
}

function Body({ path, state, onOpen }: { path: string; state: FileState; onOpen: () => void }) {
  const kind = previewKind(path);
  const { file } = state;
  if (state.status === 'loading') return <Text className="sheet-note">Loading…</Text>;
  if (state.status === 'error' || !file) return <Unavailable reason="This file could not be read." onOpen={onOpen} />;
  if (kind === 'image' && state.url) return <img className="sheet-image" src={state.url} alt={splitPath(path).name} />;
  if (kind === 'text' && file.size <= textByteCap) return <TextBody file={file} />;
  if (kind === 'text') return <Unavailable reason={`Too large to preview (${formatBytes(file.size)}).`} onOpen={onOpen} />;
  // The app CSP allows no embedded documents, so a PDF opens in the system viewer instead.
  return <Unavailable reason="Preview not available." onOpen={onOpen} />;
}

/** Right-hand overlay sheet. Reads the file only when it is open. */
export function PreviewSheet({ path, onClose }: { path: string; onClose: () => void }) {
  const assets = useAssets();
  const state = useFile(path);
  const { name, dir } = splitPath(path);
  const full = absolutePath(path, assets.workspace);
  const open = () => void assets.openPath(full).catch(() => undefined);
  const reveal = () => void assets.revealPath(full).catch(() => undefined);
  return (
    <Overlay label={`Preview of ${name}`} variant="sheet" onClose={onClose}>
      <div className="sheet">
        <header className="sheet-head">
          <div className="sheet-title">
            <span className="sheet-name">{name}</span>
            {dir && <span className="sheet-dir">{dir}</span>}
          </div>
          <span className="sheet-actions">
            <IconButton label="Open in editor" icon="external" iconSize="sm" onClick={open} />
            <IconButton label={revealLabel} icon="folderOpen" iconSize="sm" onClick={reveal} />
            <CopyButton text={full} label="Copy path" />
            <IconButton label="Close preview" icon="close" iconSize="sm" onClick={onClose} />
          </span>
        </header>
        <div className="sheet-body">
          <Body path={path} state={state} onOpen={open} />
        </div>
      </div>
    </Overlay>
  );
}
