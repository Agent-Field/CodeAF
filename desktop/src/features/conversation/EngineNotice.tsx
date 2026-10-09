import { Button } from '../../components/ui';

/** The one line shown when nothing answers; typing stays allowed and the draft is kept. */
export function EngineNotice({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="engine-notice" role="status">
      <span>codeaf engine is not running</span>
      <Button onClick={onRetry}>Retry</Button>
    </div>
  );
}
