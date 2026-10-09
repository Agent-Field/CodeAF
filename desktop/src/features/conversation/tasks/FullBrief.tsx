import { Text } from '../../../components/ui';
import { headingLabel, type BriefSection } from './brief';
import './full-brief.css';

/** Every section of the engine's brief under its own quiet label, for the reader who wants all of it. */
export function FullBrief({ sections }: { sections: BriefSection[] }) {
  return (
    <div className="full-brief" role="group" aria-label="Full brief">
      {sections.map((section) => (
        <section key={section.heading} className="full-brief-section">
          <h4 className="full-brief-label">{headingLabel(section.heading)}</h4>
          <Text className="full-brief-body">{section.body}</Text>
        </section>
      ))}
    </div>
  );
}
