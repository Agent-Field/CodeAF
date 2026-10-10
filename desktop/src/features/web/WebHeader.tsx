import { DropdownMenu, IconButton, type MenuEntry } from '../../components/ui';
import type { WebFindResult, WebHistoryStep } from '../../design/nativeWeb';
import { FindBar } from './FindBar';
import { AddressField } from './AddressField';

type Props = {
  find?: { search: (query: string, forward: boolean) => Promise<WebFindResult>; close: () => void };
  url?: string;
  favicon?: string;
  status?: string;
  loading: boolean;
  canBack: boolean;
  canForward: boolean;
  canReload: boolean;
  initialEditing?: boolean;
  onGo: (url: string) => void;
  onStep: (step: WebHistoryStep) => void;
  onChat?: () => void;
  onExternal: () => void;
};

// The real pane and the specimen share the row so every state keeps the same geometry.
export function WebHeader({ find, url, favicon, status, loading, canBack, canForward, canReload, initialEditing, onGo, onStep, onChat, onExternal }: Props) {
  const items: MenuEntry[] = [
    ...(onChat ? [{ id: 'chat', label: 'Start a conversation with this page', icon: 'chatPlus' as const, onSelect: onChat }] : []),
    { id: 'external', label: 'Open in browser', icon: 'external', disabled: !url, onSelect: onExternal },
  ];
  return <div className="web-header" role="toolbar" aria-label="Page">
    <IconButton icon="back" iconSize="sm" label="Back" disabled={!canBack} onClick={() => onStep('back')}/>
    <IconButton className="web-muted-action" icon="forward" iconSize="sm" label="Forward" disabled={!canForward} onClick={() => onStep('forward')}/>
    {loading
      ? <IconButton icon="close" iconSize="sm" label="Stop loading" onClick={() => onStep('stop')}/>
      : <IconButton icon="reload" iconSize="sm" label="Reload" disabled={!canReload} onClick={() => onStep('reload')}/>}
    {find ? <FindBar onFind={find.search} onClose={find.close}/> : <AddressField url={url} favicon={favicon} status={status} onGo={onGo} initialEditing={initialEditing}/>}
    {onChat && <IconButton className="web-wide-action" icon="chatPlus" iconSize="sm" label="Start a conversation with this page" onClick={onChat}/>}
    <IconButton className="web-wide-action web-muted-action" icon="external" iconSize="sm" label="Open in browser" disabled={!url} onClick={onExternal}/>
    <DropdownMenu label="Page actions" items={items}><IconButton className="web-more-action web-muted-action" icon="more" iconSize="sm" label="Page actions"/></DropdownMenu>
  </div>;
}
