import { fileURLToPath } from 'node:url';
import { dirname } from 'node:path';
import js from '@eslint/js';
import tseslint from 'typescript-eslint';
import react from 'eslint-plugin-react';
import reactHooks from 'eslint-plugin-react-hooks';
import globals from 'globals';

const __dirname = dirname(fileURLToPath(import.meta.url));

export default tseslint.config(
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx}'],
    plugins: {
      react,
      'react-hooks': reactHooks,
    },
    languageOptions: {
      parserOptions: {
        ecmaFeatures: {
          jsx: true,
        },
        project: './tsconfig.eslint.json',
        tsconfigRootDir: __dirname,
      },
    },
    rules: {
      // Async/Promise Safety - CRITICAL
      '@typescript-eslint/no-floating-promises': 'error',
      '@typescript-eslint/await-thenable': 'error',
      '@typescript-eslint/no-misused-promises': 'error',
      '@typescript-eslint/promise-function-async': 'warn',

      // Unused Variables (with exceptions for callbacks)
      '@typescript-eslint/no-unused-vars': ['warn', {
        argsIgnorePattern: '^_',
        varsIgnorePattern: '^_',
        caughtErrorsIgnorePattern: '^_',
      }],

      // Type Safety
      '@typescript-eslint/no-explicit-any': 'warn',
      '@typescript-eslint/explicit-function-return-type': 'off',
      '@typescript-eslint/no-non-null-assertion': 'warn',

      // React Hooks
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',

      // General Best Practices
      'no-console': ['warn', { allow: ['warn', 'error'] }],
      'no-debugger': 'error',
      'no-duplicate-imports': 'error',
      'prefer-const': 'error',
      'no-var': 'error',
      eqeqeq: ['error', 'always', { null: 'ignore' }],
    },
    settings: {
      react: {
        version: 'detect',
      },
    },
  },
  {
    // Main process files (Node.js)
    files: ['src/main/**/*.ts'],
    rules: {
      'no-console': 'off', // Console is OK in main process
    },
  },
  {
    // Vanilla browser script for the support portal. It is a plain <script>
    // bundle whose top-level functions are entry points invoked from inline
    // HTML handlers (onclick=/onsubmit= in index.html and in dynamically
    // generated markup), which ESLint cannot statically see as references.
    files: ['src/portal/**/*.js'],
    languageOptions: {
      globals: {
        ...globals.browser,
      },
    },
    rules: {
      // The typed-linting variant leaks onto this non-TS file; the core rule
      // is the correct one for plain browser JS.
      '@typescript-eslint/no-unused-vars': 'off',
      // Top-level functions here are HTML-invoked entry points; treat every
      // declaration as exported so genuinely-used handlers are not flagged,
      // while still catching unused locals/args (ignore the `_`-prefixed
      // convention, consistent with the TS block).
      'no-unused-vars': ['warn', {
        vars: 'local',
        args: 'after-used',
        argsIgnorePattern: '^_',
        varsIgnorePattern: '^_',
        caughtErrorsIgnorePattern: '^_',
      }],
    },
  },
  {
    ignores: [
      'dist/**',
      'release/**',
      'node_modules/**',
      '*.config.js',
      '*.config.ts',
    ],
  }
);
