#!/usr/bin/env python3
"""Show e2e test coverage matrix across plugins. Highlights gaps."""

import re
from pathlib import Path

E2E_DIR = Path(__file__).parent
PLUGIN_DIR = E2E_DIR.parent / "plugin"
PLUGINS = sorted(p.name for p in PLUGIN_DIR.iterdir() if p.is_dir())


def coverage_matrix():
    """Return (rows, missing_count) where each row is (label, {plugin: bool})."""
    tests_by_file: dict[str, dict[str, set[str]]] = {}

    for py in sorted(E2E_DIR.glob("test_*.py")):
        file_label = py.stem.removeprefix("test_")
        tests_by_file[file_label] = {}

        for line in py.read_text().splitlines():
            m = re.match(r"def (test_(\w+?)_([\w]+))\(", line)
            if not m:
                continue
            plugin, rest = m.group(2), m.group(3)
            if plugin not in PLUGINS:
                continue
            tests_by_file[file_label].setdefault(rest, set()).add(plugin)

    rows = []
    missing = 0
    for file_label, features in tests_by_file.items():
        for feature, plugins in sorted(features.items()):
            tag = f"{file_label}/{feature}"
            has = {p: p in plugins for p in PLUGINS}
            missing += sum(1 for v in has.values() if not v)
            rows.append((tag, has))
    return rows, missing


def format_matrix(rows, missing):
    """Format the coverage matrix as a list of lines."""
    col_w = max(len(p) for p in PLUGINS)
    label_w = 40
    lines = []
    header = f"  {'test':<{label_w}} " + " ".join(f"{p:^{col_w}}" for p in PLUGINS)
    lines.append(header)
    for tag, has in rows:
        cells = [f"{'':^{col_w}}" if has[p] else f"{'?':^{col_w}}" for p in PLUGINS]
        lines.append(f"  {tag:<{label_w}} " + " ".join(cells))
    lines.append("")
    lines.append(f"  {missing} gap(s)" if missing else "  Full coverage!")
    return lines


def main():
    rows, missing = coverage_matrix()
    for line in format_matrix(rows, missing):
        print(line)


if __name__ == "__main__":
    main()
