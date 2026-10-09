// The terminal's ANSI palette, derived from the interface's own colour tokens (Q3).
// Pure: no DOM, no React. Colours go in and out as sRGB triples (0-255); the maths runs in OKLCH
// so "60% chroma" means the same thing for every hue.

export type Rgb = readonly [number, number, number];
type Lch = readonly [number, number, number];

/** How much of each token's chroma the terminal keeps. The designer asks for about 60%. */
export const ansiChroma = 0.6;
/** How far a "bright" colour moves toward the ink colour. */
const brightMix = 0.22;
/** The hue distance from the accent to the magenta and cyan slots, which no token names. */
const hueStep = 60;

const toLinear = (c: number) => { const v = c / 255; return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
const fromLinear = (v: number) => Math.round(255 * Math.min(1, Math.max(0, v <= 0.0031308 ? v * 12.92 : 1.055 * v ** (1 / 2.4) - 0.055)));

function toLab([r, g, b]: Rgb): [number, number, number] {
  const [lr, lg, lb] = [toLinear(r), toLinear(g), toLinear(b)];
  const l = Math.cbrt(0.4122214708 * lr + 0.5363325363 * lg + 0.0514459929 * lb);
  const m = Math.cbrt(0.2119034982 * lr + 0.6806995451 * lg + 0.1073969566 * lb);
  const s = Math.cbrt(0.0883024619 * lr + 0.2817188376 * lg + 0.6299787005 * lb);
  return [0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s, 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s, 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s];
}
function fromLab([L, a, b]: readonly [number, number, number]): Rgb {
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  return [
    fromLinear(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s),
    fromLinear(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s),
    fromLinear(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s),
  ];
}
export function toLch(rgb: Rgb): Lch {
  const [L, a, b] = toLab(rgb);
  return [L, Math.hypot(a, b), (Math.atan2(b, a) * 180 / Math.PI + 360) % 360];
}
export function fromLch([L, C, H]: Lch): Rgb {
  const h = H * Math.PI / 180;
  return fromLab([L, C * Math.cos(h), C * Math.sin(h)]);
}

/** The same colour with `factor` of its chroma: lightness and hue stay. */
export function desaturate(rgb: Rgb, factor: number = ansiChroma): Rgb {
  const [L, C, H] = toLch(rgb);
  return fromLch([L, C * factor, H]);
}
/** `amount` (0-1) of the way from `from` to `to`, in OKLab, so mixes stay even in lightness. */
export function mix(from: Rgb, to: Rgb, amount: number): Rgb {
  const a = toLab(from); const b = toLab(to);
  return fromLab([a[0] + (b[0] - a[0]) * amount, a[1] + (b[1] - a[1]) * amount, a[2] + (b[2] - a[2]) * amount]);
}
/** The same colour with its hue turned by `degrees`; lightness and chroma stay. */
export function rotateHue(rgb: Rgb, degrees: number): Rgb {
  const [L, C, H] = toLch(rgb);
  return fromLch([L, C, (H + degrees + 360) % 360]);
}

const luminance = ([r, g, b]: Rgb) => 0.2126 * toLinear(r) + 0.7152 * toLinear(g) + 0.0722 * toLinear(b);
/** WCAG contrast ratio of two colours, 1 to 21. */
export function contrastRatio(a: Rgb, b: Rgb): number {
  const [x, y] = [luminance(a), luminance(b)];
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}
/** Text on a pale field needs this much; the palette keeps every hue at or above it. */
const readableRatio = 4.5;
/** The colour moved in lightness only, away from the field, until it reads at `ratio`; hue and chroma stay. */
export function readable(color: Rgb, field: Rgb, ratio: number = readableRatio): Rgb {
  const [L, C, H] = toLch(color);
  const away = luminance(field) > 0.18 ? -1 : 1;
  let lightness = L;
  for (let step = 0; step < 100 && contrastRatio(fromLch([lightness, C, H]), field) < ratio; step++) lightness = Math.min(1, Math.max(0, lightness + away * 0.01));
  return fromLch([lightness, C, H]);
}

export const hex = ([r, g, b]: Rgb) => `#${[r, g, b].map(v => v.toString(16).padStart(2, '0')).join('')}`;

const cubeLevels = [0, 95, 135, 175, 215, 255];
/**
 * The 240 colours a program reaches with `38;5;N` for N from 16 to 255: the 6x6x6 cube, then 24 greys.
 * Same rule as the sixteen: every hue at `chroma` of its chroma; the greys carry none and stay as they are.
 */
export function extendedPalette(chroma: number = ansiChroma): Rgb[] {
  const cube = cubeLevels.flatMap(r => cubeLevels.flatMap(g => cubeLevels.map((b): Rgb => desaturate([r, g, b], chroma))));
  const greys = Array.from({ length: 24 }, (_, step): Rgb => { const level = 8 + step * 10; return [level, level, level]; });
  return [...cube, ...greys];
}

/** The tokens the palette is built from, resolved to sRGB by the caller. */
export type AnsiRoles = { field: Rgb; ink: Rgb; ink2: Rgb; ink3: Rgb; accent: Rgb; danger: Rgb; success: Rgb; amber: Rgb };

/**
 * The sixteen ANSI colours. Neutrals come straight from the ink tokens (they carry no chroma worth cutting);
 * every hue is a token's hue at `chroma` of its chroma, then kept readable on the field. Magenta and cyan sit a step either side of the accent.
 * Index order is the ANSI order: black, red, green, yellow, blue, magenta, cyan, white, then the bright eight.
 */
export function ansiPalette(roles: AnsiRoles, chroma: number = ansiChroma): Rgb[] {
  const hue = (rgb: Rgb) => readable(desaturate(rgb, chroma), roles.field);
  const normal: Rgb[] = [
    roles.ink3, hue(roles.danger), hue(roles.success), hue(roles.amber), hue(roles.accent),
    hue(rotateHue(roles.accent, hueStep)), hue(rotateHue(roles.accent, -hueStep)), roles.ink2,
  ];
  const bright = normal.map((color, index) => (index === 0 ? roles.ink3 : index === 7 ? roles.ink : mix(color, roles.ink, brightMix)));
  return [...normal, ...bright];
}
