# V8.5 — Advanced Configuration Encryption (2026)

## What was added

### 1. Password-encrypted config export (Argon2id + AES-256-GCM)
```
GET /api/pro/clients/:id/export?format=encrypted&password=YourStrongPassword
```
Returns a JSON envelope:
```json
{
  "v": 1,
  "alg": "argon2id-aes-256-gcm",
  "salt": "...",
  "nonce": "...",
  "ct": "...",
  "ts": 1760...,
  "label": "user@email"
}
```
Decrypt only with the correct password. Wrong password → authenticated decryption failure.

### 2. Encrypted subscription body
```
GET /sub/<subId>?format=encrypted&password=YourStrongPassword
```
Same envelope format. Default `format=base64` remains for normal client apps (v2rayNG, Hiddify, …).

### 3. Panel master key (at-rest + signatures)
```env
ELECTRU_CONFIG_ENCRYPTION_KEY=<openssl rand -hex 32>
```
- Enables `X-Electru-Config-Sig` HMAC on subscription/export responses
- Ready for encrypting sensitive fields at rest (`ev1:…` format)

### 4. Stronger subscription tokens
New clients get 32-char hex SubIDs (full UUID without hyphens) instead of 16-char prefixes.

## Important limitation (by design)

Share links such as `vless://uuid@host:port?...` **must** include the client credential so standard apps can import them. Encrypting the UUID inside the URI would break every client.

Use **encrypted export / encrypted subscription** when handing configs out-of-band (Telegram, email, support ticket). Keep the normal subscription URL private (HTTPS + long SubID).

## Parameters (2026)
- KDF: Argon2id, t=3, memory=64 MiB, parallelism=4, key=32 bytes
- Cipher: AES-256-GCM
- Nonce: 12 random bytes per encryption

