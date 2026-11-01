# Part 7 — Clock precision + all-sections audit (frontend only, no backend changes)
- web/assets/js/app.js: live clock re-arms a timeout for the next wall-clock second boundary (was a fixed 1000ms interval that lagged up to ~1s).
- web/index.html: Subscription tab — the "Dual" address row had a ternary without an else branch (`Unexpected token '}'`), which broke the tab's rendering. Fixed.
- web/index.html: 12 blocks guarded only by `x-show="obj"` (selected subscription, honeypot report, reseller edit form, public-host status) are now `<template x-if>`; their inner expressions no longer run while `obj` is null (removed ~30 console TypeErrors on Subscription / Pro / Settings / Resellers).
- web/assets/css/style.css: `.grid > .glass { min-width:0 }` — Platform tab (Multi-Hop card) no longer overflows phones by ~177px. `.chart-box` wrapper for the finance charts (no overflow at 320px).
- web/assets/js/app.js: Chart.js instances (finance monthly/plans, subscription traffic) live on `window.__electruCharts`, outside Alpine's reactive state (fixes "reading 'save'" of null after refresh / tab re-entry).
- web/assets/js/app.js + index.html: every copy button goes through copyText() (Clipboard API with execCommand fallback; works on plain HTTP and reports failure honestly instead of always saying "copied").
- assets ?v=beta43. RELEASE_SOURCE_MANIFEST.txt regenerated.
