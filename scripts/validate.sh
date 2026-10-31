#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

node --check web/assets/js/app.js
for f in install.sh scripts/*.sh; do bash -n "$f"; done
find . -name '*.go' -print0 | xargs -0 gofmt -d > /tmp/electruvpn-gofmt.diff
if [[ -s /tmp/electruvpn-gofmt.diff ]]; then
  cat /tmp/electruvpn-gofmt.diff
  echo 'Go formatting issues found'
  exit 1
fi

python3 - <<'PY'
from pathlib import Path
import re
index = Path('web/index.html').read_text()
login = Path('web/login.html').read_text()
app = Path('web/assets/js/app.js').read_text()
security = Path('internal/api/security.go').read_text()
install = Path('install.sh').read_text()

for p in ['web/index.html','web/login.html','web/assets/js/app.js','web/assets/css/style.css','internal/api/router.go']:
    q=Path(p); assert q.exists() and q.stat().st_size > 100, p

for page in (index, login):
    assert '__ELECTRU_BASE_PATH_ATTR__' in page
    assert '__ELECTRU_BASE_PATH_JSON__' in page
assert "base-uri 'self'" in security
assert "base-uri 'none'" not in security
assert "'./api/" not in index and "'./api/" not in app
assert '"./api/' not in index and '"./api/' not in app
assert "window.__ELECTRU_BASE_PATH__ || ''" in app
assert "window.__ELECTRU_BASE_PATH__ || ''" in login
assert 'dashboard/dashboard' not in login
assert install.count('ELECTRU_PANEL_TLS_ENABLED=$PANEL_TLS_ENABLED') == 1
assert 'source "$PROJECT_DIR/scripts/platform.sh"' in install
security = Path('internal/api/security.go').read_text()
proxy = Path('scripts/setup-reverse-proxy.sh').read_text()
assert 'effectiveLimit = 129 << 20' in security and '/api/inbounds/import-3x-ui' in security
assert 'effectiveLimit = 1537 << 20' in security and '/api/pro/backups/upload' in security
assert 'client_max_body_size 130m;' in proxy and 'client_max_body_size 1538m;' in proxy
for arch in ('amd64','386','arm64','armv7','armv6'):
    assert arch in install or arch in Path('scripts/platform.sh').read_text(), arch
print('static validation: ok')
PY
bash scripts/platform-selftest.sh
python3 scripts/openapi-route-selftest.py
