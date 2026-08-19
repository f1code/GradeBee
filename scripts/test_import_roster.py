#!/usr/bin/env python3
"""Tests for scripts/import_roster.py."""
from __future__ import annotations

import sqlite3
import tempfile
import unittest
from pathlib import Path

import import_roster as ir


def _schema(conn: sqlite3.Connection) -> None:
    conn.executescript(
        """
        CREATE TABLE levels (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          group_id TEXT NOT NULL,
          name TEXT NOT NULL COLLATE NOCASE,
          report_instructions TEXT NOT NULL DEFAULT '',
          created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
          UNIQUE (group_id, name)
        );
        CREATE TABLE classes (
          id INTEGER PRIMARY KEY,
          user_id TEXT NOT NULL,
          level_id INTEGER NOT NULL REFERENCES levels(id) ON DELETE RESTRICT,
          day TEXT NOT NULL CHECK (day IN ('Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday')),
          time_slot TEXT NOT NULL DEFAULT '',
          position INTEGER NOT NULL DEFAULT 0,
          created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
          UNIQUE (user_id, level_id, day, time_slot)
        );
        CREATE TABLE students (
          id INTEGER PRIMARY KEY,
          class_id INTEGER NOT NULL REFERENCES classes(id) ON DELETE CASCADE,
          name TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
        );
        CREATE UNIQUE INDEX idx_students_class_name_nocase
          ON students(class_id, name COLLATE NOCASE);
        """
    )


class CollapseWsTest(unittest.TestCase):
    def test_collapses_internal_whitespace(self) -> None:
        self.assertEqual(ir.collapse_ws("Wednesday  16.30-17.30"), "Wednesday 16.30-17.30")


class ParseScheduleTest(unittest.TestCase):
    def test_full_weekday_and_range(self) -> None:
        self.assertEqual(ir.parse_schedule("Wednesday  16.30-17.30"), ("Wednesday", "16.30"))

    def test_abbreviated_weekday(self) -> None:
        self.assertEqual(ir.parse_schedule("Wed  14.10"), ("Wednesday", "14.10"))

    def test_bare_abbreviation_has_empty_time_slot(self) -> None:
        self.assertEqual(ir.parse_schedule("Wed"), ("Wednesday", ""))

    def test_day_token_stripped_from_surrounding_label(self) -> None:
        self.assertEqual(ir.parse_schedule("Group 1 Wednesday"), ("Wednesday", "Group 1"))

    def test_twice_weekly_keeps_other_day_and_start_times(self) -> None:
        self.assertEqual(
            ir.parse_schedule("Tuesday  17.25-18.25,  Friday  16.30-17.30"),
            ("Tuesday", "17.25 & Fri 16.30"),
        )

    def test_picks_first_weekday_of_calendar_week(self) -> None:
        self.assertEqual(
            ir.parse_schedule("Thursday  17.25-18.55,  Tuesday  17.25-18.55"),
            ("Tuesday", "17.25 & Thu 17.25"),
        )

    def test_unparseable_returns_none(self) -> None:
        self.assertIsNone(ir.parse_schedule(""))
        self.assertIsNone(ir.parse_schedule("Period 1"))


class NumberedNamesTest(unittest.TestCase):
    def test_unique_name_unchanged(self) -> None:
        rows = [
            ir.RosterRow(name="Arthur", course="Oliver", schedule="Wed", email="a@x.com"),
        ]
        planned = ir.plan_students(rows, {"a@x.com": "user_1"})
        self.assertEqual([p.imported_name for p in planned], ["Arthur"])
        self.assertEqual(planned[0].day, "Wednesday")
        self.assertEqual(planned[0].time_slot, "")

    def test_duplicates_in_same_class_are_numbered(self) -> None:
        rows = [
            ir.RosterRow(name="Arthur", course="Oliver", schedule="Wed", email="a@x.com"),
            ir.RosterRow(name="Arthur", course="Oliver", schedule="Wed", email="a@x.com"),
        ]
        planned = ir.plan_students(rows, {"a@x.com": "user_1"})
        self.assertEqual([p.imported_name for p in planned], ["Arthur 1", "Arthur 2"])

    def test_same_name_in_different_classes_stays_plain(self) -> None:
        rows = [
            ir.RosterRow(name="Arthur", course="Oliver", schedule="Wed", email="a@x.com"),
            ir.RosterRow(name="Arthur", course="Emma", schedule="Fri", email="a@x.com"),
        ]
        planned = ir.plan_students(rows, {"a@x.com": "user_1"})
        self.assertEqual([p.imported_name for p in planned], ["Arthur", "Arthur"])

    def test_case_insensitive_duplicates_are_numbered(self) -> None:
        rows = [
            ir.RosterRow(name="Arthur", course="Oliver", schedule="Wed", email="a@x.com"),
            ir.RosterRow(name="arthur", course="Oliver", schedule="Wed", email="a@x.com"),
        ]
        planned = ir.plan_students(rows, {"a@x.com": "user_1"})
        self.assertEqual([p.imported_name for p in planned], ["Arthur 1", "arthur 2"])

    def test_abbrev_and_full_weekday_are_the_same_class(self) -> None:
        rows = [
            ir.RosterRow(name="Arthur", course="Oliver", schedule="Wed  14.10", email="a@x.com"),
            ir.RosterRow(name="Arthur", course="Oliver", schedule="Wednesday  14.10", email="a@x.com"),
        ]
        planned = ir.plan_students(rows, {"a@x.com": "user_1"})
        self.assertEqual([p.imported_name for p in planned], ["Arthur 1", "Arthur 2"])
        self.assertEqual({(p.day, p.time_slot) for p in planned}, {("Wednesday", "14.10")})


class ImportTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.NamedTemporaryFile(suffix=".db", delete=False)
        self.tmp.close()
        self.db_path = self.tmp.name
        self.conn = sqlite3.connect(self.db_path)
        _schema(self.conn)
        self.conn.commit()

    def tearDown(self) -> None:
        self.conn.close()
        Path(self.db_path).unlink(missing_ok=True)

    def test_creates_level_class_and_students(self) -> None:
        rows = [
            ir.RosterRow(name="Lina", course="Oliver", schedule="Wed  14.10", email="lea@x.com"),
            ir.RosterRow(name="Arthur", course="Oliver", schedule="Wed  14.10", email="lea@x.com"),
            ir.RosterRow(name="Arthur", course="Oliver", schedule="Wed  14.10", email="lea@x.com"),
        ]
        result = ir.import_roster(
            self.conn,
            rows,
            clerk_by_email={"lea@x.com": "user_lea"},
            group_id="org_hannut",
            dry_run=False,
        )
        self.conn.commit()
        levels = self.conn.execute("SELECT name FROM levels WHERE group_id = ?", ("org_hannut",)).fetchall()
        self.assertEqual([r[0] for r in levels], ["Oliver"])
        classes = self.conn.execute("SELECT user_id, day, time_slot FROM classes").fetchall()
        self.assertEqual(classes, [("user_lea", "Wednesday", "14.10")])
        names = [r[0] for r in self.conn.execute("SELECT name FROM students ORDER BY name")]
        self.assertEqual(names, ["Arthur 1", "Arthur 2", "Lina"])
        self.assertEqual(result.levels_created, 1)
        self.assertEqual(result.classes_created, 1)
        self.assertEqual(result.students_created, 3)

    def test_same_level_and_day_different_time_slots_are_two_classes(self) -> None:
        rows = [
            ir.RosterRow(name="Louise", course="Oliver", schedule="Wednesday  14.10-15.10", email="lea@x.com"),
            ir.RosterRow(name="Ayaan", course="Oliver", schedule="Wednesday  16.30-17.30", email="lea@x.com"),
        ]
        ir.import_roster(self.conn, rows, {"lea@x.com": "user_lea"}, "org_hannut", dry_run=False)
        self.conn.commit()
        classes = self.conn.execute(
            "SELECT day, time_slot FROM classes ORDER BY time_slot"
        ).fetchall()
        self.assertEqual(classes, [("Wednesday", "14.10"), ("Wednesday", "16.30")])

    def test_idempotent_second_run_creates_nothing(self) -> None:
        rows = [
            ir.RosterRow(name="Lina", course="Oliver", schedule="Wed", email="lea@x.com"),
            ir.RosterRow(name="Arthur", course="Oliver", schedule="Wed", email="lea@x.com"),
            ir.RosterRow(name="Arthur", course="Oliver", schedule="Wed", email="lea@x.com"),
        ]
        clerk = {"lea@x.com": "user_lea"}
        ir.import_roster(self.conn, rows, clerk, "org_hannut", dry_run=False)
        self.conn.commit()
        result = ir.import_roster(self.conn, rows, clerk, "org_hannut", dry_run=False)
        self.conn.commit()
        self.assertEqual(result.levels_created, 0)
        self.assertEqual(result.classes_created, 0)
        self.assertEqual(result.students_created, 0)
        self.assertEqual(result.students_skipped, 3)
        n = self.conn.execute("SELECT COUNT(*) FROM students").fetchone()[0]
        self.assertEqual(n, 3)

    def test_dry_run_writes_nothing(self) -> None:
        rows = [ir.RosterRow(name="Lina", course="Oliver", schedule="Wed", email="lea@x.com")]
        ir.import_roster(self.conn, rows, {"lea@x.com": "user_lea"}, "org_hannut", dry_run=True)
        self.conn.commit()
        self.assertEqual(self.conn.execute("SELECT COUNT(*) FROM levels").fetchone()[0], 0)
        self.assertEqual(self.conn.execute("SELECT COUNT(*) FROM classes").fetchone()[0], 0)
        self.assertEqual(self.conn.execute("SELECT COUNT(*) FROM students").fetchone()[0], 0)

    def test_missing_clerk_id_skips_row(self) -> None:
        rows = [ir.RosterRow(name="Lina", course="Oliver", schedule="Wed", email="unknown@x.com")]
        result = ir.import_roster(self.conn, rows, {}, "org_hannut", dry_run=False)
        self.assertEqual(result.students_created, 0)
        self.assertEqual(len(result.errors), 1)
        self.assertIn("unknown@x.com", result.errors[0])

    def test_unparseable_schedule_skips_row(self) -> None:
        rows = [ir.RosterRow(name="Lina", course="Oliver", schedule="Period 1", email="lea@x.com")]
        result = ir.import_roster(self.conn, rows, {"lea@x.com": "user_lea"}, "org_hannut", dry_run=False)
        self.assertEqual(result.students_created, 0)
        self.assertEqual(len(result.errors), 1)
        self.assertIn("Period 1", result.errors[0])
        self.assertEqual(self.conn.execute("SELECT COUNT(*) FROM classes").fetchone()[0], 0)


if __name__ == "__main__":
    unittest.main()
