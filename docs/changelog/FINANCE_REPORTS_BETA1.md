# Financial Reports Module — ELECTRUVPN BETA1

## Overview
Admin (and sales) financial analytics dashboard with Excel/CSV and printable PDF export.

## Backend (`/api/pro/finance/...`)
- `GET /summary` — today / week / month / all-time revenue, MoM growth %, ARPU, paying users, pending pipeline
- `GET /plan-sales?from=&to=` — revenue & order count per plan
- `GET /monthly-trend` — last 12 months revenue series
- `GET /churn?days=30` — churn rate, renewed vs churned, at-risk clients (expire in 7 days)
- `GET /transactions?from=&to=&status=&kind=` — ledger of shop orders
- `GET /export.csv` — UTF-8 BOM CSV (Excel-friendly, Persian OK)
- `GET /export-report` — print-ready HTML report (browser Print → PDF)

Revenue is computed from `shop_orders` with `status=approved` (plan / wallet_topup / gift).

## Frontend
New sidebar tab **Finance** (owner / admin / sales):
- KPI cards, monthly bar chart, plan doughnut chart (Chart.js)
- Churn panel + at-risk list
- Transactions table
- Date range filter + Excel CSV + PDF/print buttons

## Notes
- No new DB tables; uses existing ShopOrder, Client, TelegramCustomer.
- CSV export preferred over adding excelize dependency for lean builds.
- True PDF is via browser print of the HTML report (universal, no native PDF lib required).
