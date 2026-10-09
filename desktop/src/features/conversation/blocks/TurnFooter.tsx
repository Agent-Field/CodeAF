import { Button, Icon } from '../../../components/ui';
import type { TurnV2 } from '../types';

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
        <span>Retrying</span>
      </div>
    );
  }
  if (state === 'stopped') {
    return (
      <div className="turn-footer" role="status">
        <Icon name="cancelled" size="sm" />
        <span>Stopped</span>
      </div>
    );
  }
  if (state !== 'failed') return null;
  return (
    <div className="turn-footer" data-failed="true" role="alert">
      <Icon name="alert" size="sm" />
      <span>Failed</span>
      {onRetry && <Button onClick={onRetry}>Retry</Button>}
    </div>
  );
}
