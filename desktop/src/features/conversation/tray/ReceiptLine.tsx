import { Button, Icon, type IconName } from '../../../components/ui';

export type ReceiptState = 'waiting' | 'answered' | 'withdrawn';

const ICONS: Record<ReceiptState, IconName> = { waiting: 'queued', answered: 'check', withdrawn: 'cancelled' };

/** The quiet line left in the conversation where a question was asked. Waiting ones focus the tray. */
export function ReceiptLine({ state, text, onFocus }: { state: ReceiptState; text: string; onFocus?: () => void }) {
  const content = (
    <>
      <Icon name={ICONS[state]} size="xs" />
      <span className="tray-receipt-text">{text}</span>
    </>
  );
  if (state === 'waiting' && onFocus) {
    return (
      <Button className="tray-receipt" onClick={onFocus}>
        {content}
      </Button>
    );
  }
  return <div className="tray-receipt-static">{content}</div>;
}
