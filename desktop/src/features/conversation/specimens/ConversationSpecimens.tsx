import { SectionHeading, Surface, Text } from '../../../components/ui';
import { ComposerSpecimen } from './Composer.specimen';
import { MarkdownSpecimen } from './Markdown.specimen';
import { QuestionCardSpecimen } from './QuestionCard.specimen';
import { TaskNoticeSpecimen } from './TaskNotice.specimen';
import { TaskPanelSpecimen } from './TaskPanel.specimen';
import { TaskViewSpecimen } from './TaskView.specimen';
import { ToolGroupSpecimen } from './ToolGroup.specimen';
import { TurnViewSpecimen } from './TurnView.specimen';

const specimens = [
  { name: 'Turns', View: TurnViewSpecimen },
  { name: 'Replies', View: MarkdownSpecimen },
  { name: 'Tools', View: ToolGroupSpecimen },
  { name: 'Task notices', View: TaskNoticeSpecimen },
  { name: 'Questions', View: QuestionCardSpecimen },
  { name: 'Composer', View: ComposerSpecimen },
  { name: 'Task panel', View: TaskPanelSpecimen },
  { name: 'Task view', View: TaskViewSpecimen },
];

/** Every conversation component with fixture props. Fixtures live only here and in tests. */
export function ConversationSpecimens() {
  return (
    <Surface direction="column">
      <SectionHeading>Conversation</SectionHeading>
      <Text>Specimen</Text>
      {specimens.map(({ name, View }) => (
        <section key={name} className="conversation-specimen" aria-label={`${name} specimen`}>
          <Text tone="default">{name}</Text>
          <View />
        </section>
      ))}
    </Surface>
  );
}
