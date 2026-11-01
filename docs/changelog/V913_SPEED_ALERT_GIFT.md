# V9.13 — Speed Test + Smart Alert + Gift Cards

## Speed Test
- Client-side RTT to all nodes (existing)
- Download Mbps via authenticated `/api/tma/speedtest/payload` (256KiB sample)
- UI shows download Mbps + best ping + ranked node list

## Smart Usage Alert
- 24h / 2h expiry + 10% / 2% volume (existing watermarks)
- CTA: «تمدید سریع با X% تخفیف وفاداری» (`shop_loyalty`)
- Admin: `/setloyalty 10` (0–50) or `/setloyalty off`

## Gift Cards
- Model GiftCard EVG-XXXX-XXXXXX
- User buy (order kind=gift) → admin approve → code delivered
- Admin: `/giftissue TGID PLANID`, `/giftrevoke`, `/gifts`
- User: TMA tab هدیه + bot منوی کارت هدیه + `/redeem CODE`
