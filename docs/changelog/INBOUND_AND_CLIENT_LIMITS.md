# Inbound and client volume / time limits

- Every inbound can have its own total volume (GB, 0 = unlimited) and its own
  duration (days, 0 = never expires). The limit covers all clients of the
  inbound together. Each client keeps its own separate volume and days.
- When an inbound reaches its volume or its date, it is removed from the
  generated xray / sing-box config until the limit is raised, the date is
  extended or the inbound traffic is reset. The change is applied once by the
  background traffic worker (about every 45 seconds) with a single restart.
- Create / edit inbound: step 4 has "Total volume GB" and "Days". In edit mode
  a blank days field keeps the current expiry and 0 removes the expiry.
- Quick create (gRPC, WebSocket, HTTPUpgrade, XHTTP, REALITY, Anti-Filter)
  has optional "Inbound volume" and "Inbound days" fields.
- Clients: create, bulk create and edit accept `days` (counted from now);
  edit client has a "Days from now" field. A client that was disabled by the
  traffic worker because of expiry or volume is re-enabled automatically when
  its date or volume is extended.
- API: `POST /inbounds` accepts `totalGB` and `days`;
  `POST /inbounds/:id/update` accepts `totalGB`, `days` or `expiryTime`;
  `POST /clients`, `/clients/bulk` and `/clients/:id/update` accept `days`.
- Traffic is always counted 1:1 (plain Up + Down bytes).
