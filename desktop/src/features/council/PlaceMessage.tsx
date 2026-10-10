import { PlaceSwatch, type TintName } from '../places/components/PlaceSwatch';
import './council.css';

/** The Composer placeholder in a council: the person is adding to a discussion, not starting a chat. */
export const COUNCIL_PLACEHOLDER = 'Add to this discussion';

export type CouncilSpeaker = { name: string; tint: TintName };

/** One place's turn in a council: its tint square and name, then what it said. Plain flow, no bubble, because the person is a reader here. */
export function PlaceMessage({ speaker, text }: { speaker: CouncilSpeaker; text: string }) {
  return (
    <div className="council-message" data-place={speaker.name}>
      <span className="council-message-swatch"><PlaceSwatch tint={speaker.tint} role="tile"/></span>
      <div className="council-message-text">
        <span className="council-message-name">{speaker.name}</span>
        <span className="council-message-body">{text}</span>
      </div>
    </div>
  );
}
