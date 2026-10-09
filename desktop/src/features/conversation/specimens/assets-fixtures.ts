import type { EngineFile, EnginePathFact } from '../../chat/engine-client';
import type { AssetContextValue } from '../assets/AssetContext';

/** Fixtures for specimens and tests only; the runtime never invents files. */
export const workspace = '/home/dev/acme-shop';

const goSource = `package cart

import "errors"

// ErrEmpty is returned when a checkout starts with nothing in the cart.
var ErrEmpty = errors.New("cart is empty")

type Line struct {
	SKU   string
	Qty   int
	Cents int64
}

// Total adds every line, in cents, so rounding never drifts.
func Total(lines []Line) (int64, error) {
	if len(lines) == 0 {
		return 0, ErrEmpty
	}
	var sum int64
	for _, l := range lines {
		sum += int64(l.Qty) * l.Cents
	}
	return sum, nil
}
`;

const readme = '# Acme shop\n\nA small storefront. Run `make dev` to start it.\n\n## Layout\n\n- `cart/` pricing and totals\n- `web/` the storefront\n';

function svg(from: string, to: string, shape: string): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 480"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${from}"/><stop offset="1" stop-color="${to}"/></linearGradient></defs><rect width="640" height="480" fill="url(#g)"/>${shape}</svg>`;
}

const pictures: Record<string, string> = {
  'out/hero-sunrise.svg': svg('#f6c177', '#c4745a', '<circle cx="320" cy="300" r="120" fill="#fdf0d5" opacity=".85"/><rect y="300" width="640" height="180" fill="#6b4a3a" opacity=".55"/>'),
  'out/hero-dusk.svg': svg('#6c7aa8', '#2c2f4a', '<circle cx="470" cy="150" r="60" fill="#e8e1d0" opacity=".9"/><rect y="330" width="640" height="150" fill="#1d1f33" opacity=".7"/>'),
  'out/hero-fog.svg': svg('#b7c4bd', '#6f857b', '<rect x="80" y="220" width="480" height="40" rx="20" fill="#eef2ee" opacity=".6"/><rect x="140" y="290" width="360" height="40" rx="20" fill="#eef2ee" opacity=".45"/>'),
};

function wav(): string {
  const rate = 8000;
  const samples = rate * 2;
  const bytes = new Uint8Array(44 + samples);
  const view = new DataView(bytes.buffer);
  const tag = (at: number, text: string) => [...text].forEach((c, i) => view.setUint8(at + i, c.charCodeAt(0)));
  tag(0, 'RIFF');
  view.setUint32(4, 36 + samples, true);
  tag(8, 'WAVEfmt ');
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, rate, true);
  view.setUint32(28, rate, true);
  view.setUint16(32, 1, true);
  view.setUint16(34, 8, true);
  tag(36, 'data');
  view.setUint32(40, samples, true);
  bytes.fill(128, 44);
  return btoa(String.fromCharCode(...bytes));
}

const sources: Record<string, { mime: string; data: string }> = {
  'cart/total.go': { mime: 'text/plain', data: btoa(goSource) },
  'cart/total_test.go': { mime: 'text/plain', data: btoa('package cart\n') },
  'web/checkout/summary.tsx': { mime: 'text/plain', data: btoa('export const Summary = () => null;\n') },
  'README.md': { mime: 'text/markdown', data: btoa(readme) },
  'docs/pricing.pdf': { mime: 'application/pdf', data: btoa('%PDF-1.4') },
  'out/welcome.wav': { mime: 'audio/wav', data: wav() },
  ...Object.fromEntries(Object.entries(pictures).map(([path, body]) => [path, { mime: 'image/svg+xml', data: btoa(body) }])),
};

const directories = new Set(['cart', 'web', 'out']);
const outside = new Set(['/etc/hosts']);

function fact(path: string): EnginePathFact {
  const rel = path.startsWith(`${workspace}/`) ? path.slice(workspace.length + 1) : path;
  if (outside.has(path)) return { path, exists: true, dir: false, size: 120, outside: true };
  if (directories.has(rel)) return { path, exists: true, dir: true, size: 0 };
  const source = sources[rel];
  return source ? { path, exists: true, dir: false, size: source.data.length } : { path, exists: false, dir: false, size: 0 };
}

function letterFavicon(letter: string): string {
  const body = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><rect width="16" height="16" rx="3" fill="#24292f"/><text x="8" y="12" font-size="10" text-anchor="middle" fill="#fff" font-family="sans-serif">${letter}</text></svg>`;
  return `data:image/svg+xml;base64,${btoa(body)}`;
}

/** A fake engine: a small in-memory workspace, one contacted domain with a real icon. */
export const fakeAssets: AssetContextValue = {
  available: true,
  sessionId: 'specimen',
  workspace,
  readFile: async (path: string): Promise<EngineFile> => {
    const rel = path.startsWith(`${workspace}/`) ? path.slice(workspace.length + 1) : path;
    const source = sources[rel];
    if (!source) throw new Error('not found');
    return { name: rel, mime: source.mime, size: source.data.length, hash: 'fixture', dataBase64: source.data };
  },
  stat: async paths => paths.map(fact),
  peek: () => undefined,
  favicon: async domain => (domain === 'github.com' ? letterFavicon('G') : null),
  openPath: async () => undefined,
  revealPath: async () => undefined,
  openUrl: async () => undefined,
};
