import { useEffect, useId, useRef, useState } from 'react';
import { Button, TextInput } from '../../components/ui';
import { KindIcon } from '../tabs/Tab';
import { addressParts, toAddress } from './address';

type Props = {
  url?: string;
  initialEditing?: boolean;
  favicon?: string;
  /** A transient line shown in place of the path (a refused download, a blocked popup). */
  status?: string;
  onGo: (url: string) => void;
};

/**
 * The slim address field (design 3d): at rest it reads as the site in ink and
 * the path dimmed, with the site's monogram; pressing it turns it into an
 * input holding the whole address. Enter goes, Escape puts the address back.
 * An empty pane starts in the input.
 */
export function AddressField({ url, favicon, status, onGo, initialEditing = false }: Props) {
  const [editing, setEditing] = useState(initialEditing || !url);
  const [value, setValue] = useState(url ?? '');
  const [refusal, setRefusal] = useState('');
  const input = useRef<HTMLInputElement>(null);
  const refusalId = useId();
  useEffect(() => { if (!editing) setValue(url ?? ''); }, [url, editing]);
  useEffect(() => {
    if (!editing || initialEditing) return;
    input.current?.focus();
    input.current?.select();
  }, [editing, initialEditing]);

  function stop() {
    setEditing(!url);
    setRefusal('');
    setValue(url ?? '');
    input.current?.blur();
  }

  if (!editing && url) {
    const { site, rest } = addressParts(url);
    return <span className="web-address-holder"><Button className="web-address" aria-label={`Address ${url}. Edit address`} onClick={() => setEditing(true)}>
      <KindIcon favicon={favicon} kind="web" title={site}/>
      <span className="web-address-site">{site}</span>
      <span className="web-address-rest" role={status ? 'status' : undefined}>{status ?? rest}</span>
    </Button></span>;
  }
  return <span className="web-address-holder"><form className="web-address web-address-editing" role="search" aria-label="Go to an address" onSubmit={event => {
    event.preventDefault();
    const address = toAddress(value);
    if ('refusal' in address) { setRefusal(address.refusal); return; }
    setRefusal('');
    setEditing(false);
    onGo(address.url);
  }}>
    <TextInput ref={input} appearance="field" className="web-address-input" aria-label="Address" aria-invalid={!!refusal || undefined} aria-describedby={refusal ? refusalId : undefined}
      placeholder="Type a web address" value={value} spellCheck={false} autoCapitalize="off" autoCorrect="off" maxLength={4096}
      onChange={event => { setValue(event.target.value); setRefusal(''); }}
      onKeyDown={event => { if (event.key === 'Escape' && url) { event.preventDefault(); stop(); } }}
      onBlur={event => { if (url && !event.currentTarget.form?.contains(event.relatedTarget as Node | null)) stop(); }}/>
    {refusal && <span id={refusalId} className="web-address-refusal" role="alert">{refusal}</span>}
  </form></span>;
}
