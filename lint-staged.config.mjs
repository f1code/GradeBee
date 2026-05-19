export default {
  'frontend/src/**/*.{ts,tsx}': (files) => [
    `cd frontend && pnpm exec eslint --fix ${files.join(' ')}`,
    `cd frontend && pnpm exec prettier --write ${files.join(' ')}`,
  ],
  'frontend/src/**/*.css': (files) => [
    `cd frontend && pnpm exec prettier --write ${files.join(' ')}`,
  ],
  'backend/**/*.go': () => [
    'cd backend && make lint',
    'cd backend && make check-types',
  ],
}
