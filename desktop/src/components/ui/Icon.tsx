import { PanelLeftIcon } from '@animateicons/react/lucide/panel-left-icon';
import { PlusIcon } from '@animateicons/react/lucide/plus-icon';
import { SearchIcon } from '@animateicons/react/lucide/search-icon';
import { CodeXmlIcon } from '@animateicons/react/lucide/code-xml-icon';
import { ActivityIcon } from '@animateicons/react/lucide/activity-icon';
import { LayoutGridIcon } from '@animateicons/react/lucide/layout-grid-icon';
import { SettingsIcon } from '@animateicons/react/lucide/settings-icon';
import { ArrowRightIcon } from '@animateicons/react/lucide/arrow-right-icon';
import { ChevronDownIcon } from '@animateicons/react/lucide/chevron-down-icon';
import { CheckIcon } from '@animateicons/react/lucide/check-icon';
import { useTheme } from '../../design/ThemeProvider';
import design from '../../design/tokens.json';
export const iconNames = ['sidebar','plus','search','code','activity','grid','settings','arrow','chevron','check'] as const;
export type IconName = typeof iconNames[number];
export type IconSize = 'xs' | 'sm' | 'md' | 'lg';
const icons = { sidebar: PanelLeftIcon, plus: PlusIcon, search: SearchIcon, code: CodeXmlIcon, activity: ActivityIcon, grid: LayoutGridIcon, settings: SettingsIcon, arrow: ArrowRightIcon, chevron: ChevronDownIcon, check: CheckIcon };
export function Icon({ name, size = 'md', motion = 'none' }: { name: IconName; size?: IconSize; motion?: 'none' | 'directional' | 'disclosure' }) {
 const { reducedMotion } = useTheme();
 // Upstream glyph choreography is disabled. Only approved, state-meaningful motion is allowed.
 const approved = design.icons.motionByName[name];
 const resolvedMotion = motion === approved && !(reducedMotion && motion === 'directional') ? motion : 'none';
 const Glyph = icons[name];
 return <span className={`app-icon app-icon-${size}`} aria-hidden="true" data-icon={name} data-motion={resolvedMotion}>
  <Glyph isAnimated={false} />
 </span>;
}
