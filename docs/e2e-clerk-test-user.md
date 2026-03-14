# Creating the Clerk Test User for E2E Tests

The authenticated E2E tests use `clerk.signIn()` with the `emailAddress` parameter, which requires a test user to **already exist** in your Clerk instance. This document explains how to create that user.

## Test User Email

The tests expect a user with this email address:

```
gradebee+clerk_test@example.com
```

## Option A: Create via Clerk Dashboard

1. Go to [Clerk Dashboard](https://dashboard.clerk.com/) and select your GradeBee application.
2. Navigate to **Users** in the sidebar.
3. Click **Create user**.
4. Fill in:
   - **Email address:** `gradebee+clerk_test@example.com`
   - **First name:** (optional, e.g. `GradeBee`)
   - **Last name:** (optional, e.g. `Test`)
5. Click **Create user**.

## Option B: Create via Clerk Backend API

If you prefer automation or scripting:

```bash
curl -X POST "https://api.clerk.com/v1/users" \
  -H "Authorization: Bearer YOUR_CLERK_SECRET_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "email_address": ["gradebee+clerk_test@example.com"],
    "first_name": "GradeBee",
    "last_name": "Test"
  }'
```

Replace `YOUR_CLERK_SECRET_KEY` with your `CLERK_SECRET_KEY` from `.env`.

## Verification

After creating the user:

1. Ensure `CLERK_PUBLISHABLE_KEY` and `CLERK_SECRET_KEY` are set in `.env` (or `.env.local`).
2. Run the authenticated E2E tests:

   ```bash
   npm run test:e2e -- --project=authenticated
   ```

If the user does not exist, the test will fail with an error like: `No user found with email: gradebee+clerk_test@example.com`.
