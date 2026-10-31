#!/usr/bin/env python3
"""Compare registered Gin API operations with the checked-in OpenAPI contract."""
from pathlib import Path
import re
import sys
import yaml

ROOT = Path(__file__).resolve().parents[1]
router = (ROOT / "internal/api/router.go").read_text(encoding="utf-8").splitlines()
spec = yaml.safe_load((ROOT / "web/docs/openapi.yaml").read_text(encoding="utf-8"))

actions = {"GET": "get", "POST": "post", "PUT": "put", "PATCH": "patch", "DELETE": "delete"}
routes = set()
for line in router:
    m = re.search(r'\b(group|apiV1|api|pro)\.(GET|POST|PUT|PATCH|DELETE)\("([^"]+)"', line)
    if not m:
        continue
    owner, verb, path = m.groups()
    if owner == "group":
        if path.startswith("/assets") or path in {"/", "/login", "/dashboard", "/dashboard/*route"} or path.endswith("openapi.yaml"):
            continue
        if path == "/api/login":
            path = "/login"
        elif path.startswith("/api/"):
            path = path[4:]
        else:
            continue
    elif owner == "pro":
        path = "/pro" + path
    path = re.sub(r":([A-Za-z0-9_]+)", r"{\1}", path)
    routes.add((path, actions[verb]))

docs = {
    (path, method)
    for path, item in spec.get("paths", {}).items()
    for method in item
    if method in actions.values()
}
missing = sorted(routes - docs)
extra = sorted(docs - routes)
missing_429 = sorted((p, m) for p, m in routes if m in spec.get("paths", {}).get(p, {}) and "429" not in spec["paths"][p][m].get("responses", {}))

if missing or missing_429:
    print("FAIL OpenAPI route coverage")
    if missing:
        print("Missing:", *missing, sep="\n  ")
    if missing_429:
        print("Missing 429:", *missing_429, sep="\n  ")
    sys.exit(1)

print(f"OpenAPI route coverage: PASS ({len(routes)} registered operations)")
if extra:
    print(f"Note: {len(extra)} documented public/compatibility operations are not directly represented by a Gin declaration in the current router parser.")
