import { SystemNote } from './SystemNote';

/** A failure: the one system note with colour and an action. */
export function ErrorItem({ text, onRetry }: { text: string; onRetry?: () => void }) {
  if (!text) return null;
  return (
    <SystemNote kind="failure" action={onRetry && { label: 'Retry', onClick: onRetry }}>
      {text}
    </SystemNote>
  );
}
