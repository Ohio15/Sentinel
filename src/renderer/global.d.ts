// Global type declarations for Web mode

/// <reference types="vite/client" />

// Load the @testing-library/jest-dom matcher augmentations for Vitest's
// `Assertion` interface (e.g. toBeInTheDocument, toHaveClass, toBeDisabled).
// The Vitest setup file imports this at runtime, but it lives outside this
// tsconfig's `include` scope, so the type augmentation must be referenced here
// for `tsc --noEmit` to see the matchers on `expect(...)` in test files.
import '@testing-library/jest-dom/vitest';

export {};
