import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// App is the one place these window pieces meet. A missing attribute or host is invisible in a unit of the piece itself.
const src = readFileSync(new URL('./App.tsx', import.meta.url), 'utf8');

test('the app shell wears the window tint the hook returns', () => {
  assert.match(src, /const frameTint = useWindowTint\(shell\)/);
  assert.match(src, /data-tint=\{frameTint\}/);
  assert.match(src, /className=\{`app-shell /);
});

test('the rail, the place keys, and the palette and Quick Look hosts are mounted', () => {
  assert.match(src, /usePlaceKeys\(shell, enterWorkspace\)/);
  assert.match(src, /<PlaceRail \{\.\.\.rail\}\/>/);
  // PlacesOverlays owns the Go to palette host and the Quick Look sheet. App mounts that one host.
  assert.match(src, /<PlacesOverlays shell=\{shell\}\/>/);
});

test('the Design system page shows the Places specimen', () => {
  assert.match(src, /import \{ PlacesSpecimen \} from '\.\/features\/places\/specimens\/PlacesSpecimen'/);
  assert.match(src, /<PlacesSpecimen\/>/);
  assert.match(src, /page === 'Design system'/);
});
