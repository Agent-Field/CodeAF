/** A deliberate 12% spread opens Overview; small trackpad noise does not. These are gesture thresholds, not UI geometry. */
export const OVERVIEW_PINCH_SCALE = 1.12;
const WHEEL_SEQUENCE_GAP_MS = 200;

export function createOverviewPinch() {
  let wheelScale = 1;
  let lastWheel = -Infinity;
  let opened = false;
  return {
    reset() { wheelScale = 1; lastWheel = -Infinity; opened = false; },
    scale(scale: number) {
      if (opened || !Number.isFinite(scale) || scale < OVERVIEW_PINCH_SCALE) return false;
      opened = true;
      return true;
    },
    wheel(deltaY: number, now: number) {
      if (!Number.isFinite(deltaY) || !Number.isFinite(now)) return false;
      if (now - lastWheel > WHEEL_SEQUENCE_GAP_MS) { wheelScale = 1; opened = false; }
      lastWheel = now;
      // DOM wheel delta is negative for a spread. Inward movement cancels accumulated outward movement.
      wheelScale = deltaY < 0 ? wheelScale * Math.exp(-deltaY / 100) : 1;
      return this.scale(wheelScale);
    },
  };
}
