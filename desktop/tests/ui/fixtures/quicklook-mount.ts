import { createElement as h, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { QuickLook } from '../../../src/components/ui/QuickLook';
import { Button } from '../../../src/components/ui';
import '../../../src/styles/tokens.css';
import '../../../src/styles/ui.css';

/** Mounts an opener button and a Quick Look sheet over it, so focus return and the Space/Esc closes can be asserted on the real primitive. */
export function mountQuickLook(theme: string) {
  localStorage.setItem('codeaf-theme', theme);
  const host = document.createElement('main');
  document.body.replaceChildren(host);
  function Demo() {
    const [open, setOpen] = useState(false);
    return h('div', null,
      h('button', { id: 'opener', onClick: () => setOpen(true) }, 'Open'),
      h(QuickLook, { open, onClose: () => setOpen(false), title: 'Marketing', tint: 'tide',
        footer: h(Button, { variant: 'primary' }, 'Go to'), children: h('input', { id: 'field', 'aria-label': 'Note' }) }));
  }
  createRoot(host).render(h(ThemeProvider, null, h(Demo)));
}
