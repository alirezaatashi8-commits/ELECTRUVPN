# V8.7 — Advanced RBAC, Admin Quotas & Audit

## Roles

| Role | Access |
|------|--------|
| **owner** | Full control, manage admins |
| **admin** | System + clients, no demote owner |
| **sales** | Create/update/delete clients within personal GB/client quotas |
| **support** | Read clients + own audit trail |
| **operator** | Legacy mid-level write (same client write as sales for quotas) |

## Per-admin sales quotas

When creating an admin (owner only):

```json
POST /api/pro/admins
{
  "username": "seller1",
  "password": "strongpass",
  "role": "sales",
  "maxTotalGB": 500,
  "maxGBPerClient": 50,
  "maxClients": 100,
  "notes": "Tehran reseller"
}
```

- `maxTotalGB`: pool of GB they may assign across all their clients (`0` = unlimited)
- `maxGBPerClient`: cap per single client
- `maxClients`: max number of clients they may create
- Tracked fields: `allocatedGB`, `clientCount`, `remainingGB`

Enforced on client create/update/delete. Quota released when they delete a client.

## APIs

| Method | Path | Who |
|--------|------|-----|
| GET | `/api/pro/admins` | owner |
| POST | `/api/pro/admins` | owner |
| POST | `/api/pro/admins/:id` | owner |
| GET | `/api/pro/admins/me/quota` | any logged-in admin |
| GET | `/api/pro/audit?limit=100&username=&action=` | owner/admin see all; sales/support see own |

## Audit log

Each action stores `userId`, `username`, `role`, `action`, `target`, `ip`, `details`.

Examples: `client.create`, `admin.create`, `admin.update`, inbound mutations (existing).

## Client attribution

`Client.createdByUserId` links a VPN user to the panel admin who sold/created them.

