# github-device-auth

This repository implements GitHub's OAuth device authorization flow. The reusable protocol surface is deliberately stateless: request a device code, poll for the resulting token, or exchange a refresh token for a replacement token pair.

The installable CLI exposes:

```text
github-device-auth device <client-id>
github-device-auth poll <client-id> <device-code>
github-device-auth refresh <client-id> <refresh-token> [client-secret]
```

The `public` Go package exposes the same device/poll/refresh primitives for composition by callers such as ghxd. It owns no credential files, GitHub Actions secrets, GCS persistence, or deployment policy.

## HTTP router

The historical chi router remains a separate adapter around the same underlying device-flow implementation. It can request device codes, poll, refresh, and optionally cache tokens for HTTP/serverless deployments. Existing router consumers continue to import `github.com/dash-xd/github-device-auth/router`; the stateless `public` package does not import or wrap the router.

The router's GCS support is a deployment convenience, not part of the core device-flow contract. A deployment that does not request caching can use the router without token persistence. Callers using the `public` package or CLI do not depend on GCS at all.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| POST | `/auth/github/device` | Start the device flow; the router's query options may stream/persist results for HTTP deployments. |
| POST | `/auth/github/poll` | Poll once for a device code. |
| POST | `/auth/github/refresh` | Exchange a refresh token for a new access+refresh token pair. |
| POST | `/auth/github/token` | Router-only cached-token convenience when caching is configured. |
| GET | `/device-flow-test` | Browser test surface for the router. |

## Client ID

The device-flow client ID is public application metadata. Router deployments resolve it from `GITHUB_CLIENT_ID` or the documented `X-GitHub-App-Client-ID` request-header fallback. The stateless CLI/public functions receive the client ID explicitly from their caller.

`GITHUB_CLIENT_SECRET` is optional. Public-client device-flow refresh does not require it; callers may supply one only for credential types that need it.

## Router caching

The router retains its existing optional tenant-scoped GCS cache implementation for deployments that intentionally choose that adapter behavior. Historically, Huram's GCP deployment used `TENANT_ID` plus runtime region to derive a bucket and persisted token generations there.

That storage is not a requirement of the reusable device-flow implementation. A caller such as ghxd can carry a complete credential bundle in its own execution context, invoke the stateless refresh primitive when needed, and leave persistence to its own control plane.

## Local development

`go test ./...` exercises the implementation using mock GitHub token endpoints where appropriate. To run the HTTP adapter locally, use the existing router integration in `gospace-minimal`; to consume the protocol directly from Go, import `github.com/dash-xd/github-device-auth/public`.
