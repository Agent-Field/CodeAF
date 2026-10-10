import type { ButtonHTMLAttributes, HTMLAttributes, ReactNode } from 'react';
import { IconButton, NavigationItem, type IconName } from '../../../components/ui';
import { PlaceDot } from '../PlaceDot';
import { PlaceSwatch, type TintName } from '../components/PlaceSwatch';
import './rail-places.css';

export type RailRowProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'> & {
  name: string;
  active: boolean;
  icon?: IconName;
  tint?: TintName;
  parentName?: string;
  status?: 'waiting' | 'failed';
  statusLabel?: string;
  meta?: ReactNode;
  closedButBusy?: boolean;
  close?: { onClose: () => void; tabs?: number; shortcut?: string };
  containerProps?: HTMLAttributes<HTMLDivElement> & { 'data-drop'?: boolean };
};

/** One row serves both icon-led navigation and tint-led places, so their states and keyboard focus cannot drift. */
export function RailRow({ name, active, icon, tint, parentName, status, statusLabel, meta, closedButBusy, close, containerProps, className = '', ...props }: RailRowProps) {
  const canClose = close && !closedButBusy;
  const tabs = close?.tabs;
  const closeTitle = `Close ${name}${tabs && tabs > 0 ? ` · ${tabs} ${tabs === 1 ? 'tab' : 'tabs'}` : ''}`;
  return <div {...containerProps} className={`rail-row ${containerProps?.className ?? ''}`} data-status={status} data-busy-closed={closedButBusy || undefined} data-closable={canClose ? true : undefined}>
    <NavigationItem {...props} className={`rail-row-main ${tint ? 'rail-place' : ''} ${className}`} active={active} icon={icon}
      lead={tint ? <PlaceSwatch tint={tint} role="rail"/> : undefined}
      trail={<span className="rail-row-trail">{meta !== undefined && meta !== null && meta !== '' && meta !== 0 && <span className="rail-meta">{meta}</span>}<PlaceDot status={status} label={statusLabel}/></span>}>
      <span className="rail-place-name">{name}{parentName && <span className="rail-place-path"> · {parentName}</span>}</span>
      {closedButBusy && <span className="rail-place-path rail-place-closed">closed · still running</span>}
    </NavigationItem>
    {canClose && <IconButton className="rail-row-close" icon="close" iconSize="micro" label={`Close ${name}`}
      title={closeTitle} shortcut={close.shortcut} onClick={event => { event.stopPropagation(); close.onClose(); }}/>}
  </div>;
}
