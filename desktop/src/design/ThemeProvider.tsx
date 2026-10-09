import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { isTauri } from '@tauri-apps/api/core';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { useMediaQuery } from './useMediaQuery';
import design from './tokens.json';
export type Theme = 'system' | 'light' | 'dark';
interface ThemeContextValue { theme: Theme; resolvedTheme: 'light' | 'dark'; setTheme: (theme: Theme) => void; reducedMotion: boolean }
const ThemeContext = createContext<ThemeContextValue | null>(null);
const storageKey = 'codeaf-theme';
function storedTheme(): Theme {
 try { const t = localStorage.getItem(storageKey); return t === 'light' || t === 'dark' ? t : 'system'; } catch { return 'system'; }
}
export function ThemeProvider({ children }: { children: ReactNode }) {
 const [theme, setTheme] = useState<Theme>(storedTheme);
 const systemDark = useMediaQuery('(prefers-color-scheme: dark)');
 const reducedMotion = useMediaQuery('(prefers-reduced-motion: reduce)');
 const resolvedTheme = theme === 'system' ? systemDark ? 'dark' : 'light' : theme;
 useEffect(() => {
  document.documentElement.dataset.theme = theme;
  document.documentElement.dataset.resolvedTheme = resolvedTheme;
  document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]').forEach(meta => { meta.content = design.themes[resolvedTheme]['chrome-canvas']; });
  try { localStorage.setItem(storageKey, theme); } catch { /* Appearance remains usable without storage. */ }
  if (isTauri()) void getCurrentWindow().setTheme(theme === 'system' ? null : theme).catch(console.error);
 }, [theme, resolvedTheme]);
 useEffect(() => {
  const sync = (event: StorageEvent) => { if (event.key === storageKey || event.key === null) setTheme(storedTheme()); };
  window.addEventListener('storage', sync);
  return () => window.removeEventListener('storage', sync);
 }, []);
 return <ThemeContext.Provider value={{theme, resolvedTheme, setTheme, reducedMotion}}>{children}</ThemeContext.Provider>;
}
export function useTheme() {
 const context = useContext(ThemeContext);
 if (!context) throw new Error('Design components require ThemeProvider');
 return context;
}
