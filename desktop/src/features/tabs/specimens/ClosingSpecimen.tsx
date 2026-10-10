import { Icon, KeyboardShortcut, ToastView, type IconName } from '../../../components/ui';
import { closeShortcut, closeStopShortcut, newGroupShortcut } from '../closing/shortcuts';
import { Tab } from '../Tab';
import './closing-specimen.css';

type Row = { id: string; label: string; icon?: IconName; shortcut?: string; submenu?: boolean; highlighted?: boolean; danger?: boolean } | 'separator';

/** A menu drawn with the shared menu classes, open and at rest, so the Design system page can show it without a trigger. */
function MenuSurface({ rows, wide = false, label }: { rows: Row[]; wide?: boolean; label: string }) {
  return (
    <div className={`app-menu closing-specimen-menu${wide ? ' app-menu-wide' : ''}`} role="presentation" aria-label={label}>
      {rows.map((row, index) => row === 'separator'
        ? <div key={index} className="menu-separator"/>
        : <div key={row.id} className={`menu-item${row.danger ? ' menu-item-danger' : ''}`} data-highlighted={row.highlighted || undefined}>
          {row.icon && <Icon name={row.icon} size="sm"/>}<span className="menu-label">{row.label}</span>
          {row.submenu ? <Icon name="chevronRight" size="xs"/> : row.shortcut && <KeyboardShortcut label={row.shortcut} variant="inline"/>}
        </div>)}
    </div>
  );
}

const tabMenu: Row[] = [
  { id: 'split', label: 'Open in split', icon: 'split', submenu: true },
  { id: 'group', label: 'Add to group', icon: 'layers', submenu: true, highlighted: true },
  { id: 'pin', label: 'Pin tab', icon: 'pin' },
  { id: 'duplicate', label: 'Duplicate', icon: 'copy' },
  'separator',
  { id: 'window', label: 'Move to new window', icon: 'appWindow' },
  'separator',
  { id: 'close', label: 'Close tab', shortcut: closeShortcut },
  { id: 'close-others', label: 'Close other tabs' },
  { id: 'close-right', label: 'Close tabs to the right' },
];
const groupSubmenu: Row[] = [
  { id: 'g1', label: 'Trailing commas', icon: 'layers' },
  { id: 'g2', label: 'Release v2.4', icon: 'layers' },
  'separator',
  { id: 'new', label: 'New group…', icon: 'plus', shortcut: newGroupShortcut },
];
const groupMenu: Row[] = [
  { id: 'rename', label: 'Rename', icon: 'pencil' },
  { id: 'split', label: 'Open as split', icon: 'grid' },
  { id: 'collapse', label: 'Collapse', icon: 'shrink' },
  { id: 'ungroup', label: 'Ungroup', icon: 'layers' },
  'separator',
  { id: 'close', label: 'Close 4 tabs', danger: true },
];
const explicitMenu: Row[] = [
  { id: 'close', label: 'Close tab', shortcut: closeShortcut },
  { id: 'stop', label: 'Close and stop', shortcut: closeStopShortcut, highlighted: true },
];

const noop = () => {};
const Tip = ({ text, shortcut }: { text: string; shortcut: string }) => <span className="tooltip closing-specimen-tip">{text}<span className="tooltip-shortcut">{shortcut}</span></span>;
const Note = ({ children }: { children: string }) => <span className="closing-specimen-note">{children}</span>;

/** Design 3g (menus) and 3l (closing running work) on the Design system page. Specimen only: no data here is live. */
export function ClosingSpecimen() {
  return <div className="closing-specimen" data-closing-specimen>
    <span className="tabs-specimen-label">Menus · tab menu with a submenu, group label menu, the explicit close path</span>
    <div className="closing-specimen-row">
      <div className="closing-specimen-cell"><MenuSurface wide label="Tab menu" rows={tabMenu}/></div>
      <div className="closing-specimen-cell"><MenuSurface label="Add to group submenu" rows={groupSubmenu}/></div>
      <div className="closing-specimen-cell"><MenuSurface label="Group label menu" rows={groupMenu}/></div>
      <div className="closing-specimen-cell"><MenuSurface label="Close choices" rows={explicitMenu}/></div>
    </div>
    <span className="tabs-specimen-label">Closing running work · hover, hold ⌥, after closing, idle tab</span>
    <div className="closing-specimen-row">
      <div className="closing-specimen-cell">
        <Note>1 · Hover a running tab</Note>
        <div className="tabs-specimen-wide"><Tab specimen kind="conversation" title="Config stack" hover tabIndex={-1} onClose={noop} closeHint="Close · keeps running" closeShortcut={closeShortcut}/></div>
        <Tip text="Close · keeps running" shortcut={closeShortcut}/>
      </div>
      <div className="closing-specimen-cell">
        <Note>2 · Hold ⌥ while hovering</Note>
        <div className="tabs-specimen-wide"><Tab specimen kind="conversation" title="Config stack" hover closeMode="stop" tabIndex={-1} onClose={noop} closeHint="Close and stop" closeShortcut={closeStopShortcut}/></div>
        <Tip text="Close and stop" shortcut={closeStopShortcut}/>
      </div>
      <div className="closing-specimen-cell">
        <Note>3 · After closing a running tab</Note>
        <ToastView toast={{ message: [{ strong: 'Config stack' }, ' closed and still running'], tone: 'info', actions: [{ label: 'Stop it', onSelect: () => {} }], undo: () => {} }}/>
      </div>
      <div className="closing-specimen-cell">
        <Note>3 · When stopping fails</Note>
        <ToastView toast={{ message: ['Could not stop ', { strong: 'Config stack' }, '. It is still running.'], tone: 'danger', actions: [{ label: 'Try again', onSelect: () => {} }], undo: () => {} }}/>
      </div>
      <div className="closing-specimen-cell">
        <Note>Idle tab</Note>
        <div className="tabs-specimen-wide"><Tab specimen kind="file" title="lexer.go" hover tabIndex={-1} onClose={noop} closeHint="Close" closeShortcut={closeShortcut}/></div>
        <Tip text="Close" shortcut={closeShortcut}/>
      </div>
    </div>
  </div>;
}
