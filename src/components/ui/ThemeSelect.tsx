import { useTheme, type Theme } from '../../design/ThemeProvider';
import { Icon } from './Icon';
export function ThemeSelect() {
 const { theme, setTheme } = useTheme();
 return <label className="theme-control"><Icon name="settings" size="sm" animated={false}/><select className="theme-select" aria-label="Theme" value={theme} onChange={e => setTheme(e.target.value as Theme)}><option value="system">System appearance</option><option value="light">Light appearance</option><option value="dark">Dark appearance</option></select></label>;
}
