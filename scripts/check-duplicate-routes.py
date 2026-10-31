#!/usr/bin/env python3
"""Fail if the same HTTP route is registered twice in one gin router.

gin panics at startup ("handlers are already registered for path ...") when a
method+path pair is registered twice, which makes the whole panel fail its
health check. Run this before packaging a release:

    python3 scripts/check-duplicate-routes.py
"""
import collections
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
GROUP = re.compile(r'(\w+)\s*:?=\s*(\w+)\.Group\("([^"]*)"')
ROUTE = re.compile(r'\b(\w+)\.(GET|POST|PUT|DELETE|PATCH|Any|Handle)\("([^"]*)"')


def main() -> int:
    seen = collections.defaultdict(list)
    for path in sorted((ROOT / "internal").rglob("*.go")):
        if path.name.endswith("_test.go"):
            continue
        src = path.read_text(encoding="utf-8")
        groups = {m.group(1): (m.group(2), m.group(3)) for m in GROUP.finditer(src)}

        def full(name):
            prefix = ""
            while name in groups:
                parent, part = groups[name]
                prefix, name = part + prefix, parent
            return name, prefix

        for lineno, line in enumerate(src.splitlines(), 1):
            for m in ROUTE.finditer(line):
                root, prefix = full(m.group(1))
                seen[(path.name, root, m.group(2), prefix + m.group(3))].append(lineno)
    bad = {k: v for k, v in seen.items() if len(v) > 1}
    for (fname, _root, method, route), lines in sorted(bad.items()):
        print(f"DUPLICATE {method} {route}  {fname}: lines {lines}")
    if bad:
        return 1
    print("No duplicate routes found.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
