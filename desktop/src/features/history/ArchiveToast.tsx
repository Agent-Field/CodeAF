import { useEffect, useRef } from 'react';
import { toast } from '../../components/ui/Toast';

type Props = { count: number; onReview: () => void; onRestore: () => void; onDismiss: () => void };

/** The archive notice uses the window's shared host, so placement, timing and held actions have one owner. */
export function ArchiveToast(props: Props) {
  const latest = useRef(props);
  latest.current = props;
  useEffect(() => {
    const id = toast.show({
      lead: 'archive',
      message: `Archived ${props.count} ${props.count === 1 ? 'tab' : 'tabs'} idle for more than 12h`,
      actions: [
        { label: 'Review', kind: 'ghost', onAction: () => latest.current.onReview() },
        { label: 'Restore all', kind: 'field', onAction: () => latest.current.onRestore() },
      ],
    });
    // Inline workspace callbacks change on every render; they must neither restart the notice nor retain old tabs.
    const unsubscribe = toast.channel.subscribe(() => {
      if (!toast.channel.getToasts().some(notice => notice.id === id)) latest.current.onDismiss();
    });
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !document.querySelector('dialog[open]')) toast.dismiss(id);
    };
    window.addEventListener('keydown', onKey);
    return () => {
      unsubscribe();
      window.removeEventListener('keydown', onKey);
      toast.dismiss(id);
    };
  }, [props.count]);
  return null;
}
