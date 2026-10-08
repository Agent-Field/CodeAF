import { useTheme, type Theme } from '../../design/ThemeProvider';
import { Icon } from './Icon';
import { Select } from './Select';
const options = [
 { value: 'system', label: 'System appearance' },
 { value: 'light', label: 'Light appearance' },
 { value: 'dark', label: 'Dark appearance' },
];
export function ThemeSelect() {
 const { theme, setTheme } = useTheme();
 return <div className="theme-control"><Icon name="settings" size="sm"/><Select label="Theme" className="theme-select" value={theme} onValueChange={value => setTheme(value as Theme)} options={options}/></div>;
}
