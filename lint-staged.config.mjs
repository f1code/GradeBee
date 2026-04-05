export default {
  'frontend/src/**/*.{ts,tsx}': (files) => [
    `cd frontend && npx eslint --fix ${files.join(' ')}`,
    `cd frontend && npx prettier --write ${files.join(' ')}`,
  ],
  'frontend/src/**/*.css': (files) => [
    `cd frontend && npx prettier --write ${files.join(' ')}`,
  ],
  'backend/**/*.go': () => [
    'cd backend && make lint',
    'cd backend && make check-types',
  ],
}
