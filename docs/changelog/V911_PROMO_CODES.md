# V9.11 — Dynamic Promo Codes

## Model
- PromoCode: code, percent|fixed, value, maxUses, usedCount, perUserLimit, minOrder, expires, active
- PromoUsage: one row per order (idempotent)
- ShopOrder: promoCode, originalPriceToman, discountToman

## User
- TMA shop: input + apply → buy with promoCode
- Bot: `/promo YALDA20` then buy from shop

## Admin
```
/addpromo YALDA20 percent 20 100 2026-12-30
/addpromo OFF50 fixed 50000 50 2026-11-01 100000
/disablepromo YALDA20
/promos
```

## API
- POST /api/tma/promo/apply
- POST /api/tma/shop/buy { planId, promoCode }
- Commit on FulfillOrder / DebitAndFulfill
