---
name: reviewing-changes
description: >
  Review a coherent set of changes (a branch, a diff, or a worktree)
  against an approved plan and the project's conventions, and emit an
  explicit verdict in a fixed output format. Use when a branch or
  patchset is ready for a structured check before merging.
allowed-tools:
  - Bash(git *)
  - Bash(rg *)
  - Bash(go *)
  - Bash(golangci-lint *)
  - Bash(make *)
  - Bash(npm *)
  - Bash(pnpm *)
  - Bash(yarn *)
---

# Reviewing Changes

A pure content skill: a checklist and a required output format for reviewing
a set of changes against a plan. This skill performs no orchestration. The
caller (a workflow, a sub-agent harness, or a human) decides *who* runs the
review and *where*; this skill only describes *how*.

## When to Use

Anytime a coherent set of changes is ready and needs a structured check
against a plan: pre-merge review of a feature branch, sanity check on a
worktree before handoff, or post-hoc audit of a diff.

**For an independent review, invoke this skill from a fresh sub-agent
context.** Loading it in the same agent that produced the changes will not
give you independence — that agent has already rationalised its own work.
The kanban-based-development workflow handles this by spawning a fresh
sub-agent and having *it* load this skill.

This skill is plain content and is portable across hosts that implement the
Anthropic Agent Skills format (e.g. OpenCode, Claude Code).

## Inputs

The reviewer needs the following before starting. If invoked by a workflow,
the workflow supplies these in the prompt; if invoked directly by a user,
gather them from the user or context.

- **Task ID** (optional, kanban-specific) — used to read the task body for
  context.
- **Plan file path** — the approved plan the changes are meant to implement.
- **Branch / refs** — branch name, or explicit base + head refs, so
  `git diff <base>...<head>` and `git log <base>..<head>` resolve.
- **Worktree path / cwd** — the directory the review runs from. Must be a
  checkout of the head ref so `git diff` and file reads resolve correctly.

## Process

1. Read the plan file end-to-end.
2. Read the task body, if a task ID was provided.
3. Read the project convention docs that apply: root `AGENTS.md` and any
   nested ones, `backend/ARCHITECTURE.md`, `frontend/DESIGN.md` (when
   relevant).
4. Run `git diff <base>...<head>` and `git log <base>..<head>` to see the
   full set of changes and the commit messages.
5. Spot-check touched files for surrounding context — diffs alone often
   miss invariants in the surrounding code.

## Independence Rule

The verdict must be grounded in the actual diff and the plan. Do not trust
the implementer's summary, commit messages, or task body claims about what
was done — verify them against the diff. If a claim in the task body
contradicts the diff, that is itself a finding.

## Checklist

- **Plan adherence** — diff matches the approved plan; no scope creep; open
  questions in the plan are resolved or explicitly deferred.
- **Project conventions** — follows the rules in `AGENTS.md` (root and
  nested); backend code follows `backend/ARCHITECTURE.md` patterns; frontend
  code follows `frontend/DESIGN.md` tokens and patterns.
- **Tests** — new behaviour has tests; bug fixes have a regression test;
  tests actually exercise the change (not just import the code).
- **Lint / build** — verify the implementer ran the project's lint and test
  commands and they passed. Check the task body and commit messages for
  evidence. **If the evidence is absent, run them yourself** before
  emitting a verdict.
- **Docs** — per the project's "definition of done" table in `AGENTS.md`,
  confirm the authoritative docs are updated (e.g. `backend/ARCHITECTURE.md`
  when handlers/repos/migrations change; `frontend/DESIGN.md` when design
  tokens change; `.env.example` when env vars change).
- **Correctness / security smells** — error handling, input validation,
  concurrency, secret handling, obvious foot-guns.
- **Self-consistency** — no leftover debug prints, commented-out code, or
  TODOs introduced without rationale.

## Required Output Format

Emit exactly one markdown block in this shape, and nothing else:

```
## Review (verdict: APPROVE | CHANGES_REQUESTED)
Reviewed: <branch> against <base>
- [blocking] <file>:<line> — <issue>
- [blocking] <issue>
- [nit] <issue>
```

Rules:

- The verdict is `APPROVE` if and only if there are zero `[blocking]`
  findings.
- `[nit]` findings are non-blocking; they are recorded for the human or
  agent to address at their discretion.
- If there are no findings at all, omit the bullet lines but keep the
  verdict and `Reviewed:` lines.
- File references should use `<file>:<line>` so the caller can navigate
  directly to the issue.

## Reviewer Prompt (verbatim block for callers to pass to a sub-agent)

Callers spawning a fresh-context sub-agent can paste the block below as the
sub-agent's prompt, after filling in the placeholders. The sub-agent should
load this skill and follow it.

```
Load the `reviewing-changes` skill and follow its instructions.

Inputs:
- Task ID: <ID or "none">
- Plan: <path/to/plan.md>
- Base ref: <e.g. main>
- Head ref / branch: <e.g. task/123-foo>
- Worktree path: <absolute path; this is your cwd>

Process: read plan → read task body (if given) → read project convention
docs (`AGENTS.md` at root + nested, `backend/ARCHITECTURE.md`,
`frontend/DESIGN.md` when relevant) → `git diff <base>...<head>` and
`git log <base>..<head>` → spot-check touched files for surrounding
context.

Independence rule: verdict must be grounded in the actual diff and plan.
Do not trust the implementer's summary or task body claims about what
was done — verify against the diff.

Checklist:
- Plan adherence — diff matches approved plan; no scope creep; open
  questions resolved.
- Project conventions — AGENTS.md rules; backend ARCHITECTURE.md;
  frontend DESIGN.md.
- Tests — new behaviour has tests; bugs have a regression test; tests
  actually exercise the change.
- Lint/build — verify the implementer ran the project's lint/test
  commands and they passed (check task body / commit messages for
  evidence; if absent, run them yourself).
- Docs — per the project's "definition of done" table in AGENTS.md,
  confirm authoritative docs are updated.
- Correctness/security smells — error handling, input validation,
  concurrency, secrets, obvious foot-guns.
- Self-consistency — no leftover debug prints, commented-out code, or
  TODOs introduced without rationale.

Return exactly one markdown block in this format and nothing else:

## Review (verdict: APPROVE | CHANGES_REQUESTED)
Reviewed: <branch> against <base>
- [blocking] <file>:<line> — <issue>
- [blocking] <issue>
- [nit] <issue>

Verdict is APPROVE iff there are zero [blocking] findings. Nits are
non-blocking and recorded for later attention.
```

## How to Interpret the Result (caller instructions)

- The verdict is the token after `verdict:` on the first line: `APPROVE`
  or `CHANGES_REQUESTED`.
- The entire markdown block is the artefact to record. Callers integrating
  with kanban (or any task tracker) should append the whole block to the
  task body so successive reviews accumulate as an audit trail rather than
  overwriting one another.
- `[nit]` findings do not block merge but should not be silently dropped —
  the caller decides whether to address them now or file follow-up work.
