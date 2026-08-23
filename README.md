# github-device-auth

A chi router implementing GitHub's [OAuth device authorization
flow](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#device-flow):
requesting a device code, polling for the resulting access token,
refreshing it, and (optionally) caching it in a tenant-scoped GCS
bucket instead of returning it to the caller.

This repo owns none of its own hosting: it exports `router.NewRouter()`
and expects to be served by something else. In this project that's
[`dash-xd/gospace-minimal`](https://github.com/dash-xd/gospace-minimal),
deployed as a GCP Cloud Function (gen 2) by
[`xd-dash/huram-abi`](https://github.com/xd-dash/huram-abi)'s
`terraform/device-auth-router` module and
`deploy-device-auth-router` workflow.

## Endpoints

| Method | Path | Body | Purpose |
|---|---|---|---|
| POST | `/auth/github/device` | - | Start the device flow. Add `?full` to instead stream the whole flow (device code + poll outcome) as SSE; add `&cache` with `?full` to cache the resulting token server-side instead of returning it. |
| POST | `/auth/github/poll` | `{"device_code": "..."}` | Poll once for the token belonging to a device code obtained from `/auth/github/device` (without `?full`). |
| POST | `/auth/github/refresh` | `{"refresh_token": "..."}` | Exchange a refresh token for a new access token. Add `?cache` to cache the result instead of returning it. |
| POST | `/auth/github/token` | - | Return the cached access token for this deployment, transparently refreshing it first if needed. Requires caching to be configured (see below). |
| GET | `/device-flow-test` | - | Serves `router/webui/device-flow-test.html`, a standalone vanilla HTML/JS page that exercises the full flow against whatever origin it's loaded from. |

`GITHUB_CLIENT_ID` must be set for every endpoint except `/auth/github/token`'s
cache-only bucket lookup (`/auth/github/token` still needs it too, to know
which client's cache slot to read). `GITHUB_CLIENT_SECRET` is optional -
device-flow refresh tokens belong to a public client and refresh without
one; only set it if you also refresh non-device-flow tokens through here.

## Trying it

Open the deployed function's `/device-flow-test` path in a browser (or
run this router locally - see gospace-minimal's `cmd/localserve` - and
open `http://127.0.0.1:8080/device-flow-test`). It defaults its base URL
to whatever origin served it, starts the flow over SSE, shows the code
to enter on GitHub, and once caching confirms the token landed in the
bucket, fetches it back via `/auth/github/token` to prove the round
trip. See `router/webui/device-flow-test.html` for the vanilla-JS
implementation (it uses `fetch()` + manual SSE frame parsing rather than
`EventSource`, since the SSE endpoint here is a POST route).

## Token caching and tenant-scoped storage

Caching is opt-in per request (`?cache` on `/auth/github/device?full` or
`/auth/github/refresh`) and, once a token is cached, is what
`/auth/github/token` reads from. There is deliberately **no bucket-name
environment variable** to configure. Instead:

- `TENANT_ID` identifies who this deployment belongs to. It's the same
  value across every function a tenant's service account runs, so it
  changes far less often than a bucket name would.
- The **runtime region** is discovered from the GCE metadata server (a
  `REGION` env var is a fallback for local development, where there's no
  metadata server to ask).
- `internal/tenantstorage.BucketName` combines the two into
  `{TENANT_ID}-{region}-github-token-cache` - the exact bucket
  `terraform/tenant-token-cache` (in huram-abi) provisions for that same
  `(tenant_id, region)` pair, and the exact bucket that module's
  `service_account_email` output is scoped to and nothing else.

Deploying without `TENANT_ID` set is fine - the device/poll/refresh
endpoints all still work - but any `?cache` request and every call to
`/auth/github/token` will 500 with "token cache bucket is not
configured", since there's no tenant identity to derive a bucket from.

### What should `TENANT_ID` be?

Conceptually, `TENANT_ID` is meant to correspond to the GitHub account
(org or user) that will actually run the device flow and own the cached
token - that's what keeps one tenant's cached credentials from ever
being reachable by another tenant's function. **But `TENANT_ID` is a
label your deploy step chooses, not something looked up from GitHub.**
Nothing here reads it back from an installation, a GitHub App
registration, or an OAuth grant, and nothing enforces that the account
you eventually authenticate as matches it - so it works identically
whether the app is already installed somewhere or not.

That matters for bootstrapping: if you've just registered a GitHub App
by hand and haven't installed it anywhere yet, there is no "installed
account" to look up a tenant identifier from - and you don't need one.
Pick the identifier now (conventionally, the lowercased login of the
org/user account you intend to authenticate as - see the naming rule
below), pass it as `tenant_id` to huram-abi's
`deploy-device-auth-router` workflow, and deploy. The bucket and
service account it provisions will already be correctly scoped by the
time you actually run the device flow against it - installation status
never enters into it.

Two things to keep in mind when choosing it:

- **Naming rule**: `terraform/tenant-token-cache` requires `tenant_id`
  to be lowercase letters, digits, and hyphens, 1-22 characters (GCS
  bucket name / service account `account_id` constraints, with room
  left for the `-runtime` suffix). GitHub logins can contain characters
  or casing that don't satisfy this - lowercase it and drop anything
  that doesn't fit (e.g. `My-Org` -> `my-org`).
- **It's a provisioning-time decision, not a runtime check**: if you
  reuse the same `tenant_id` for two different GitHub accounts, or
  authenticate as an account other than the one `tenant_id` was meant to
  represent, this router has no way to notice or object - it just
  caches whatever token the device flow returns under that tenant's
  bucket. Keep the mapping straight in whatever bootstrapping notes or
  provisioning docs you already keep.

See `xd-dash/huram-abi`'s `deploy-device-auth-router` action/workflow
for how `tenant_id` gets turned into an actual service account + bucket
pair, and this repo's `internal/tenantstorage` package doc for the full
naming-convention rationale.

## Limitations: what a device-flow token can't do

The access token this router hands back (cached or not) is whatever
GitHub issues at the end of the device flow for your OAuth App/GitHub
App client - its capabilities are bounded entirely by that app's
configured scopes/permissions, same as any OAuth token. Two
consequences worth calling out explicitly:

- **It cannot manage Actions/Dependabot/Codespaces secrets.** GitHub's
  secrets-management REST endpoints (creating/updating repository,
  environment, or organization secrets) require either a classic
  personal access token, a fine-grained PAT with the specific "Secrets"
  permission granted, or a GitHub App installation token with that same
  permission - a user-facing OAuth/device-flow access token is not
  accepted there regardless of what scopes it carries. If your
  automation needs to write secrets, that's a separate credential (a
  PAT, or a GitHub App installation access token obtained some other
  way) - don't expect a token from this router's cache to work for it.
- **Everything else is scope-limited as usual.** Org administration,
  repo transfers, and similar sensitive operations depend on the
  scopes/permissions granted when the device flow was authorized, same
  as any other OAuth-issued token - this router doesn't expand or
  change what the token can do, it only automates obtaining, refreshing,
  and (optionally) caching it.

## Local development

`go test ./...` runs the full suite, including a mock GitHub token
endpoint (`router/github_mock_test.go`) - no real GitHub App/network
access needed. To serve the router locally end-to-end (e.g. to try
`/device-flow-test` against a real GitHub App), see
`dash-xd/gospace-minimal`'s `cmd/localserve` and `.github/actions/router`
- point its `internal/routersource/source/source.go` at this repo's
`router.NewRouter`, set `GITHUB_CLIENT_ID` (and `TENANT_ID`/`REGION` if
you want to exercise caching), and run it.
