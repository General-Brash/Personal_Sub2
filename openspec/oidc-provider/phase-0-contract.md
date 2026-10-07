# OIDC Provider Phase 0 contract

- Contract ID: `OIDC-P0-2026-09-17`
- Workspace: `D:\Codex_Program\Personal_Sub2\main`
- Baseline: `main` / `190d05a8ddab9ec457eda63d78631665baaec108`
- Owner: backend implementation in this task
- Status: frozen for the backend first increment; provider remains disabled by default

## 1. Capability boundary

The provider has one immutable issuer and one endpoint family:

```text
issuer:                 https://auth.taffy.edu.kg
authorize:              https://auth.taffy.edu.kg/oauth/authorize
token:                  https://auth.taffy.edu.kg/oauth/token
userinfo:               https://auth.taffy.edu.kg/oauth/userinfo
jwks:                   https://auth.taffy.edu.kg/oauth/jwks
revocation:             https://auth.taffy.edu.kg/oauth/revoke
```

Only administrator-created confidential first-party server-side Web clients are allowed.
Only `client_secret_basic`, `authorization_code`, `refresh_token`, `response_type=code`,
`response_mode=query`, PKCE `S256`, and ID-token signing `RS256` are supported.

Explicitly unsupported and rejected: public/SPA/native clients, implicit/hybrid/password/client-
credentials/device grants, dynamic registration, request/request_uri/claims parameters, duplicate
singleton authorization parameters,
`groups`, business/financial/model/API-key claims, and access-token use outside Provider UserInfo.

`oidc_provider.enabled` is `false` by default. When disabled, every Provider endpoint fails closed
without constructing a login, token, or signing state machine.

## 2. Identity and claims

- RP identity key is `(iss, sub)`.
- `sub` is a persistent Provider subject record, not an email, username, API key, group, or
  business identifier. The subject record is retained across soft deletion and is never recreated
  from a different natural person.
- Role mapping is an exhaustive server-side map: `user -> user`, `admin -> admin`,
  `super_admin -> superadmin`; unknown roles fail closed.
- `profile` exposes only `preferred_username`; `email` exposes only `email`; no
  `email_verified`; `roles` exposes only `role`; no groups or commercial claims.

## 3. State, cookies and crypto

- Authorization transaction state is server-side and single-use. The transaction cookie contains
  only an opaque random handle.
- `state` and `nonce` are stored as AES-256-GCM ciphertext with purpose-bound AAD plus a SHA-256
  fingerprint. The provider encryption key is independent from `jwt.secret` and is required when
  the provider is enabled.
- Browser session cookies are Secure, HttpOnly, SameSite=Lax, Path=/, host-only `__Host-` cookies.
  Session IDs rotate after password/TOTP authentication and session revocation.
- Admin writes require a synchronizer CSRF cookie/header pair, exact permission mapping, and the
  existing step-up middleware. The OIDC route permission check is independent of the global
  `ADMIN_PERMISSIONS_MODE` and fails closed in disabled/shadow mode. The non-HttpOnly
  `__Host-sub2_oidc_admin_csrf` cookie is emitted only for the current authenticated JWT admin
  subject and session/epoch, and `X-CSRF-Token` plus the cookie must match that same context;
  admin API keys cannot perform OIDC sensitive writes.
- Password/TOTP authorization uses a bounded provider-specific fail-closed attempt guard. A
  future shared limiter may replace it, but limiter/storage failure or capacity exhaustion never
  opens unlimited password attempts.

## 4. State machines

```text
authorization transaction: created -> authenticated -> consented -> code_issued
                           -> denied | expired | cancelled
authorization code:        active -> consumed | expired | revoked
refresh token:             active -> rotated | replayed | revoked | expired
refresh family:            active -> revoked | compromised | expired
signing key:               pending -> active -> retiring -> retired | revoked
```

Authorization code consumption and refresh rotation are authoritative PostgreSQL transactions.
A refresh replay compromises and revokes the complete family. Redis is not an authority for these
states.

## 5. TTL and response contract

| Item | Frozen value |
|---|---:|
| transaction | 5 minutes |
| browser session idle/absolute | 30 minutes / 12 hours |
| authorization code | 60 seconds |
| access token | 5 minutes |
| ID token | 5 minutes |
| refresh idle/absolute | 7 days / 30 days |
| clock skew | 60 seconds |
| secret overlap | 24 hours maximum |

Discovery and JWKS are cacheable. Discovery advertises only the configured allowed scopes and
claims derived from those scopes. Token, UserInfo, revocation, authorization success/error, login,
consent, and admin status responses use `no-store`; token responses also use `Pragma: no-cache`.
Authorization errors retain the original `state` whenever redirect validation and the server-side
transaction make a redirect safe.
Invalid/expired/already-revoked revocation targets return the same HTTP 200 empty response after
client authentication and do not reveal ownership/existence.

## 6. First backend increment and explicit deferrals

Implemented in this task: disabled-by-default configuration validation, Phase 0 fixtures,
provider-specific schema/migration/repository contracts, discovery, strict request parsing,
server-side authorization transaction/session/code state, password/TOTP browser authorization,
consent POST, token code exchange, RS256 signing/JWKS, opaque UserInfo token, refresh rotation /
family replay, revocation, admin client/secret/key endpoints, permission/audit/CSRF hooks, and
static/compile-only tests.

Deferred and fail-closed rather than emulated: Passkey login inside the Provider authorization UI,
full browser/admin HttpOnly session replacement for the existing panel, production KMS integration,
OpenID Foundation conformance, PostgreSQL execution/backup/restore, multi-instance runtime,
Cloudflare/origin deployment, and frontend administration. No deferred item is represented as a
successful capability in Discovery.

## 7. Second-round admin/backend contract

The admin API uses explicit canonical DTOs and never serializes internal service/repository
records directly. Collection responses are always `{ "items": [...] }`. Client/key/consent/audit
DTOs use explicit snake_case JSON names; secret digests, private keys, token material, and other
credential material are never returned. A client create or secret rotation returns the plaintext
`client_secret` exactly once in its canonical issue DTO.

The canonical admin routes are:

```text
PUT  /api/v1/admin/oidc-provider/clients/:id              (If-Match/version CAS)
GET  /api/v1/admin/oidc-provider/consents
POST /api/v1/admin/oidc-provider/consents/:id/revoke       (CSRF + step-up + permission)
GET  /api/v1/admin/oidc-provider/audit-events
```

The exact permission inventory is: `oidc.provider.read`, `oidc.clients.read`,
`oidc.clients.write`, `oidc.clients.secret.rotate`, `oidc.clients.disable`,
`oidc.consents.read`, `oidc.consents.revoke`, `oidc.keys.read`, `oidc.keys.rotate`,
`oidc.keys.revoke`, and `oidc.audit.read`. Every admin mutation accepts JSON `reason` and
`request_id`; `reason` reaches the service/repository and structured audit context.

Authorization authentication is an idempotent, binding-preserving CAS: a repeated
`created -> authenticated` transition succeeds only for the same transaction, browser session,
and user; an already-authenticated transaction cannot be continued by an arbitrary authenticated
session. Client secret rotation locks the client row, retires the previous active secret, and
allows at most two unexpired active/retiring secrets. Signing keys cannot revive from retired or
revoked states and the unique active key cannot be revoked without a protected replacement.

Provider business reads/writes and mutations fail closed while disabled. The status endpoint is a
configuration-only safe status read that reports `enabled=false/status=disabled` without loading or
mutating provider state.

## 8. Third-round backend security hardening

- Authorization transactions may remain unconsented only while pending interaction; every issued
  authorization code, token exchange, access token, and offline-access refresh family carries a
  non-null `consent_id` bound to the same user, client, scope snapshot, and client policy version.
- Authorization-code exchange and refresh rotation lock and recheck the consent, client policy,
  client enabled state, and active user. A revoked consent cannot issue or rotate credentials.
- `AdminRevokeConsent` is one database transaction that revokes the consent and all associated
  authorization codes, access tokens, refresh families, and refresh tokens; no partial cascade is
  accepted.
- OIDC admin routes independently enforce their exact `oidc.*` permission even when global admin
  permission enforcement is disabled or shadow-only. CSRF tokens are stateless HMACs bound to
  `user_id | JWT session id | token version | purpose`; they are never stored or logged.

## 9. Site-level consent prompt mode

- `oidc_consent_prompt_mode` is a site-level setting accepting `always` or `remember`; its default is `always`. A setting read failure or invalid value fails closed to `always`.
- In `always`, every interactive authorization displays the consent page, even when an active consent exists; `trusted_skip_consent` never bypasses consent. `prompt=none` may still reuse an existing active consent; without one it returns `consent_required`. In `remember`, an active consent for the same scope and client policy version may be reused; otherwise authorization requires interaction unless `trusted_skip_consent` preauthorization applies (not with `prompt=consent`).
- This setting is managed through the generic `/admin/settings` entry with `system.settings.manage`. This is an explicitly user-selected exception; do not characterize it as using OIDC-specific `oidc.*` permissions or OIDC-specific CSRF/step-up controls.
