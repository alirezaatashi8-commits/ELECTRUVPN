#!/usr/bin/env bash
# آپلود پروژه روی GitHub. توکن هیچ‌جا ذخیره نمی‌شود.
# استفاده (داخل پوشه پروژه):  bash upload-to-github.sh
set -euo pipefail

GH_USER="alirezaatashi8-commits"
GH_REPO="ELECTRUVPN"
BRANCH="main"

command -v git >/dev/null 2>&1 || { apt-get update -qq && apt-get install -y -qq git; }

if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  read -rsp "توکن GitHub را وارد کنید (نمایش داده نمی‌شود): " GITHUB_TOKEN; echo
fi
AUTH="AUTHORIZATION: basic $(printf '%s' "${GH_USER}:${GITHUB_TOKEN}" | base64 -w0)"

[[ -d .git ]] || git init -q
git checkout -B "$BRANCH" -q
git config user.name  "$GH_USER"
git config user.email "${GH_USER}@users.noreply.github.com"

VER="$(head -n1 RELEASE_VERSION.txt 2>/dev/null || echo update)"
git add -A
git commit -q -m "${VER}" || echo "تغییری برای commit وجود نداشت"

git remote remove origin 2>/dev/null || true
git remote add origin "https://github.com/${GH_USER}/${GH_REPO}.git"
git -c http.extraheader="$AUTH" push -u origin "$BRANCH" --force-with-lease 2>/dev/null \
  || git -c http.extraheader="$AUTH" push -u origin "$BRANCH" --force

unset GITHUB_TOKEN AUTH
echo "✅ آپلود شد: https://github.com/${GH_USER}/${GH_REPO}"
echo "نصب:  curl -fsSL https://raw.githubusercontent.com/${GH_USER}/${GH_REPO}/${BRANCH}/scripts/remote-install.sh | sudo bash"
