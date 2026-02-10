import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import './styles/index.css';
import { initializeTheme } from './stores/themeStore';
import { ErrorBoundary } from './components/ErrorBoundary';

// Initialize theme before rendering
initializeTheme();

// Global error handler for unhandled errors
const handleGlobalError = (error: Error, errorInfo: React.ErrorInfo) => {
  // Log to console
  console.error('[Global Error Boundary]', error, errorInfo);
};

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ErrorBoundary onError={handleGlobalError}>
      <App />
    </ErrorBoundary>
  </React.StrictMode>
);
