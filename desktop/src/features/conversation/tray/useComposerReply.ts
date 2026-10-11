import { useLayoutEffect, useState } from 'react';
import type { ComposerTarget } from './composerTarget';

/** A new card offers itself to an existing chat draft; paging a bound reply keeps it bound. */
export function useComposerReply(target: ComposerTarget | null, draft: string) {
  const [binding, setBinding] = useState<{ key: string | null; mode: 'bound' | 'offer' | 'dismissed' }>({ key: null, mode: 'offer' });
  useLayoutEffect(() => {
    const key = target?.key ?? null;
    setBinding(before => before.key === key ? before : { key, mode: key && (before.key && before.mode === 'bound' || draft === '') ? 'bound' : 'offer' });
  }, [target?.key, draft]);
  const bound = Boolean(target && binding.key === target.key && binding.mode === 'bound');
  return {
    bound,
    bind: (key = target?.key) => key && setBinding({ key, mode: 'bound' }),
    chip: target && binding.key === target.key && binding.mode !== 'dismissed' ? {
      title: target.title, bound,
      bind: () => setBinding({ key: target.key, mode: 'bound' }),
      dismiss: () => setBinding({ key: target.key, mode: 'dismissed' }),
    } : undefined,
  };
}
