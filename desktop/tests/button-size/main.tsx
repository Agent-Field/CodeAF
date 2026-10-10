import { createRoot } from 'react-dom/client';
import '../../src/App.css';
import { Button, type ButtonVariant } from '../../src/components/ui/Button';

const variants: ButtonVariant[] = ['primary', 'raised', 'quiet', 'ghost', 'danger'];
createRoot(document.getElementById('root')!).render(<>
 {variants.map(variant => <section key={variant}>
  <Button variant={variant}>Default {variant}</Button>
  <Button variant={variant} size="control">Control {variant}</Button>
  <Button variant={variant} size="tray">Tray {variant}</Button>
 </section>)}
 <Button size="tray" disabled>Disabled tray</Button>
 <Button size="tray" loading>Loading tray</Button>
 <Button size="tray" onClick={event => { event.currentTarget.textContent = 'Answered'; }}>Answer</Button>
</>);
