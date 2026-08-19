#!/usr/bin/env python3
"""Import classes and students into GradeBee's SQLite DB from a roster CSV.

Joins the roster to a Clerk import results CSV (email → clerk_user_id) and
writes levels, classes, and students directly to SQLite. No HTTP API.

Roster CSV columns (header required):
    Name,Courses,Schedule,email

    Name      student name (required)
    Courses   Level name. Created in --group-id if missing.
    Schedule  free-text meeting time; parsed into Day + Time slot
              (e.g. "Wednesday  16.30-17.30" → Wednesday / 16.30;
              "Tuesday  17.25-18.25,  Friday  16.30-17.30"
              → Tuesday / 17.25 & Fri 16.30). Day is the first weekday
              of the calendar week when several days appear. Extra
              Day/Teacher columns are ignored.
    email     teacher email; must appear in the results CSV

Results CSV columns (from import_clerk_users.py):
    email,clerk_user_id

A class is unique on (teacher clerk user id, level, day, time_slot). A
student is unique on (class, name) case-insensitive. If the same name
appears more than once in the same class, they are imported as "Name 1",
"Name 2", ...

Usage:
    python3 scripts/import_roster.py users-hannut.csv users-hannut.results.csv \
        --group-id org_abc123 --db data/gradebee.db
    python3 scripts/import_roster.py users-hannut.csv users-hannut.results.csv \
        --group-id org_abc123 --dry-run

Output: writes <roster>.roster-results.csv next to the roster file.
"""
from __future__ import annotations

import argparse
import csv
import os
import re
import sqlite3
import sys
from collections import defaultdict
from dataclasses import dataclass, field

VALID_DAYS = (
    "Monday",
    "Tuesday",
    "Wednesday",
    "Thursday",
    "Friday",
    "Saturday",
    "Sunday",
)

_TOKEN_TO_DAY = {
    "mon": "Monday",
    "monday": "Monday",
    "tue": "Tuesday",
    "tuesday": "Tuesday",
    "wed": "Wednesday",
    "wednesday": "Wednesday",
    "thu": "Thursday",
    "thursday": "Thursday",
    "fri": "Friday",
    "friday": "Friday",
    "sat": "Saturday",
    "saturday": "Saturday",
    "sun": "Sunday",
    "sunday": "Sunday",
}

_DAY_RE = re.compile(
    r"\b(monday|tuesday|wednesday|thursday|friday|saturday|sunday|"
    r"mon|tue|wed|thu|fri|sat|sun)\b",
    re.IGNORECASE,
)

_START_TIME_RE = re.compile(r"\d{1,2}[.:]\d{2}")


@dataclass
class RosterRow:
    name: str
    course: str
    schedule: str
    email: str


@dataclass
class PlannedStudent:
    name: str
    imported_name: str
    course: str
    schedule: str
    day: str
    time_slot: str
    email: str
    clerk_user_id: str


@dataclass
class ImportResult:
    levels_created: int = 0
    levels_skipped: int = 0
    classes_created: int = 0
    classes_skipped: int = 0
    students_created: int = 0
    students_skipped: int = 0
    errors: list[str] = field(default_factory=list)
    student_results: list[dict[str, str]] = field(default_factory=list)


def collapse_ws(value: str) -> str:
    return " ".join(value.split())


def _meeting_label(part: str) -> str:
    leftover = collapse_ws(_DAY_RE.sub(" ", part).strip(" @-&"))
    if not leftover:
        return ""
    m = _START_TIME_RE.search(leftover)
    return m.group(0) if m else leftover


def parse_schedule(raw: str) -> tuple[str, str] | None:
    """Split a roster Schedule string into (day, time_slot).

    Day is the earliest weekday in the calendar week (Monday first).
    Time slot is the start time only (end times are dropped). Extra
    meetings keep their weekday abbreviation: "17.25 & Fri 16.30".
    Returns None when no weekday can be identified.
    """
    text = collapse_ws(raw)
    if not text:
        return None
    by_day: dict[str, str] = {}
    for part in text.split(","):
        part = collapse_ws(part)
        for m in _DAY_RE.finditer(part):
            d = _TOKEN_TO_DAY[m.group(1).lower()]
            by_day.setdefault(d, _meeting_label(part))
    if not by_day:
        return None
    day = min(by_day, key=lambda d: VALID_DAYS.index(d))
    primary = by_day[day]
    extras = [
        f"{d[:3]} {by_day[d]}".strip() if by_day[d] else d[:3]
        for d in VALID_DAYS
        if d != day and d in by_day
    ]
    if not extras:
        return day, primary
    if primary:
        return day, primary + " & " + " & ".join(extras)
    return day, " & ".join(extras)


def _norm_email(email: str) -> str:
    return email.strip().lower()


def plan_students(rows: list[RosterRow], clerk_by_email: dict[str, str]) -> list[PlannedStudent]:
    clerk = {_norm_email(k): v.strip() for k, v in clerk_by_email.items() if v and v.strip()}
    groups: dict[tuple[str, str, str, str, str], list[int]] = defaultdict(list)
    parsed_by_row: list[tuple[str, str]] = []
    for i, row in enumerate(rows):
        parsed = parse_schedule(row.schedule)
        day, time_slot = parsed if parsed else ("", "")
        parsed_by_row.append((day, time_slot))
        key = (
            _norm_email(row.email),
            row.course.strip(),
            day,
            time_slot,
            row.name.strip().casefold(),
        )
        groups[key].append(i)

    planned: list[PlannedStudent | None] = [None] * len(rows)
    for idxs in groups.values():
        n = len(idxs)
        for seq, i in enumerate(idxs, start=1):
            row = rows[i]
            name = row.name.strip()
            email = _norm_email(row.email)
            day, time_slot = parsed_by_row[i]
            planned[i] = PlannedStudent(
                name=name,
                imported_name=f"{name} {seq}" if n > 1 else name,
                course=row.course.strip(),
                schedule=collapse_ws(row.schedule),
                day=day,
                time_slot=time_slot,
                email=email,
                clerk_user_id=clerk.get(email, ""),
            )
    return [p for p in planned if p is not None]


def _level_id(conn: sqlite3.Connection, group_id: str, name: str) -> int | None:
    row = conn.execute(
        "SELECT id FROM levels WHERE group_id = ? AND name = ? COLLATE NOCASE",
        (group_id, name),
    ).fetchone()
    return row[0] if row else None


def _class_id(
    conn: sqlite3.Connection, user_id: str, level_id: int, day: str, time_slot: str
) -> int | None:
    row = conn.execute(
        "SELECT id FROM classes WHERE user_id = ? AND level_id = ? AND day = ? AND time_slot = ?",
        (user_id, level_id, day, time_slot),
    ).fetchone()
    return row[0] if row else None


def _student_exists(conn: sqlite3.Connection, class_id: int, name: str) -> bool:
    row = conn.execute(
        "SELECT 1 FROM students WHERE class_id = ? AND name = ? COLLATE NOCASE",
        (class_id, name),
    ).fetchone()
    return row is not None


def _ensure_level(
    conn: sqlite3.Connection, group_id: str, name: str, dry_run: bool
) -> tuple[int | None, bool]:
    """Returns (level_id, created). level_id is None on dry-run create."""
    existing = _level_id(conn, group_id, name)
    if existing is not None:
        return existing, False
    if dry_run:
        return None, True
    cur = conn.execute("INSERT INTO levels (group_id, name) VALUES (?, ?)", (group_id, name))
    return cur.lastrowid, True


def _ensure_class(
    conn: sqlite3.Connection, user_id: str, level_id: int, day: str, time_slot: str, dry_run: bool
) -> tuple[int | None, bool]:
    existing = _class_id(conn, user_id, level_id, day, time_slot)
    if existing is not None:
        return existing, False
    if dry_run:
        return None, True
    pos_row = conn.execute(
        "SELECT COALESCE(MAX(position), 0) + 1 FROM classes WHERE user_id = ?",
        (user_id,),
    ).fetchone()
    cur = conn.execute(
        "INSERT INTO classes (user_id, level_id, day, time_slot, position) VALUES (?, ?, ?, ?, ?)",
        (user_id, level_id, day, time_slot, pos_row[0]),
    )
    return cur.lastrowid, True


def import_roster(
    conn: sqlite3.Connection,
    rows: list[RosterRow],
    clerk_by_email: dict[str, str],
    group_id: str,
    dry_run: bool,
) -> ImportResult:
    result = ImportResult()
    planned = plan_students(rows, clerk_by_email)
    created_levels: set[str] = set()
    created_classes: set[tuple[str, str, str, str]] = set()

    for p in planned:
        rec = {
            "email": p.email,
            "clerk_user_id": p.clerk_user_id,
            "level": p.course,
            "day": p.day,
            "time_slot": p.time_slot,
            "source_name": p.name,
            "imported_name": p.imported_name,
            "status": "",
            "error": "",
        }
        if not p.clerk_user_id:
            err = f"{p.email}: no clerk_user_id in results CSV"
            result.errors.append(err)
            rec["status"] = "error"
            rec["error"] = err
            result.student_results.append(rec)
            continue
        if not p.course:
            err = f"{p.email}: empty Courses for student {p.name!r}"
            result.errors.append(err)
            rec["status"] = "error"
            rec["error"] = err
            result.student_results.append(rec)
            continue
        if not p.day:
            err = f"{p.email}: cannot parse day from Schedule {p.schedule!r} for student {p.name!r}"
            result.errors.append(err)
            rec["status"] = "error"
            rec["error"] = err
            result.student_results.append(rec)
            continue
        if not p.imported_name:
            err = f"{p.email}: empty student Name"
            result.errors.append(err)
            rec["status"] = "error"
            rec["error"] = err
            result.student_results.append(rec)
            continue

        level_key = p.course.casefold()
        level_id, level_new = _ensure_level(conn, group_id, p.course, dry_run)
        if level_new and level_key not in created_levels:
            created_levels.add(level_key)
            result.levels_created += 1

        class_key = (p.clerk_user_id, p.course.casefold(), p.day, p.time_slot)
        if level_id is None and not dry_run:
            err = f"{p.email}: failed to resolve level {p.course!r}"
            result.errors.append(err)
            rec["status"] = "error"
            rec["error"] = err
            result.student_results.append(rec)
            continue

        class_new = False
        class_id: int | None = None
        if dry_run and level_id is None:
            class_new = class_key not in created_classes
        else:
            assert level_id is not None
            class_id, class_new = _ensure_class(
                conn, p.clerk_user_id, level_id, p.day, p.time_slot, dry_run
            )
        if class_new and class_key not in created_classes:
            created_classes.add(class_key)
            result.classes_created += 1

        exists = False
        if class_id is not None:
            exists = _student_exists(conn, class_id, p.imported_name)
        if exists:
            result.students_skipped += 1
            rec["status"] = "skipped"
        elif dry_run:
            result.students_created += 1
            rec["status"] = "created"
        else:
            assert class_id is not None
            conn.execute(
                "INSERT INTO students (class_id, name) VALUES (?, ?)",
                (class_id, p.imported_name),
            )
            result.students_created += 1
            rec["status"] = "created"
        result.student_results.append(rec)

    seen_levels = {p.course.casefold() for p in planned if p.clerk_user_id and p.course and p.day}
    result.levels_skipped = max(0, len(seen_levels) - result.levels_created)
    seen_classes = {
        (p.clerk_user_id, p.course.casefold(), p.day, p.time_slot)
        for p in planned
        if p.clerk_user_id and p.course and p.day
    }
    result.classes_skipped = max(0, len(seen_classes) - result.classes_created)
    return result


def read_roster(path: str) -> list[RosterRow]:
    rows: list[RosterRow] = []
    with open(path, newline="", encoding="utf-8-sig") as f:
        reader = csv.DictReader(f)
        required = {"Name", "Courses", "Schedule", "email"}
        headers = {h.strip() for h in (reader.fieldnames or [])}
        missing = required - headers
        if missing:
            raise SystemExit(f"roster CSV missing column(s): {', '.join(sorted(missing))}")
        for i, raw in enumerate(reader, start=2):
            name = (raw.get("Name") or "").strip()
            email = (raw.get("email") or "").strip()
            if not name:
                print(f"skipping line {i}: empty Name", file=sys.stderr)
                continue
            if not email:
                print(f"skipping line {i}: empty email", file=sys.stderr)
                continue
            rows.append(
                RosterRow(
                    name=name,
                    course=(raw.get("Courses") or "").strip(),
                    schedule=raw.get("Schedule") or "",
                    email=email,
                )
            )
    return rows


def read_clerk_ids(path: str) -> dict[str, str]:
    mapping: dict[str, str] = {}
    with open(path, newline="", encoding="utf-8-sig") as f:
        reader = csv.DictReader(f)
        required = {"email", "clerk_user_id"}
        headers = {h.strip() for h in (reader.fieldnames or [])}
        missing = required - headers
        if missing:
            raise SystemExit(f"results CSV missing column(s): {', '.join(sorted(missing))}")
        for raw in reader:
            email = _norm_email(raw.get("email") or "")
            user_id = (raw.get("clerk_user_id") or "").strip()
            if email and user_id:
                mapping[email] = user_id
    return mapping


def write_results(path: str, result: ImportResult) -> None:
    with open(path, "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(
            f,
            fieldnames=[
                "email",
                "clerk_user_id",
                "level",
                "day",
                "time_slot",
                "source_name",
                "imported_name",
                "status",
                "error",
            ],
        )
        writer.writeheader()
        writer.writerows(result.student_results)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("roster_csv", help="roster CSV (Name,Courses,Schedule,email)")
    parser.add_argument("results_csv", help="Clerk import results CSV (email,clerk_user_id)")
    parser.add_argument("--group-id", required=True, help="Clerk Organization ID (levels.group_id)")
    parser.add_argument(
        "--db",
        default=os.environ.get("DB_PATH") or "data/gradebee.db",
        help="SQLite database path (default: $DB_PATH or data/gradebee.db)",
    )
    parser.add_argument("--dry-run", action="store_true", help="plan the import, write no rows")
    args = parser.parse_args()

    if not os.path.isfile(args.db):
        raise SystemExit(f"database not found: {args.db}")

    rows = read_roster(args.roster_csv)
    if not rows:
        raise SystemExit("no usable rows found in roster CSV")
    clerk = read_clerk_ids(args.results_csv)
    print(f"Loaded {len(rows)} roster row(s), {len(clerk)} clerk user id(s)")

    conn = sqlite3.connect(args.db)
    try:
        conn.execute("PRAGMA foreign_keys = ON")
        result = import_roster(conn, rows, clerk, args.group_id, dry_run=args.dry_run)
        if args.dry_run:
            conn.rollback()
        else:
            conn.commit()
    finally:
        conn.close()

    prefix = "DRY RUN: would create" if args.dry_run else "created"
    print(
        f"{prefix} {result.levels_created} level(s), "
        f"{result.classes_created} class(es), "
        f"{result.students_created} student(s); "
        f"skipped {result.students_skipped} student(s)"
    )
    if result.errors:
        print(f"{len(result.errors)} row(s) failed:", file=sys.stderr)
        for err in result.errors:
            print(f"  {err}", file=sys.stderr)

    out_path = args.roster_csv.rsplit(".", 1)[0] + ".roster-results.csv"
    write_results(out_path, result)
    print(f"Results written to {out_path}")
    if result.errors:
        sys.exit(1)


if __name__ == "__main__":
    main()
