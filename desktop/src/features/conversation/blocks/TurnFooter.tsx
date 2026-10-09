import { Button, Icon } from '../../../components/ui';
import type { TurnV2 } from '../types';
import './turn-answer.css';

type TurnFooterProps = {
  state: TurnV2['state'];
  onRetry?: () => void;
  retrying?: boolean;
};

/** The quiet line that ends a turn which did not end well; nothing for a turn that did. */
export function TurnFooter({ state, onRetry, retrying = false }: TurnFooterProps) {
  if (retrying) {
    return (
      <div className="turn-footer" role="status">
        <span className="turn-footer-dot" aria-hidden="true" />
        <span>Retrying</span>
      </div>
    );
  }
  if (state === 'stopped') {
    return (
      <div className="turn-footer" role="status">
        <span className="turn-footer-icon">
          <Icon name="ban" size="xs" />
        </span>
        <span>Stopped</span>
      </div>
    );
  }
  if (state !== 'failed') return null;
  return (
    <div className="turn-footer" data-failed="true" role="alert">
      <span className="turn-footer-icon">
        <Icon name="alert" size="xs" />
      </span>
      <span>Failed</span>
      {onRetry && (
        <Button className="turn-footer-retry" onClick={onRetry}>
          Retry
        </Button>
      )}
    </div>
  );
}
