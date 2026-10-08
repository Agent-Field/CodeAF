import { useEffect, useRef } from 'react';
import type { IconHandle } from '@animateicons/react';
import { PanelLeftIcon } from '@animateicons/react/lucide/panel-left-icon';
import { PlusIcon } from '@animateicons/react/lucide/plus-icon';
import { SearchIcon } from '@animateicons/react/lucide/search-icon';
import { CodeXmlIcon } from '@animateicons/react/lucide/code-xml-icon';
import { ActivityIcon } from '@animateicons/react/lucide/activity-icon';
import { LayoutGridIcon } from '@animateicons/react/lucide/layout-grid-icon';
import { SettingsIcon } from '@animateicons/react/lucide/settings-icon';
import { ArrowRightIcon } from '@animateicons/react/lucide/arrow-right-icon';
import { useTheme } from '../../design/ThemeProvider';
import design from '../../design/tokens.json';
export const iconNames = ['sidebar','plus','search','code','activity','grid','settings','arrow'] as const;
export type IconName = typeof iconNames[number];
export type IconSize = 'xs' | 'sm' | 'md' | 'lg';
const icons = { sidebar: PanelLeftIcon, plus: PlusIcon, search: SearchIcon, code: CodeXmlIcon, activity: ActivityIcon, grid: LayoutGridIcon, settings: SettingsIcon, arrow: ArrowRightIcon };
export function Icon({ name, size = 'md', animated = true }: { name: IconName; size?: IconSize; animated?: boolean }) {
 const { reducedMotion } = useTheme();
 const wrapper = useRef<HTMLSpanElement>(null);
 const handle = useRef<IconHandle>(null);
 useEffect(() => {
  const control = wrapper.current?.closest('button, a, [role="button"]');
  if (!control || !animated || reducedMotion) { handle.current?.stopAnimation(); return; }
  const start = () => { if (!control.hasAttribute('disabled')) handle.current?.startAnimation(); };
  const stop = () => handle.current?.stopAnimation();
  control.addEventListener('mouseenter', start); control.addEventListener('mouseleave', stop);
  control.addEventListener('focus', start); control.addEventListener('blur', stop);
  return () => { control.removeEventListener('mouseenter', start); control.removeEventListener('mouseleave', stop); control.removeEventListener('focus', start); control.removeEventListener('blur', stop); stop(); };
 }, [animated, reducedMotion, name]);
 const Glyph = icons[name];
 return <span ref={wrapper} className={`app-icon app-icon-${size}`} aria-hidden="true" data-icon={name} data-animated={animated && !reducedMotion}>
  <Glyph ref={handle} isAnimated={false} duration={design.icons.durationSeconds} />
 </span>;
}
