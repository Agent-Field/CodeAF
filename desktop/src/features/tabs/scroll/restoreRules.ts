import type { ScrollSpot } from './scrollMemory';

/** A pane left following its tail restores to today's end, rather than yesterday's offset. */
export const restoredTop = (spot: ScrollSpot, maxTop: number) => spot.end ? maxTop : spot.top;

/** Delayed transcript replay must grow to the saved offset before restoration can settle. */
export const canRestore = (spot: ScrollSpot, maxTop: number, maxLeft: number) =>
  (spot.end || spot.top <= maxTop + 0.5) && spot.left <= maxLeft + 0.5;
