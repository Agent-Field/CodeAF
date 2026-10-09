import { Text } from '../../../components/ui';
import type { Question } from './form';

/** Whether answering can wait: only a question the reply is not held on says so. */
export const doesNotBlock = (members: Question[]) => !members.some((member) => member.blocking?.turn);

/** "Doesn't block this reply" sits at the right end of the action row (Conversation 1a), never under the card. */
export function NonBlockingNote() {
  return <Text className="tray-note">Doesn&rsquo;t block this reply</Text>;
}
