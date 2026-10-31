<div align="center">

# ⚡ ELECTRUVPN

### پنل مدرن مدیریت Xray و Sing-box
**نصب با یک دستور · آپدیت مستقیم از GitHub · آمار زنده · ربات و مینی‌اپ تلگرام**

![Status](https://img.shields.io/badge/status-BETA-orange?style=for-the-badge)
![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=for-the-badge&logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/Ubuntu%20%7C%20Debian-systemd-E95420?style=for-the-badge&logo=ubuntu&logoColor=white)
![Postgres](https://img.shields.io/badge/PostgreSQL-16%2F17-4169E1?style=for-the-badge&logo=postgresql&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-ready-2496ED?style=for-the-badge&logo=docker&logoColor=white)
![Last Commit](https://img.shields.io/github/last-commit/alirezaatashi8-commits/ELECTRUVPN?style=for-the-badge)
![Stars](https://img.shields.io/github/stars/alirezaatashi8-commits/ELECTRUVPN?style=for-the-badge&color=yellow)

</div>

---

<div dir="rtl" align="right">

## 🚀 نصب سریع

روی یک سرور **Ubuntu / Debian** با دسترسی root فقط همین یک دستور را بزنید:

</div>

```bash
curl -fsSL https://raw.githubusercontent.com/alirezaatashi8-commits/ELECTRUVPN/main/scripts/remote-install.sh | sudo bash
```

<div dir="rtl" align="right">

همه‌چیز مستقیماً از GitHub دریافت می‌شود؛ نیازی به آپلود یا کپی دستی فایل روی سرور نیست.
این دستور **نصب کامل** را اجرا می‌کند (`install.sh`)، نه فقط آپدیت. وسط نصب، ویزارد گواهی HTTPS نمایش داده می‌شود و می‌پرسد پنل روی **دامنه** نصب شود یا روی **IP سرور** (یا بدون گواهی):

1. گواهی برای دامنه پنل + دامنه جدا برای ساب‌اسکریپشن (پیشنهادی)
2. گواهی برای IP سرور
3. رد کردن گواهی (HTTP)

نصب‌کننده خودش پکیج‌ها، PostgreSQL، Go، Xray-core و Sing-box را نصب می‌کند، پنل را می‌سازد و سرویس systemd می‌سازد.

> **نکته:** بعد از هر نصب و هر آپدیت، `ufw` به‌صورت خودکار **غیرفعال (disable)** می‌شود تا کانفیگ‌ها روی همه پورت‌ها کار کنند؛ لازم نیست دستی `ufw disable` بزنید. اگر می‌خواهید ufw دست نخورد، قبل از نصب/آپدیت `ELECTRU_KEEP_UFW=1` را export کنید.

نصب بدون سؤال (مثلاً اسکریپت/cloud-init):

</div>

```bash
export ELECTRU_NONINTERACTIVE=1
export ELECTRU_CERT_CHOICE=1            # 1=دامنه  2=IP  3=بدون گواهی
export ELECTRU_CERT_DOMAIN=panel.example.com
export ELECTRU_SUBSCRIPTION_DOMAIN=sub.example.com
export ELECTRU_CERT_PORT80_OK=y
curl -fsSL https://raw.githubusercontent.com/alirezaatashi8-commits/ELECTRUVPN/main/scripts/remote-install.sh | sudo -E bash
```

<div dir="rtl" align="right">

بعد از نصب، پنل روی پورت **2087** (یا ۴۴۳ اگر گواهی فعال شود) در دسترس است و یوزر و پسورد اولیه در خروجی نصب چاپ می‌شود.

## 🔄 آپدیت

### روش اول: از داخل داشبورد
در پنل، نسخه جدید روی GitHub بررسی می‌شود و با یک کلیک آپدیت می‌شود.

### روش دوم: با یک دستور

</div>

```bash
sudo electruvpn backup   # پیشنهاد می‌شود
sudo electruvpn update https://github.com/alirezaatashi8-commits/ELECTRUVPN/archive/refs/heads/main.zip
```

<div dir="rtl" align="right">

آپدیت از آرشیو GitHub دانلود می‌شود، نیازی به unzip دستی نیست، تنظیمات و دیتابیس شما حفظ می‌شود و در پایان آپدیت، `ufw` هم مثل نصب اولیه غیرفعال (disable) می‌شود.

---

## ✨ امکانات

### 🛰 پروتکل‌ها
- **Xray:** VLESS، VMess، Trojan، Shadowsocks با REALITY، gRPC، WebSocket، HTTPUpgrade و XHTTP
- **Sing-box:** Hysteria2، TUIC، Naive، AnyTLS، ShadowTLS، WireGuard
- ویزارد ۴ مرحله‌ای ساخت اینباند و ویزارد ساخت کلاینت

### 📊 آمار زنده برای همه پروتکل‌ها
- کاربران آنلاین، تعداد دستگاه‌های متصل و سرعت لحظه‌ای
- Kick کردن کاربر از پنل
- **سرعت‌سنج زنده** روی صفحه ساب‌اسکریپشن (دو عقربه دانلود و آپلود به سبک Speedtest)

### 🤖 تلگرام
- ربات کاربر و ربات ادمین
- مینی‌اپ کاربر (TMA) و داشبورد ادمین
- فروشگاه، کیف پول، تریال، رفرال و کد تخفیف
- اعلان انقضا و هشدار سرعت

### 🌐 چندنودی و مهاجرت
- ایجنت چندنودی
- مهاجرت از **3x-ui** (‏`.db`، ‏`.sql`، دامپ PostgreSQL و JSON)
- بکاپ خودکار و ابری، و انتقال کامل اکانت‌ها به سرور دیگر

### 🔒 امنیت
- JWT، Rate limit، نقش‌ها (RBAC)، رمزنگاری AES-256 برای اطلاعات حساس
- حالت امنیتی strict، مسیر مخفی برای پنل و fail2ban (ufw بعد از نصب/آپدیت عمداً غیرفعال می‌شود)
- DNS رمزنگاری‌شده (DoH) سمت سرور
- ژئو دیتای مخصوص ایران (`geoip:ir`) با آپدیت هفتگی
- Honeypot و محافظ لایه ۷

### 🖥 رابط کاربری
- تاریک، ریسپانسیو و دو زبانه (فارسی / انگلیسی)
- چارت مصرف، گزارش‌های مالی، تیکت و پشتیبانی
- مستندات API (OpenAPI) داخل `web/docs/openapi.yaml`

---

## 📋 پیش‌نیازها

| مورد | مقدار |
|---|---|
| سیستم‌عامل | Ubuntu / Debian با systemd |
| دسترسی | root |
| فضای آزاد | حداقل ۱ گیگ روی `/opt` |
| دیتابیس | PostgreSQL 16/17 (پیشنهادی)، SQLite برای تست |

---

## 🧰 دستورات CLI

</div>

| دستور | کاربرد |
|---|---|
| `electruvpn` | منوی تعاملی |
| `electruvpn status` | وضعیت سرویس و پنل |
| `electruvpn url` | نمایش آدرس پنل |
| `electruvpn start / stop / restart` | کنترل سرویس |
| `electruvpn logs [N]` | مشاهده لاگ زنده |
| `electruvpn port <port>` | تغییر پورت پنل |
| `electruvpn path [random\|/p]` | تغییر مسیر مخفی پنل |
| `electruvpn resetpw [pass]` | ریست پسورد ادمین |
| `electruvpn backup` | بکاپ دیتابیس |
| `electruvpn geo update` | آپدیت دیتای جئو |
| `electruvpn metrics enable` | فعال‌سازی Prometheus |
| `electruvpn update <zip\|url>` | آپدیت پنل |

<div dir="rtl" align="right">

---

## 🐳 Docker (اختیاری)

</div>

```bash
git clone https://github.com/alirezaatashi8-commits/ELECTRUVPN.git
cd ELECTRUVPN
cp .env.example .env
# مقدار POSTGRES_PASSWORD و ELECTRU_JWT_SECRET را عوض کنید:  openssl rand -hex 24
docker compose up -d --build
```

<div dir="rtl" align="right">

## 🔁 مهاجرت از 3x-ui

از پنل: **Inbounds ← Import 3x-ui DB**. قبل از مهاجرت از هر دو پنل بکاپ بگیرید.

## ⚙️ تنظیمات

فایل تنظیمات بعد از نصب در `/etc/electruvpn/electruvpn.env` قرار می‌گیرد؛ نمونه کامل با توضیح در `.env.example` است.
دامنه ساب‌اسکریپشن را حتماً ثابت (دامنه، نه IP) بگذارید.

## 📝 نسخه و تغییرات

- نسخه فعلی: `RELEASE_VERSION.txt`
- یادداشت‌های انتشار: `RELEASE_NOTES_BETA1.txt`
- تاریخچه کامل: `docs/changelog/`

> ⚠️ پروژه در مرحله **BETA** است. قبل از استفاده روی سرور اصلی، بکاپ بگیرید.

## 🤝 مشارکت

Issue و Pull Request خوش‌آمد است. برای گزارش باگ، نسخه، سیستم‌عامل و خروجی `electruvpn logs` را بنویسید.

## 💖 حمایت مالی

اگر پروژه برایتان مفید بود، از توسعه آن حمایت کنید:

| روش | مشخصات |
|---|---|
| 💳 کارت بانکی | `6104331109870551` به نام **علیرضا آتشی** |
| 🪙 TRON (TRX) | `TCht51UQez7ZZbos5KREZjBagRQ8pJi8JL` |

فقط TRX روی شبکه **TRON** ارسال کنید و قبل از ارسال، شبکه را چک کنید.

</div>

---

<div align="center">

ساخته شده با ❤️ توسط [alirezaatashi8-commits](https://github.com/alirezaatashi8-commits)

⭐ اگر پروژه را دوست داشتید، یک ستاره بدهید

</div>
