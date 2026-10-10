import { useEffect, useState } from 'react';
import { keyStatusWords } from './keyClient';

/**
 * The provider key row: where the key comes from, in words only. The secret never reaches the renderer, so there is
 * no value, mask or length to show; while loading or when the engine cannot say, the row draws nothing.
 */
export function KeyStatus() {
  const [words, setWords] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    void keyStatusWords().then(next => { if (live) setWords(next); });
    return () => { live = false; };
  }, []);
  if (!words) return null;
  return (
    <li className="settings-row settings-row-inline" data-setting="provider-key">
      <span className="settings-row-name">Provider key</span>
      <span className="settings-row-controls">{words}</span>
    </li>
  );
}
