import { useEffect, useRef, useState } from 'react';
import { TextInput } from '../../components/ui';
import type { WebFindResult } from '../../design/nativeWeb';

type Props = {
  onFind: (query: string, forward: boolean) => Promise<WebFindResult>;
  onClose: () => void;
};

/** A native result owns the count; stepping never invents an ordinal. */
export function FindBar({ onFind, onClose }: Props) {
  const [query, setQuery] = useState('');
  const [result, setResult] = useState<WebFindResult>();
  const input = useRef<HTMLInputElement>(null);
  const request = useRef(0);
  useEffect(() => {
    input.current?.focus();
    return () => { request.current++; };
  }, []);

  async function search(value: string, forward: boolean) {
    const id = ++request.current;
    setResult(undefined);
    try {
      const next = await onFind(value, forward);
      if (id === request.current && value) setResult(next);
    } catch {
      // A failed native request cannot supply a count.
      if (id === request.current) setResult(undefined);
    }
  }

  function close() {
    request.current++;
    void onFind('', true).catch(() => undefined);
    onClose();
  }

  return <span className="web-address web-find" role="search" aria-label="Find in page">
    <TextInput ref={input} appearance="field" className="web-address-input web-find-input" aria-label="Find in page"
      placeholder="Find in page" value={query} spellCheck={false} autoCapitalize="off" autoCorrect="off"
      onChange={event => { setQuery(event.target.value); void search(event.target.value, true); }}
      onKeyDown={event => {
        if (event.nativeEvent.isComposing) return;
        if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); }
        if (event.key === 'Enter') { event.preventDefault(); event.stopPropagation(); if (query) void search(query, !event.shiftKey); }
      }}/>
    {query && result?.matches !== undefined && <span className="web-find-count" role="status">{result.matches} matches</span>}
  </span>;
}
