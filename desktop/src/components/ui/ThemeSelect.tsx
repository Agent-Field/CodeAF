import { useTheme, type Theme } from '../../design/ThemeProvider';
import { Select } from './Select';
const options = [
 { value: 'system', label: 'System appearance' },
 { value: 'light', label: 'Light appearance' },
 { value: 'dark', label: 'Dark appearance' },
];
export function ThemeSelect() {
 const { theme, setTheme } = useTheme();
 return <Select label="Theme" icon="settings" value={theme} onValueChange={value => setTheme(value as Theme)} options={options}/>;
}
