# ElectruVPN Settings Center — Beta


## بخش‌های اصلی
- Account & Login
- Panel & HTTPS
- Security Center
- API & Access
- Network / Diagnostics / Firewall policy
- Performance / Runtime policy
- Xray / sing-box defaults
- Backup & Restore / verification / remote storage policy
- Database maintenance
- Notifications & Alert Engine policy
- Nodes & Failover
- Client Defaults
- Appearance & Localization
- Unified Logs & Audit
- Update & Maintenance policy
- Emergency Controls
- Beta Feature Flags

## امنیت
- Session tracking با Token ID هش‌نشده در DB و بدون ذخیره JWT خام
- مشاهده و لغو sessionهای فعال
- revoke-all با invalidation قبلی password_changed_at
- session timeout قابل تنظیم
- Panel/API IP allow/block policy
- Security Scan
- Audit برای عملیات حساس
- Emergency mode برای توقف login/API/maintenance/read-only policy

## Backup
- ساخت Backup
- Restore با safety snapshot قبل از جایگزینی
- Verify archive/database presence
- remote backup policy fields
- encryption/verification policy fields

## Database
- SQLite integrity check
- VACUUM
- ANALYZE

## Diagnostics
- TCP/DNS diagnostics
- Runtime metrics
- service journal tail
- Xray/sing-box config validation از endpoint موجود

## محدودیت تست محیطی
`go test ./...` در محیط ساخت فعلی به دلیل نبودن `go.sum` entries و در دسترس نبودن dependency download کامل اجرا نمی‌شود. تست‌های syntax برای JS و shell scripts و gofmt انجام شده‌اند.
