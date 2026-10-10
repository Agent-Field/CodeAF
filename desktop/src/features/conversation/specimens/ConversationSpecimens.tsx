import { SectionHeading, Surface, Text } from '../../../components/ui';
import { DecisionTraySpecimen } from '../tray/specimens/DecisionTray.specimen';
import { AssetsSpecimen } from './Assets.specimen';
import { ComposerSpecimen } from './Composer.specimen';
import { FoldingSpecimen } from './Folding.specimen';
import { LatestPillSpecimen } from './LatestPill.specimen';
import { MarkdownSpecimen } from './Markdown.specimen';
import { SystemNotesSpecimen } from './SystemNotes.specimen';
import { TaskDetailSpecimen } from '../detail/TaskDetail.specimen';
import { TaskNoticeSpecimen } from './TaskNotice.specimen';
import { TaskPanelSpecimen } from './TaskPanel.specimen';
import { TasksTableSpecimen } from './TasksTable.specimen';
import { TaskViewSpecimen } from './TaskView.specimen';
import { TurnViewV2Specimen } from './TurnViewV2.specimen';
import { UserMessageSpecimen } from './UserMessage.specimen';
import { WorkSpecimen } from './Work.specimen';
import { CouncilSpecimen } from './Council.specimen';

const specimens = [
  { name: 'Turns', View: TurnViewV2Specimen },
  { name: 'User message', View: UserMessageSpecimen },
  { name: 'Long history', View: FoldingSpecimen },
  { name: 'Replies', View: MarkdownSpecimen },
  { name: 'Work', View: WorkSpecimen },
  { name: 'Assets', View: AssetsSpecimen },
  { name: 'System notes', View: SystemNotesSpecimen },
  { name: 'Task notices', View: TaskNoticeSpecimen },
  { name: 'Decision tray', View: DecisionTraySpecimen },
  { name: 'Composer', View: ComposerSpecimen },
  { name: 'Latest pill', View: LatestPillSpecimen },
  { name: 'Task panel', View: TaskPanelSpecimen },
  { name: 'Tasks table', View: TasksTableSpecimen },
  { name: 'Task view', View: TaskViewSpecimen },
  { name: 'Task detail', View: TaskDetailSpecimen },
  { name: 'Council', View: CouncilSpecimen },
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
