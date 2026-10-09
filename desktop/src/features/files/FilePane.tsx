import { useEffect, useRef, useState, type RefObject } from 'react';
import { Text } from '../../components/ui';
import type { PaneRenderProps } from '../tabs/kinds/slots';
import { FileSurface } from './FileSurface';
import { defaultView, type FileTabKind } from './fileTarget';
import { useFileDiff, useFileRefresh, useFileSession, useFileText, useHandoff } from './useWorkView';
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

function useShown(host: RefObject<HTMLDivElement | null>): boolean {
  const [shown, setShown] = useState(true);
  useEffect(() => {
    const section = host.current?.closest('section');
    const read = () => setShown(!document.hidden && !(section instanceof HTMLElement && section.hidden));
    read();
    document.addEventListener('visibilitychange', read);
    const observer = section ? new MutationObserver(read) : undefined;
    if (section) observer?.observe(section, { attributes: true, attributeFilter: ['hidden'] });
    return () => { document.removeEventListener('visibilitychange', read); observer?.disconnect(); };
  }, [host]);
  return shown;
}

function LiveFile({ pane, actions, focused, path, session }: { focused: boolean; pane: PaneRenderProps['pane']; actions: PaneRenderProps['actions']; path: string; session?: { id: string; workspace: string } }) {
  const host = useRef<HTMLDivElement>(null);
  const shown = useShown(host);
  const [wantText, setWantText] = useState(false);
  const refresh = useFileRefresh(session?.id, path, shown);
  const diff = useFileDiff(session?.id, path, refresh);
  const text = useFileText(session?.id, path, wantText, refresh);
  const handoff = useHandoff(session?.id, path);
  const view = pane.file?.view ?? defaultView(pane.kind as FileTabKind);
  return <div ref={host} className="file-live"><FileSurface path={path} workspace={session?.workspace ?? ''} view={view} diff={session ? diff : undefined} text={wantText ? text : undefined} onNeedText={() => setWantText(true)} handoff={handoff} keys={focused} onView={next => actions.onView({ file: { path, view: next } })}/></div>;
}
