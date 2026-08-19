#!/usr/bin/env python3
"""Bulk-create Clerk users from a CSV and (optionally) add them to a Clerk
Organization (= a GradeBee Group, see docs/adr/0001-groups-as-clerk-organizations.md).

Users are created directly via the Clerk Backend API (not invited), so the
script gets back a real Clerk user ID immediately -- needed to seed classes /
memberships for each user in the same run.

CSV columns (header required):
    email,first_name,last_name,org_role

    email        required. Duplicate emails (case-insensitive) are collapsed
                 to one user; first occurrence wins. Later rows may fill in
                 a missing name, and "admin" wins over "member" if roles
                 conflict.
    first_name   optional
    last_name    optional
    org_role     optional; "admin" or "member" (default: member).
                 Ignored if --org-id is not given.

Usage:
    export CLERK_SECRET_KEY=sk_test_xxx
    python3 scripts/import_clerk_users.py users.csv --org-id org_abc123
    python3 scripts/import_clerk_users.py users.csv --dry-run

Each created user gets a random password (Clerk requires one for direct
creation). Passwords are written to the output CSV so they can be relayed to
users out-of-band; this script does not email anyone.

Output: writes <input>.results.csv next to the input file with columns
    email,clerk_user_id,password,org_status,error
"""
from __future__ import annotations

import argparse
import csv
import secrets
import string
import sys
import time
import urllib.error
import urllib.request
import json
import os
from dataclasses import dataclass

CLERK_API_BASE = "https://api.clerk.com/v1"
# Cloudflare in front of api.clerk.com rejects Python-urllib's default UA (error 1010).
USER_AGENT = "GradeBee-ClerkImport/1.0"


@dataclass
class Row:
    email: str
    first_name: str = ""
    last_name: str = ""
    org_role: str = "member"


@dataclass
class Result:
    email: str
    clerk_user_id: str = ""
    password: str = ""
    org_status: str = ""
    error: str = ""


def gen_password(length: int = 20) -> str:
    alphabet = string.ascii_letters + string.digits + "!@#$%^&*"
    return "".join(secrets.choice(alphabet) for _ in range(length))


def clerk_request(secret_key: str, method: str, path: str, body: dict | None = None) -> dict:
    url = f"{CLERK_API_BASE}{path}"
    data = json.dumps(body).encode("utf-8") if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Authorization", f"Bearer {secret_key}")
    req.add_header("Content-Type", "application/json")
    req.add_header("User-Agent", USER_AGENT)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read()
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        detail = e.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"{method} {path} -> HTTP {e.code}: {detail}") from None


def read_rows(csv_path: str) -> list[Row]:
    rows: list[Row] = []
    with open(csv_path, newline="", encoding="utf-8-sig") as f:
        reader = csv.DictReader(f)
        required = {"email"}
        missing = required - set(h.strip() for h in (reader.fieldnames or []))
        if missing:
            raise SystemExit(f"CSV missing required column(s): {', '.join(sorted(missing))}")
        for i, raw in enumerate(reader, start=2):  # header is line 1
            email = (raw.get("email") or "").strip()
            if not email:
                print(f"skipping line {i}: empty email", file=sys.stderr)
                continue
            role = (raw.get("org_role") or "member").strip().lower() or "member"
            if role not in ("admin", "member"):
                raise SystemExit(f"line {i}: invalid org_role '{role}' (must be admin or member)")
            rows.append(
                Row(
                    email=email,
                    first_name=(raw.get("first_name") or "").strip(),
                    last_name=(raw.get("last_name") or "").strip(),
                    org_role=role,
                )
            )
    return dedupe_rows(rows)


def _email_key(email: str) -> str:
    return email.strip().lower()


def dedupe_rows(rows: list[Row]) -> list[Row]:
    """Collapse to one row per email (case-insensitive). First occurrence wins.

    Missing names are filled in from later duplicates. If any duplicate is
    org_role=admin, the kept row becomes admin.
    """
    kept: dict[str, Row] = {}
    order: list[str] = []
    skipped_by_email: dict[str, int] = {}
    for row in rows:
        key = _email_key(row.email)
        existing = kept.get(key)
        if existing is None:
            kept[key] = row
            order.append(key)
            continue
        skipped_by_email[existing.email] = skipped_by_email.get(existing.email, 0) + 1
        if row.first_name and existing.first_name and row.first_name != existing.first_name:
            print(
                f"duplicate {row.email}: keeping first_name '{existing.first_name}', ignoring '{row.first_name}'",
                file=sys.stderr,
            )
        elif row.first_name and not existing.first_name:
            existing.first_name = row.first_name
        if row.last_name and existing.last_name and row.last_name != existing.last_name:
            print(
                f"duplicate {row.email}: keeping last_name '{existing.last_name}', ignoring '{row.last_name}'",
                file=sys.stderr,
            )
        elif row.last_name and not existing.last_name:
            existing.last_name = row.last_name
        if row.org_role == "admin" and existing.org_role != "admin":
            existing.org_role = "admin"
    skipped = sum(skipped_by_email.values())
    if skipped:
        print(
            f"deduplicated {skipped} duplicate row(s); {len(order)} unique user(s) remain",
            file=sys.stderr,
        )
        for email, n in skipped_by_email.items():
            print(f"  {email}: {n} duplicate(s) skipped", file=sys.stderr)
    return [kept[k] for k in order]


def create_user(secret_key: str, row: Row) -> tuple[str, str]:
    """Returns (clerk_user_id, password)."""
    password = gen_password()
    body = {
        "email_address": [row.email],
        "password": password,
        "skip_password_checks": True,
    }
    if row.first_name:
        body["first_name"] = row.first_name
    if row.last_name:
        body["last_name"] = row.last_name
    created = clerk_request(secret_key, "POST", "/users", body)
    return created["id"], password


def add_to_org(secret_key: str, org_id: str, user_id: str, role: str) -> None:
    # Clerk organization roles are namespaced, e.g. "org:admin" / "org:member".
    clerk_role = role if role.startswith("org:") else f"org:{role}"
    clerk_request(
        secret_key,
        "POST",
        f"/organizations/{org_id}/memberships",
        {"user_id": user_id, "role": clerk_role},
    )


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("csv_path", help="path to input CSV (email,first_name,last_name,org_role)")
    parser.add_argument("--org-id", help="Clerk Organization ID to add every created user to")
    parser.add_argument("--dry-run", action="store_true", help="parse and validate the CSV, make no API calls")
    parser.add_argument(
        "--delay", type=float, default=0.3, help="seconds to sleep between users, to stay under rate limits (default 0.3)"
    )
    args = parser.parse_args()

    secret_key = os.environ.get("CLERK_SECRET_KEY") or ""
    if not args.dry_run and not secret_key:
        raise SystemExit("CLERK_SECRET_KEY environment variable is not set")

    rows = read_rows(args.csv_path)
    if not rows:
        raise SystemExit("no usable rows found in CSV")

    print(f"Loaded {len(rows)} user(s) from {args.csv_path}")
    if args.dry_run:
        for r in rows:
            print(f"  DRY RUN: would create {r.email} ({r.first_name} {r.last_name}) role={r.org_role}")
        return

    results: list[Result] = []
    for i, row in enumerate(rows, start=1):
        result = Result(email=row.email)
        print(f"[{i}/{len(rows)}] creating {row.email} ...", end=" ", flush=True)
        try:
            user_id, password = create_user(secret_key, row)
            result.clerk_user_id = user_id
            result.password = password
            print(f"user_id={user_id}", end=" ")
        except RuntimeError as e:
            result.error = str(e)
            results.append(result)
            print(f"FAILED: {e}")
            time.sleep(args.delay)
            continue

        if args.org_id:
            try:
                add_to_org(secret_key, args.org_id, user_id, row.org_role)
                result.org_status = f"added as {row.org_role}"
                print(f"org={result.org_status}")
            except RuntimeError as e:
                result.org_status = "FAILED"
                result.error = str(e)
                print(f"org FAILED: {e}")
        else:
            print()

        results.append(result)
        time.sleep(args.delay)

    out_path = args.csv_path.rsplit(".", 1)[0] + ".results.csv"
    with open(out_path, "w", newline="", encoding="utf-8") as f:
        writer = csv.writer(f)
        writer.writerow(["email", "clerk_user_id", "password", "org_status", "error"])
        for r in results:
            writer.writerow([r.email, r.clerk_user_id, r.password, r.org_status, r.error])

    failed = sum(1 for r in results if r.error and not r.clerk_user_id)
    print(f"\nDone. {len(results) - failed}/{len(results)} users created. Results written to {out_path}")
    if failed:
        print(f"{failed} user(s) failed to create -- see {out_path}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
