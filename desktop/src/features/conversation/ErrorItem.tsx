import { Button } from '../../components/ui';

export function ErrorItem({ text, onRetry }: { text: string; onRetry?: () => void }) {
  if (!text) return null;
  return (
    <div className="error-item" role="alert">
      <span className="error-item-text">{text}</span>
      {onRetry && <Button onClick={onRetry}>Retry</Button>}
    </div>
  );
}
