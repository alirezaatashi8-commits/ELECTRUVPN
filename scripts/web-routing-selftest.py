#!/usr/bin/env python3
from pathlib import Path
from urllib.parse import urljoin

root = Path(__file__).resolve().parents[1]
index = (root / 'web/index.html').read_text()
login = (root / 'web/login.html').read_text()

def render(template: str, base: str) -> str:
    b = '' if base == '/' else '/' + base.strip('/')
    return template.replace('__ELECTRU_BASE_PATH_ATTR__', b).replace('__ELECTRU_BASE_PATH_JSON__', repr(b))

for base in ('/', '/panel-test-123'):
    html = render(index, base)
    expected = '<base href="/" />' if base == '/' else f'<base href="{base}/" />'
    assert expected in html, (base, expected)
    assert '<base href="//">' not in html
    # Browser URL resolution with the injected base must remain inside the panel base.
    doc = f'https://example.test{base}/dashboard/dashboard'
    base_url = f'https://example.test{base if base != '/' else ''}/'
    asset = urljoin(base_url, './assets/js/app.js')
    expected_prefix = 'https://example.test/assets/' if base == '/' else f'https://example.test{base}/assets/'
    assert asset.startswith(expected_prefix), (base, asset)
    assert '/dashboard/assets/' not in asset

assert "(window.__ELECTRU_BASE_PATH__ || '') + '/dashboard'" in login
assert "(window.__ELECTRU_BASE_PATH__ || '') + '/api/login'" in login
reverse_proxy = (root / 'scripts/setup-reverse-proxy.sh').read_text()
assert 'proxy_set_header Host \\$host;' in reverse_proxy
assert 'proxy_set_header X-Forwarded-Proto \\$scheme;' in reverse_proxy
assert 'proxy_set_header Host $host;' not in reverse_proxy
assert 'proxy_set_header X-Forwarded-Proto $scheme;' not in reverse_proxy
assert "(window.__ELECTRU_BASE_PATH__ || '') + '/api/pro/backups/'" in index
print('web routing matrix: ok')
