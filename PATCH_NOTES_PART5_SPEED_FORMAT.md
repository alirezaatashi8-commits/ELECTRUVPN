# Part 5 — Speed display fix
- web/index.html: Inbound Live modal appended "/s" to fmtSpeed() (which already returns "MB/s"), showing "MB/s/s". Removed the extra suffix (4 places).
- web/assets/js/app.js: formatSpeed() now delegates to fmtSpeed() so the Online tab and all Live views share one formatter (adds GB/s, TB/s).
- assets ?v=beta41. No backend changes.
