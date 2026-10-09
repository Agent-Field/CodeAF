import { useState } from 'react';
import { Text } from '../../components/ui';
import type { PaneRenderProps } from '../tabs/kinds/slots';
import { FileSurface } from './FileSurface';
import { defaultView, type FileTabKind } from './fileTarget';
import { useFileDiff, useFileSession, useFileText, useHandoff } from './useWorkView';
import './files.css';

/**
 * The pane of a file or diff tab, bound to the engine. The tab remembers a path and which view it was on;
 * every byte is read from the engine through the tab's saved session (it may be on another machine).
 */
export function FilePane({ pane, actions, focused }: PaneRenderProps) {
  const session = useFileSession(pane.sessionFile);
  const target = pane.file;
  if (!target || !pane.sessionFile) return <div className="file-surface"><Text className="file-message">This file cannot be read: its conversation is not attached.</Text></div>;
  if (session.status === 'failed') return <div className="file-surface"><Text className="file-message">{session.message}</Text></div>;
  return <LiveFile key={pane.id} pane={pane} actions={actions} focused={focused} path={target.path} session={session.status === 'ready' ? session.value : undefined}/>;
}

function LiveFile({ pane, actions, focused, path, session }: { focused: boolean; pane: PaneRenderProps['pane']; actions: PaneRenderProps['actions']; path: string; session?: { id: string; workspace: string } }) {
  const [wantText, setWantText] = useState(false);
  const diff = useFileDiff(session?.id, path);
  const text = useFileText(session?.id, path, wantText);
  const handoff = useHandoff(session?.id, path);
  const view = pane.file?.view ?? defaultView(pane.kind as FileTabKind);
  return <FileSurface path={path} workspace={session?.workspace ?? ''} view={view} diff={session ? diff : undefined} text={wantText ? text : undefined} onNeedText={() => setWantText(true)} handoff={handoff} keys={focused} onView={next => actions.onView({ file: { path, view: next } })}/>;
}
