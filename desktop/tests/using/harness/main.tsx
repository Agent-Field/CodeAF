// Test-only entry for the Using list: it mounts the specimen alone, so the browser tests never depend on the app shell.
import '../../../src/design/inputModality';
import '../../../src/App.css';
import React from 'react';
import ReactDOM from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { UsingSpecimen } from './UsingSpecimen';

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <ThemeProvider><main><UsingSpecimen /></main></ThemeProvider>
  </React.StrictMode>,
);
