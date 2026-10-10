import { StatusMark } from '../../components/ui/StatusMark';
import { useTooltip } from '../../components/ui/Tooltip';

/** The rail names attention in words; running work stays quiet (Places 10a). */
export function PlaceDot({ status, label }: { status?: 'waiting' | 'failed'; label?: string }) {
  const words = label || (status === 'waiting' ? 'Needs you' : 'Failed');
  const tooltip = useTooltip<HTMLSpanElement>(words);
  if (status !== 'waiting' && status !== 'failed') return null;
  // The shared dense mark keeps both the six-pixel geometry and themed status colours in one place.
  // A passive trigger lets a press on the dot reach the place row's own action.
  return <span {...tooltip.props}>
    <StatusMark status={status} label={words} dense/>
    {tooltip.element}
  </span>;
}
