# PART 18 — Persian / English (fa / en) language: Settings pages

Files: `web/index.html`, `web/assets/js/app.js`

- The whole **Settings** area (all 9 pages: Account & Login, Panel & Server Address, Security & Access, Network & Performance, Backup & Database, Nodes & Clients, Notifications & Alerts, Appearance & Language, Updates & Logs) now follows the panel language switcher (فارسی / English).
- 183 new keys (`set.*`) added to both the `fa` and `en` packs in `app.js` (packs stay identical: 331 keys each). Labels, descriptions, buttons, placeholders, select options, metric tiles and the final-confirmation notice are translated via `t('set.…')`.
- Original text is kept inside each element as a fallback, so nothing is blank if a key is ever missing.
- Technical identifiers are intentionally left as-is (BBR, IPv6, UDP, HSTS, Fail2Ban, fq / fq_codel, debug / info / warning, sftp / webdav, MB / GB / TB, Cloudflare / Google / Quad9 …).
- Appearance → «Language» select now switches the panel language immediately (`setLanguage()`), no need to save first.
- New helper `tt(fa, en)` in `app.js` for one-off bilingual messages; all settings-related toasts and confirm dialogs (credentials, panel/HTTPS save, backups & restore/upload, BBR, DB maintenance, sessions, public host / IPv4) are now bilingual.
- Longer sentences that contain inline `<code>` / `<b>` (metrics endpoint, feature-flag hints) have separate fa / en blocks shown by `uiLang`.
- No backend / API / DB change. Layout, field bindings and save logic untouched.
- File names and ZIP name unchanged. `RELEASE_SOURCE_MANIFEST.txt` regenerated.

Not translated yet (still original mixed fa/en text): Finance, Pro Center, Control Center, Operations (large parts), Inbounds/Clients modals, subscription page.
