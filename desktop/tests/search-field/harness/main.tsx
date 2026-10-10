import React, { useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import '../../../src/App.css';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { Button } from '../../../src/components/ui/Button';
import { SearchField } from '../../../src/components/ui/SearchField';
function Specimen() {
 const [value, setValue] = useState('');
 const field = useRef<HTMLInputElement>(null);
 return <main><h1>SearchField specimen</h1><Button onClick={() => field.current?.focus()}>Focus search</Button>
  <SearchField ref={field} label="Search conversations" placeholder="Search what you discussed, decided or changed" value={value} onChange={setValue} />
  <output aria-label="Search query">{value}</output><Button>After search</Button>
 </main>;
}
createRoot(document.getElementById('root')!).render(<React.StrictMode><ThemeProvider><Specimen /></ThemeProvider></React.StrictMode>);
