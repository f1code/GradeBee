# Groups implemented as Clerk Organizations

**Status:** accepted

## Context & Decision

GradeBee is moving from per-user data to a shared model where **Levels** and their **Report Instructions** are owned by a **Group** (a school or subgroup) and authored by an Admin, consumed by Teachers. We implement a Group as a **Clerk Organization**, reusing Clerk's memberships, `admin`/`member` roles, invitations, and org-switcher. Domain rows (`levels`, `report_examples`) carry the Clerk org ID as `group_id`. Every Level, Class, and Report lives within exactly one Group — there is no personal/ungrouped path.

## Considered Options

- **Custom `groups` table** — full control and native hierarchy, but reinvents memberships, roles, invitations, and org-switching.
- **Personal-group-per-user** — rejected: Clerk's plan caps Organizations at ~100, so auto-provisioning one org per user does not scale.

## Consequences

- Clerk's ~100-Organization ceiling caps growth. Fine for MVP (one Group, two users), but exceeding ~100 Groups requires a paid Clerk tier or migrating to a custom groups table.
- Clerk Organizations are flat. The future **framework → school** hierarchy is a domain-layer concern (copying or parenting Levels across Groups), not a Clerk feature.
- Self-serve Group onboarding for brand-new signups is out of MVP scope; the initial Group and memberships are set up manually.
