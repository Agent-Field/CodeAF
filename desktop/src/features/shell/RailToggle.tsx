import type { Ref } from 'react';
import { IconButton } from '../../components/ui';
import { shellShortcuts } from '../../design/keyboard';
import './rail.css';

type RailToggleProps = {
  /** 'rail' sits at the right of the rail's top row (26px); 'strip' leads the tab strip once the rail is collapsed (30px, then a hairline). */
  placement: 'rail' | 'strip';
  collapsed: boolean;
  onClick: () => void;
  ref?: Ref<HTMLButtonElement>;
};

/** The one rail toggle (design shell-helpers RAIL and 2d): ink-3 panel-left glyph, no fill until hover. */
export function RailToggle({ placement, collapsed, onClick, ref }: RailToggleProps) {
  const label = collapsed ? 'Show sidebar' : 'Hide sidebar';
  return <>
    <IconButton ref={ref} className="rail-toggle" data-placement={placement} label={label} icon="sidebar" title={`${label} (${shellShortcuts.rail})`} onClick={onClick}/>
    {placement === 'strip' && <span className="rail-toggle-divider" role="separator" aria-orientation="vertical"/>}
  </>;
}
