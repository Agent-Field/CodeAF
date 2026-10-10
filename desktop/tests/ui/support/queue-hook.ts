// Vite resolves these imports together so the hook and renderer share one React instance.
export { default as React } from 'react';
export { createRoot } from 'react-dom/client';
export { useConversation } from '../../../src/features/conversation/useConversation';
