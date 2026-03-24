import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e/playwright',
  timeout: 60000,
  retries: 1,
  use: {
    baseURL: process.env.SENTINEL_URL || 'https://sentinel.nexus',
    extraHTTPHeaders: {
      'Accept': 'application/json',
    },
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    ignoreHTTPSErrors: true,
  },
  projects: [
    {
      name: 'api-tests',
      testMatch: /.*\.api\.spec\.ts/,
    },
    {
      name: 'e2e-tests',
      testMatch: /.*\.e2e\.spec\.ts/,
      use: {
        browserName: 'chromium',
      },
    },
  ],
});
