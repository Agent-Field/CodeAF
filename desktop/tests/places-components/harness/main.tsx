// Test-only entry for the Places primitives: it mounts the specimen alone, so the browser tests never depend on the app shell
// (which owns wiring later). It is not part of the production build, which only builds the root index.html.
import '../../../src/design/inputModality';
import '../../../src/App.css';
import React from 'react';
import ReactDOM from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { PlacesComponentsSpecimen } from '../../../src/features/places/specimens/PlacesComponentsSpecimen';

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <ThemeProvider><main className="places-harness"><PlacesComponentsSpecimen /></main></ThemeProvider>
  </React.StrictMode>,
);
