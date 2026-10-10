#!/usr/bin/env python3
"""Generate H.264 CABAC (m, n) context-initialization tables for ctxIdx 0-459.

Row-oriented Tables 9-12 to 9-17 are transcribed below; column-oriented Tables
9-18 to 9-24 are parsed from `pdftotext -layout` output of the standard in docs/.
Emits Go, Python, or Rust source on stdout. Development-time only.
"""

import argparse
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
PDF = ROOT / "docs" / "T-REC-H.264-202606-I!!PDF-E.pdf"
COUNT = 460

# ctxIdx 0-10 (Table 9-12) and 60-69 (Table 9-17) apply to every slice type.
ALL_SLICES = {
    0: (20, -15), 1: (2, 54), 2: (3, 74), 3: (20, -15), 4: (2, 54), 5: (3, 74),
    6: (-28, 127), 7: (-23, 104), 8: (-6, 53), 9: (-1, 54), 10: (7, 51),
    60: (0, 41), 61: (0, 63), 62: (0, 63), 63: (0, 63), 64: (-9, 83),
    65: (4, 86), 66: (0, 97), 67: (-7, 72), 68: (13, 41), 69: (3, 62),
}
# Tables 9-13 to 9-16: ctxIdx 11-59 per cabac_init_idc 0-2.
PB_ONLY = [
    [(23, 33), (23, 2), (21, 0), (1, 9), (0, 49), (-37, 118), (5, 57), (-13, 78), (-11, 65),
     (1, 62), (12, 49), (-4, 73), (17, 50),
     (18, 64), (9, 43), (29, 0), (26, 67), (16, 90), (9, 104), (-46, 127), (-20, 104),
     (1, 67), (-13, 78), (-11, 65), (1, 62), (-6, 86), (-17, 95), (-6, 61), (9, 45),
     (-3, 69), (-6, 81), (-11, 96), (6, 55), (7, 67), (-5, 86), (2, 88), (0, 58), (-3, 76),
     (-10, 94), (5, 54), (4, 69), (-3, 81), (0, 88),
     (-7, 67), (-5, 74), (-4, 74), (-5, 80), (-7, 72), (1, 58)],
    [(22, 25), (34, 0), (16, 0), (-2, 9), (4, 41), (-29, 118), (2, 65), (-6, 71), (-13, 79),
     (5, 52), (9, 50), (-3, 70), (10, 54),
     (26, 34), (19, 22), (40, 0), (57, 2), (41, 36), (26, 69), (-45, 127), (-15, 101),
     (-4, 76), (-6, 71), (-13, 79), (5, 52), (6, 69), (-13, 90), (0, 52), (8, 43),
     (-2, 69), (-5, 82), (-10, 96), (2, 59), (2, 75), (-3, 87), (-3, 100), (1, 56), (-3, 74),
     (-6, 85), (0, 59), (-3, 81), (-7, 86), (-5, 95),
     (-1, 66), (-1, 77), (1, 70), (-2, 86), (-5, 72), (0, 61)],
    [(29, 16), (25, 0), (14, 0), (-10, 51), (-3, 62), (-27, 99), (26, 16), (-4, 85),
     (-24, 102), (5, 57), (6, 57), (-17, 73), (14, 57),
     (20, 40), (20, 10), (29, 0), (54, 0), (37, 42), (12, 97), (-32, 127), (-22, 117),
     (-2, 74), (-4, 85), (-24, 102), (5, 57), (-6, 93), (-14, 88), (-6, 44), (4, 55),
     (-11, 89), (-15, 103), (-21, 116), (19, 57), (20, 58), (4, 84), (6, 96), (1, 63),
     (-5, 85), (-13, 106), (5, 63), (6, 75), (-3, 90), (-1, 101),
     (3, 55), (-4, 79), (-2, 75), (-12, 97), (-7, 50), (1, 60)],
]
# Table 9-16: ctxIdx 399-401 for I slices, then cabac_init_idc 0-2.
TRANSFORM_8X8 = [
    [(31, 21), (31, 31), (25, 50)],
    [(12, 40), (11, 51), (14, 59)],
    [(25, 32), (21, 49), (21, 54)],
    [(21, 33), (19, 50), (17, 61)],
]
ROW = re.compile(r"^\s*(-?\d+(?:\s+-?\d+)*)\s*$")


def parse_columns(text: str) -> dict[int, list[tuple[int, int]]]:
    start = text.index("Table 9-18 – Values of variables m and n for ctxIdx from 70 to 104\n")
    end = text.index("Table 9-25 – Values of variables m and n for ctxIdx from 460 to 483\n")
    rows: dict[int, list[tuple[int, int]]] = {}
    for line in text[start:end].replace("−", "-").splitlines():
        match = ROW.match(line)
        if not match:
            continue
        values = [int(token) for token in match.group(1).split()]
        if len(values) not in (9, 18):
            continue
        for offset in range(0, len(values), 9):
            ctx_idx, pairs = values[offset], values[offset + 1:offset + 9]
            if not 70 <= ctx_idx < COUNT or ctx_idx in rows:
                raise SystemExit(f"unexpected or duplicate ctxIdx row {ctx_idx}")
            rows[ctx_idx] = [(pairs[i], pairs[i + 1]) for i in range(0, 8, 2)]
    expected = set(range(70, 276)) | set(range(277, 399)) | set(range(402, COUNT))
    if set(rows) != expected:
        raise SystemExit(f"missing ctxIdx rows: {sorted(expected - set(rows))[:10]}")
    return rows


def build_tables() -> list[list[tuple[int, int]]]:
    if shutil.which("pdftotext") is None:
        raise SystemExit("pdftotext is required")
    work = ROOT / "tmp"
    work.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(dir=work) as directory:
        output = Path(directory) / "h264.txt"
        subprocess.run(["pdftotext", "-layout", str(PDF), str(output)], check=True)
        rows = parse_columns(output.read_text(encoding="utf-8"))
    # Column 0 is I/SI slices; columns 1-3 are cabac_init_idc 0-2. Unused entries stay (0, 0).
    tables = [[(0, 0)] * COUNT for _ in range(4)]
    for column in range(4):
        for ctx_idx, pair in ALL_SLICES.items():
            tables[column][ctx_idx] = pair
        if column > 0:
            for index, pair in enumerate(PB_ONLY[column - 1]):
                tables[column][11 + index] = pair
        for index, pair in enumerate(TRANSFORM_8X8[column]):
            tables[column][399 + index] = pair
        for ctx_idx, pairs in rows.items():
            tables[column][ctx_idx] = pairs[column]
    return tables


def emit(tables: list[list[tuple[int, int]]], language: str) -> str:
    def rows(open_pair: str, close_pair: str, indent: str) -> list[str]:
        lines = []
        for table in tables:
            lines.append(f"{indent}[")
            for start in range(0, COUNT, 8):
                pairs = ", ".join(f"{open_pair}{m}, {n}{close_pair}" for m, n in table[start:start + 8])
                lines.append(f"{indent}    {pairs},")
            lines.append(f"{indent}],")
        return lines

    header = "Code generated by scripts/generate_cabac_init_tables.py from H.264 Tables 9-12 to 9-24; DO NOT EDIT."
    if language == "go":
        body = rows("{", "}", "\t")
        body = [line.replace("[", "{").replace("]", "}") if line.strip() in ("[", "],") else line for line in body]
        return "\n".join([
            f"// {header}", "", "package vid", "",
            "// cabacContextInitTable holds (m, n) by [I/SI, cabac_init_idc 0-2][ctxIdx].",
            f"var cabacContextInitTable = [4][{COUNT}][2]int8{{", *body, "}", "",
        ])
    if language == "python":
        body = rows("(", ")", "    ")
        body = [line.replace("[", "(").replace("]", ")") if line.strip() in ("[", "],") else line for line in body]
        return "\n".join([
            f'"""{header}"""', "",
            "# (m, n) by [I/SI, cabac_init_idc 0-2][ctxIdx].",
            "CONTEXT_INIT_TABLE = (", *body, ")", "",
        ])
    body = rows("(", ")", "    ")
    return "\n".join([
        f"//! {header}", "",
        "/// (m, n) by [I/SI, cabac_init_idc 0-2][ctxIdx].",
        "#[rustfmt::skip]",
        f"pub(crate) const CONTEXT_INIT_TABLE: [[(i8, i8); {COUNT}]; 4] = [", *body, "];", "",
    ])


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("language", choices=("go", "python", "rust"))
    args = parser.parse_args()
    sys.stdout.write(emit(build_tables(), args.language))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
