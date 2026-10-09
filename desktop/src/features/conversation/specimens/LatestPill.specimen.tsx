import { LatestPill } from '../LatestPill';

const noop = () => undefined;
// A fixed clock keeps the specimen still: 1m 13s after the start, the design's own figure.
const NOW = 1_700_000_000_000;

/** The pill's two states over a dock-height stage: something live below, and only something new. */
export function LatestPillSpecimen() {
  return (
    <div className="latest-pill-stage">
      <div className="latest-pill-stage-dock"><LatestPill working since={NOW - 73_000} now={NOW} onJump={noop} /></div>
      <div className="latest-pill-stage-dock"><LatestPill working={false} onJump={noop} /></div>
    </div>
  );
}
