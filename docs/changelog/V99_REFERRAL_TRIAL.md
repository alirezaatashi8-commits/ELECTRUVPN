# V9.9 — Smart Referral & One-Click Trial

## Referral / Affiliates
- Each user: deep link `https://t.me/Bot?start=ref{tgId}`
- First-touch attribution on `/start ref…`
- On approved **plan** (or custom_gb) purchase → wallet credit to referrer
- Admin controls (panel API + admin bot):
  - enabled on/off
  - percent 0–50 (`0` = track only, no money)
  - first-purchase-only (default on)
  - min order amount
  - bot username for links

Admin bot:
```
/referral
/setreferral 15
/setreferral off
/setreferral first on|off
/setreferral min 50000
/setreferral bot MyBotUser
```

Panel API:
- `GET/POST /api/pro/shop/referral`

TMA: tab **دعوت** · bot menu **🔗 دعوت دوستان**

## One-Click Free Trial
- Existing one-per-Telegram hard limit kept
- Device limits on trial: LimitIP=1, MaxActiveSessions=1, DeviceBinding on
- TMA: `POST /api/tma/trial` + home card button
- Bot: 🎁 تست رایگان (already present, hardened)

Settings remain: trial_enabled, trial_mb, trial_hours, trial_inbound_id
