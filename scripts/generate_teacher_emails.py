#!/usr/bin/env python3
"""Build teacher emails from an Excel roster.

Reads a sheet with a Teacher column ("First Last"), then writes a CSV with
every original column plus first_name, last_name, and email.

Email format: firstname.lastname@DOMAIN
  - lowercased
  - accents stripped (Élodie → elodie)
  - spaces inside a name part removed (Ana María López → ana.marialopez)
  - apostrophes and other punctuation dropped; hyphens kept

Usage:
    python3 scripts/generate_teacher_emails.py roster.xlsx --domain school.edu
    python3 scripts/generate_teacher_emails.py roster.xlsx --domain school.edu -o teachers.csv
    python3 scripts/generate_teacher_emails.py roster.xlsx --domain school.edu --sheet Staff
"""
from __future__ import annotations

import argparse
import csv
import sys
import unicodedata
import zipfile
import xml.etree.ElementTree as ET
from pathlib import Path

ADDED_COLUMNS = ("first_name", "last_name", "email")


def strip_accents(value: str) -> str:
    decomposed = unicodedata.normalize("NFD", value)
    return "".join(ch for ch in decomposed if unicodedata.category(ch) != "Mn")


def email_slug(value: str) -> str:
    """Lowercase, strip accents, keep letters/digits/hyphens, drop the rest."""
    cleaned = []
    for ch in strip_accents(value).lower():
        if ch.isalnum() or ch == "-":
            cleaned.append(ch)
    return "".join(cleaned)


def split_teacher(raw: str) -> tuple[str, str] | None:
    """Split 'First Last' on the first whitespace run. Returns None if < 2 parts."""
    parts = raw.split(None, 1)
    if len(parts) != 2:
        return None
    return parts[0], parts[1]


def build_email(first_name: str, last_name: str, domain: str) -> str:
    local = f"{email_slug(first_name)}.{email_slug(last_name)}"
    return f"{local}@{domain}"


def normalize_domain(domain: str) -> str:
    domain = domain.strip().lstrip("@").lower()
    if not domain or "." not in domain:
        raise SystemExit(f"invalid domain '{domain}' (expected e.g. school.edu)")
    return domain


_SSML = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
_REL = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
_NS = {"m": _SSML}


def _col_index(cell_ref: str) -> int:
    col = "".join(ch for ch in cell_ref if ch.isalpha())
    n = 0
    for ch in col:
        n = n * 26 + (ord(ch.upper()) - 64)
    return n - 1


def _shared_strings(zf: zipfile.ZipFile) -> list[str]:
    try:
        root = ET.fromstring(zf.read("xl/sharedStrings.xml"))
    except KeyError:
        return []
    strings: list[str] = []
    for si in root.findall("m:si", _NS):
        strings.append("".join(t.text or "" for t in si.findall(".//m:t", _NS)))
    return strings


def _sheet_target(zf: zipfile.ZipFile, sheet: str | None) -> str:
    wb = ET.fromstring(zf.read("xl/workbook.xml"))
    rels = ET.fromstring(zf.read("xl/_rels/workbook.xml.rels"))
    rid_to_target: dict[str, str] = {}
    for rel in rels:
        rid = rel.attrib.get("Id")
        target = rel.attrib.get("Target")
        if not rid or not target:
            continue
        if not target.startswith("xl/"):
            target = "xl/" + target.lstrip("/")
        rid_to_target[rid] = target

    sheets = wb.findall("m:sheets/m:sheet", _NS)
    if not sheets:
        raise SystemExit("workbook has no sheets")

    names = [sh.attrib.get("name", "") for sh in sheets]
    chosen = sheets[0]
    if sheet:
        for sh in sheets:
            if sh.attrib.get("name") == sheet:
                chosen = sh
                break
        else:
            raise SystemExit(f"sheet '{sheet}' not found; available: {', '.join(names)}")

    rid = chosen.attrib.get(f"{{{_REL}}}id")
    if not rid or rid not in rid_to_target:
        raise SystemExit("could not resolve worksheet path in the xlsx")
    return rid_to_target[rid]


def _cell_value(cell: ET.Element, shared: list[str]) -> str | None:
    cell_type = cell.attrib.get("t")
    if cell_type == "inlineStr":
        return "".join(t.text or "" for t in cell.findall(".//m:t", _NS)) or None
    value = cell.find("m:v", _NS)
    if value is None or value.text is None:
        return None
    if cell_type == "s":
        return shared[int(value.text)]
    return value.text


def read_excel(path: Path, sheet: str | None) -> tuple[list[str], list[list[object]]]:
    try:
        with zipfile.ZipFile(path) as zf:
            shared = _shared_strings(zf)
            root = ET.fromstring(zf.read(_sheet_target(zf, sheet)))
    except zipfile.BadZipFile:
        raise SystemExit(f"not a valid .xlsx file: {path}") from None

    grid: list[list[object]] = []
    for row in root.findall("m:sheetData/m:row", _NS):
        cells: dict[int, object] = {}
        max_col = -1
        for cell in row.findall("m:c", _NS):
            ref = cell.attrib.get("r", "A")
            idx = _col_index(ref)
            max_col = max(max_col, idx)
            cells[idx] = _cell_value(cell, shared)
        grid.append([cells.get(i) for i in range(max_col + 1)] if max_col >= 0 else [])

    if not grid:
        raise SystemExit("Excel sheet is empty")

    headers = ["" if h is None else str(h).strip() for h in grid[0]]
    data = [list(r) for r in grid[1:]]
    return headers, data


def find_teacher_col(headers: list[str], column: str) -> int:
    wanted = column.strip().lower()
    for i, h in enumerate(headers):
        if h.lower() == wanted:
            return i
    raise SystemExit(
        f"column '{column}' not found; available: {', '.join(h or '<empty>' for h in headers)}"
    )


def cell_text(value: object) -> str:
    if value is None:
        return ""
    return str(value).strip()


def transform(
    headers: list[str],
    data: list[list[object]],
    teacher_col: int,
    domain: str,
) -> tuple[list[str], list[list[str]]]:
    out_headers = list(headers)
    existing = {h.lower(): i for i, h in enumerate(out_headers)}
    added_idx: dict[str, int] = {}
    for name in ADDED_COLUMNS:
        if name in existing:
            added_idx[name] = existing[name]
        else:
            added_idx[name] = len(out_headers)
            out_headers.append(name)

    out_rows: list[list[str]] = []
    emails: dict[str, list[int]] = {}
    for row_num, raw in enumerate(data, start=2):
        row = [cell_text(raw[i]) if i < len(raw) else "" for i in range(len(out_headers))]
        # pad in case we appended columns
        while len(row) < len(out_headers):
            row.append("")

        teacher = cell_text(raw[teacher_col] if teacher_col < len(raw) else None)
        if not teacher:
            print(f"row {row_num}: empty Teacher, leaving name/email blank", file=sys.stderr)
        else:
            split = split_teacher(teacher)
            if split is None:
                print(
                    f"row {row_num}: Teacher '{teacher}' is not 'First Last', leaving name/email blank",
                    file=sys.stderr,
                )
            else:
                first, last = split
                email = build_email(first, last, domain)
                if not email_slug(first) or not email_slug(last):
                    print(
                        f"row {row_num}: Teacher '{teacher}' produced an empty email local-part, leaving email blank",
                        file=sys.stderr,
                    )
                    row[added_idx["first_name"]] = first
                    row[added_idx["last_name"]] = last
                    row[added_idx["email"]] = ""
                else:
                    row[added_idx["first_name"]] = first
                    row[added_idx["last_name"]] = last
                    row[added_idx["email"]] = email
                    emails.setdefault(email, []).append(row_num)

        out_rows.append(row)

    for email, rows in emails.items():
        if len(rows) > 1:
            print(f"duplicate email {email} on rows {', '.join(map(str, rows))}", file=sys.stderr)

    return out_headers, out_rows


def write_csv(path: Path, headers: list[str], rows: list[list[str]]) -> None:
    with path.open("w", newline="", encoding="utf-8-sig") as f:
        writer = csv.writer(f)
        writer.writerow(headers)
        writer.writerows(rows)


def default_output(input_path: Path) -> Path:
    return input_path.with_suffix(".csv")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("excel_path", type=Path, help="path to the .xlsx roster")
    parser.add_argument("--domain", required=True, help="email domain, e.g. school.edu")
    parser.add_argument("-o", "--output", type=Path, help="output CSV path (default: <excel>.csv)")
    parser.add_argument("--sheet", help="sheet name (default: first sheet)")
    parser.add_argument("--column", default="Teacher", help="name column to split (default: Teacher)")
    args = parser.parse_args()

    if not args.excel_path.is_file():
        raise SystemExit(f"file not found: {args.excel_path}")

    domain = normalize_domain(args.domain)
    headers, data = read_excel(args.excel_path, args.sheet)
    teacher_col = find_teacher_col(headers, args.column)
    out_headers, out_rows = transform(headers, data, teacher_col, domain)

    output = args.output or default_output(args.excel_path)
    write_csv(output, out_headers, out_rows)
    print(f"Wrote {len(out_rows)} row(s) to {output}")


if __name__ == "__main__":
    main()
