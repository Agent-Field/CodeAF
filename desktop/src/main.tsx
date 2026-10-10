import './design/inputModality';
import { ThemeProvider } from './design/ThemeProvider';
import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { connectAttentionNotifications } from "./lib/native/notify";

const stopNotifications = connectAttentionNotifications();
import.meta.hot?.dispose(stopNotifications);

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <ThemeProvider><App /></ThemeProvider>
  </React.StrictMode>,
);
