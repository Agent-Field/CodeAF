// The xterm theme, built at runtime from the interface's own CSS tokens (no literal colours here).
// Each token is read through getComputedStyle, resolved to sRGB on a one-pixel canvas (browsers differ
// in how they print an oklch value), then handed to the pure palette maths in ansi.ts.
import type { ITheme } from '@xterm/xterm';
import { ansiPalette, extendedPalette, hex, type Rgb } from './ansi';

let canvas: CanvasRenderingContext2D | null = null;
function resolve(css: string): Rgb {
  canvas ??= document.createElement('canvas').getContext('2d', { willReadFrequently: true });
  if (!canvas) return [0, 0, 0];
  canvas.canvas.width = canvas.canvas.height = 1;
  canvas.clearRect(0, 0, 1, 1);
  canvas.fillStyle = css;
  canvas.fillRect(0, 0, 1, 1);
  const [r, g, b] = canvas.getImageData(0, 0, 1, 1).data;
  return [r, g, b];
}

/** The token's colour as it paints inside `scope` (a token may differ by theme and tint). */
export function tokenColor(scope: HTMLElement, token: string): Rgb {
  const probe = document.createElement('span');
  probe.style.color = `var(--${token})`;
  scope.append(probe);
  const color = resolve(getComputedStyle(probe).color);
  probe.remove();
  return color;
}

/** Translucent selection: the accent at a fifth of its strength, on whatever the field is. */
const selectionAlpha = '33';

export function readTerminalTheme(scope: HTMLElement): ITheme {
  const color = (token: string) => tokenColor(scope, token);
  const palette = ansiPalette({ field: color('term'), ink: color('ink'), ink2: color('ink-2'), ink3: color('ink-3'), accent: color('accent'), danger: color('danger'), success: color('success'), amber: color('amber') }).map(hex);
  const [black, red, green, yellow, blue, magenta, cyan, white, brightBlack, brightRed, brightGreen, brightYellow, brightBlue, brightMagenta, brightCyan, brightWhite] = palette;
  const field = hex(color('term'));
  return {
    background: field, foreground: hex(color('ink-2')), cursor: hex(color('ink-2')), cursorAccent: field,
    selectionBackground: `${hex(color('accent'))}${selectionAlpha}`, selectionInactiveBackground: `${hex(color('accent'))}${selectionAlpha}`,
    black, red, green, yellow, blue, magenta, cyan, white, brightBlack, brightRed, brightGreen, brightYellow, brightBlue, brightMagenta, brightCyan, brightWhite,
    extendedAnsi: extendedPalette().map(hex),
  };
}

/** A token's length or number as the stylesheet defines it (for the options xterm takes as numbers). */
export function tokenNumber(scope: HTMLElement, token: string): number {
  return Number.parseFloat(getComputedStyle(scope).getPropertyValue(`--${token}`));
}
export const tokenFont = (scope: HTMLElement, token: string) => getComputedStyle(scope).getPropertyValue(`--${token}`).trim();

/** Calls `onChange` when the appearance (theme, tint) changes; returns the stop function. */
export function watchAppearance(onChange: () => void): () => void {
  const observer = new MutationObserver(onChange);
  observer.observe(document.documentElement, { attributes: true });
  return () => observer.disconnect();
}

/**
 * xterm's `lineHeight` multiplies the font's own line height, while the design states leading as a multiple
 * of the font size (12.5px at 1.7). This converts one to the other by measuring the font as the browser sets it.
 */
export function lineHeightFor(scope: HTMLElement, fontFamily: string, fontSize: number, leading: number): number {
  const probe = document.createElement('span');
  probe.textContent = 'W';
  Object.assign(probe.style, { position: 'absolute', visibility: 'hidden', lineHeight: 'normal', fontFamily, fontSize: `${fontSize}px` });
  scope.append(probe);
  const natural = probe.getBoundingClientRect().height;
  probe.remove();
  return natural > 0 ? (fontSize * leading) / natural : leading;
}
