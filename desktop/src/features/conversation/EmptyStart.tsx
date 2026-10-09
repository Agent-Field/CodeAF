import { Text } from '../../components/ui';
import './empty-start.css';

/** The empty conversation's only line; the composer sits centred beneath it. */
export function EmptyStart() {
  return <Text className="empty-start-title">What are we building?</Text>;
}
