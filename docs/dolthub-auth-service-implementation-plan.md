# DoltHub Auth Service Implementation Plan

Date: 2026-04-07

## Goal

Replace Nango in hosted mode with a separate `dolthub-auth` service that:

- stores DoltHub API keys under a hard secret boundary
- stores Wasteland connection metadata on the same connection object
- exposes browser-facing connect-token redemption
- exposes a DoltHub-only authenticated proxy for Wasteland
- preserves hosted behavior with minimal product changes

## Key Implementation Decision

Wasteland does not currently have a durable user-account model. To satisfy the
approved design, phase 1 introduces two browser-side identity layers:

- `subject_id`
  - durable browser principal
  - survives logout
  - is the auth-service owner of the stored connection
- `session_id`
  - short-lived authenticated app session
  - destroyed on logout
  - points at the current `connection_id`

This is the minimum change that makes all of the following true at once:

- logout does not delete the stored connection
- Wasteland can mint connect tokens before a connection exists
- auth-service ownership is bound to a stable subject
- Wasteland can later verify `connection_id` ownership server-to-server

## Workstreams

### 1. Auth Service

Owner:

- new package(s) under `internal/dolthubauth/`
- new serve command / entrypoint wiring in `cmd/wl/`

Scope:

- Postgres-backed connection store
- local master-key encryption backend with pluggable interface
- HMAC-signed Wasteland service auth
- connect-token + redeem-secret issuance and redemption
- metadata read and patch endpoints
- DoltHub-only proxy endpoints
- liveness/readiness endpoints

Primary files:

- `internal/dolthauth/*` or `internal/dolthubauth/*`
- `cmd/wl/cmd_serve_auth.go` or equivalent hosted/auth command wiring
- `cmd/wl/main.go` / root-command wiring

Acceptance:

- service starts and creates schema
- browser redemption stores encrypted credentials
- Wasteland service auth is signature-checked
- proxy injects DoltHub auth server-side

### 2. Wasteland Hosted Integration

Owner:

- `internal/hosted/`
- `cmd/wl/cmd_serve.go`

Scope:

- replace `NangoClient` usage with auth-service client
- add subject-principal cookie support
- change `/api/auth/connect-session` to mint auth-service connect tokens
- change `/api/auth/connect` to finalize a returned `connection_id`
- replace metadata reads/writes with auth-service calls
- replace hosted proxy transport and raw-key client construction
- migrate fork registration to proxy-backed clients
- fix shared pending-cache keying in hosted mode

Primary files:

- `internal/hosted/server.go`
- `internal/hosted/auth.go`
- `internal/hosted/session.go`
- `internal/hosted/resolver.go`
- `internal/hosted/fork_register.go`
- `internal/hosted/proxy.go`
- `cmd/wl/cmd_serve.go`

Acceptance:

- hosted mode boots without Nango env vars
- browser can connect, refresh, join, leave, and browse
- hosted resolver and fork registration no longer require raw API keys

### 3. Frontend Connect Flow

Owner:

- `web/src/api/*`
- `web/src/components/ConnectPage*`

Scope:

- remove Nango frontend dependency usage
- request connect token + redeem secret from Wasteland
- POST credentials directly to auth service
- finalize connect with returned `connection_id`
- keep join flow server-to-server through Wasteland

Primary files:

- `web/src/api/client.ts`
- `web/src/api/types.ts`
- `web/src/components/ConnectPage.tsx`
- `web/src/components/ConnectPage.test.tsx`
- remove `web/src/api/nango.ts`

Acceptance:

- hosted connect works without `@nangohq/frontend`
- tests cover direct redemption and finalize-connect flow

### 4. Config And Deployment Surface

Owner:

- `cmd/wl/cmd_serve.go`
- docs and env wiring

Scope:

- replace hosted Nango env vars with auth-service env vars
- add auth-service-specific env contract
- keep hosted app and auth service deployable independently

New Wasteland hosted env:

- `WL_SESSION_SECRET`
- `WL_AUTH_SUBJECT_SECRET`
- `DOLTHUB_AUTH_BASE_URL`
- `DOLTHUB_AUTH_KEY_ID`
- `DOLTHUB_AUTH_SHARED_SECRET`

New auth-service env:

- `DOLTHUB_AUTH_LISTEN_ADDR`
- `DOLTHUB_AUTH_DATABASE_URL`
- `DOLTHUB_AUTH_TENANT_ID`
- `DOLTHUB_AUTH_ENVIRONMENT`
- `DOLTHUB_AUTH_CURRENT_KEY_ID`
- `DOLTHUB_AUTH_CURRENT_SHARED_SECRET`
- `DOLTHUB_AUTH_NEXT_KEY_ID` optional
- `DOLTHUB_AUTH_NEXT_SHARED_SECRET` optional
- `DOLTHUB_AUTH_TOKEN_PEPPER`
- `DOLTHUB_AUTH_REDEEM_PEPPER`
- `DOLTHUB_AUTH_MASTER_KEY`
- `DOLTHUB_AUTH_ALLOWED_ORIGINS`

## Migration Order

### Phase 1. Land Service And Client Primitives

- add auth-service packages, schema, crypto, HMAC auth, and proxy transport
- add Wasteland auth-service client package
- add subject-cookie helpers

Gate:

- packages compile
- unit tests for store/auth/signing/redemption pass

### Phase 2. Replace Connect Flow

- Wasteland `/api/auth/connect-session` now returns auth-service redemption data
- frontend redeems directly against auth service
- Wasteland `/api/auth/connect` finalizes only `connection_id`

Gate:

- hosted connect page tests pass
- finalize-connect path sets session correctly

### Phase 3. Replace Runtime Metadata + Proxy Usage

- auth status, bootstrap, join, leave, save settings, restore-session checks
  move off Nango
- resolver and fork registrar move to proxy-backed clients

Gate:

- hosted auth/server tests pass
- resolver/fork registration tests pass

### Phase 4. Remove Nango

- delete Nango client/package usage
- remove Nango frontend dependency
- remove Nango env wiring and tests

Gate:

- repo builds without Nango references

## Review And Quality Gates

Before calling the work complete:

- Go tests for new auth-service package
- targeted hosted-mode Go tests
- frontend vitest for connect flow
- `/review-pr --diff <generated diff> --repo /data/projects/wasteland --skip-gemini`
- fix findings until synthesis returns no blockers or majors

## Known High-Risk Areas

- `subject_id` introduction must not break existing hosted session assumptions
- resolver pending cache must stop sharing per-upstream state across different
  authenticated connections
- fork registration must stop requiring raw API keys
- connect flow must keep secrets out of Wasteland request bodies, logs, and
  telemetry

## Expected First Cut

Phase 1 implementation will support:

- Postgres store
- local master-key encryption backend
- HMAC request signing
- direct browser redemption
- hosted metadata read/update
- DoltHub REST and GraphQL proxy

Phase 1 will intentionally not implement:

- cloud KMS backend
- end-user delete-connection UX
- `doltremoteapi.dolthub.com` proxying
- distributed cache invalidation
