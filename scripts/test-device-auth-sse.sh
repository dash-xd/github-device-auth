#!/usr/bin/env bash
#
# Like test-device-auth.sh's device flow, but drives the SSE variant
# instead: POST /auth/github/device?full[&cache] does the device-code
# request AND the polling server-side, streaming both the device code and
# the eventual outcome back over one connection as device_code / token /
# cached / error SSE events (see router/device_full.go, router/sse.go).
# That means this script never polls itself - it just prints the device
# code the moment it arrives and waits on the stream, which is also why
# there's no --non-interactive flag here: the server is already doing the
# waiting.
#
# Every HTTP call logs its status code and, on failure, the response body,
# so a 4xx/5xx from the service is never silently swallowed.

set -euo pipefail

SCRIPT_NAME="$(basename "$0")"

PROJECT_ID=""
SERVICE_NAME="github-device-auth-router"
REGION="us-central1"
USE_CACHE=false
SHOW_TOKENS=false
IMPERSONATE_SERVICE_ACCOUNT="${IDENTITY_TOKEN_IMPERSONATE_SERVICE_ACCOUNT:-}"

ACCESS_TOKEN=""
REFRESH_TOKEN=""

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

log() {
    local level="$1"
    shift
    printf '[%(%Y-%m-%dT%H:%M:%S%z)T] [%s] %s\n' -1 "$level" "$*" >&2
}

log_info()  { log "INFO"  "$@"; }
log_warn()  { log "WARN"  "$@"; }
log_error() { log "ERROR" "$@"; }

usage() {
    cat <<EOF
Usage: $SCRIPT_NAME auth [options]
       $SCRIPT_NAME token [options]

Runs the full GitHub device flow over the SSE endpoint
(POST /auth/github/device?full) in one round trip: the service requests
the device code, polls for the token itself, and streams both back on the
same connection - this script prints the device code as soon as it
arrives and then just waits on the stream for the final outcome.

  $SCRIPT_NAME auth
      Start the flow and print the resulting access/refresh token.

  $SCRIPT_NAME auth --cache
      Same, but tell the service to cache the resulting token in its
      tenant-scoped bucket instead of streaming it back directly - this
      script then makes one follow-up call to /auth/github/token to read
      it back, proving the cache round trip (see internal/tenantstorage).

  $SCRIPT_NAME token
      Skip the device flow entirely and just call POST /auth/github/token,
      exercising RequireValidCachedToken's expiry-check/refresh/reauth-required
      logic against whatever token is already cached (see
      router/tokenmiddleware.go). Useful for testing that middleware in
      isolation, e.g. after letting a previously cached token expire.

Options:
  --project-id <id>       GCP project ID.
                           (default: \`gcloud config get-value project\`)
  --service-name <name>   Cloud Run service name. (default: $SERVICE_NAME)
  --region <region>       GCP region of the Cloud Run service. (default: $REGION)
  --cache                 Add &cache to the request - see above.
  --impersonate-service-account <email>
                           Pass --impersonate-service-account to every
                           \`gcloud auth print-identity-token\` call. Needed
                           when the active credential is Workload Identity
                           Federation (gcloud rejects --audiences outright
                           on that credential type otherwise), not needed
                           for a plain service-account-keyed login.
                           (default: \$IDENTITY_TOKEN_IMPERSONATE_SERVICE_ACCOUNT)
  --show-tokens           Print the actual token values at the end instead
                           of just "yes"/"no". Tokens are secrets - only use
                           this in a trusted, non-logged terminal.
  -h, --help              Show this help text.

Examples:
  $SCRIPT_NAME auth --project-id my-proj
  $SCRIPT_NAME auth --cache --service-name my-router
  $SCRIPT_NAME auth --impersonate-service-account terraform@my-proj.iam.gserviceaccount.com
  $SCRIPT_NAME token --project-id my-proj
EOF
}

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

if [[ $# -eq 0 ]]; then
    usage >&2
    exit 1
fi

COMMAND="$1"
shift

case "$COMMAND" in
    auth|token)
        ;;
    -h|--help)
        usage
        exit 0
        ;;
    *)
        log_error "Unknown command: $COMMAND"
        usage >&2
        exit 1
        ;;
esac

while [[ $# -gt 0 ]]; do
    case "$1" in
        --project-id)
            PROJECT_ID="${2:?--project-id requires a value}"
            shift 2
            ;;
        --service-name)
            SERVICE_NAME="${2:?--service-name requires a value}"
            shift 2
            ;;
        --region)
            REGION="${2:?--region requires a value}"
            shift 2
            ;;
        --cache)
            USE_CACHE=true
            shift
            ;;
        --impersonate-service-account)
            IMPERSONATE_SERVICE_ACCOUNT="${2:?--impersonate-service-account requires a value}"
            shift 2
            ;;
        --show-tokens)
            SHOW_TOKENS=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            log_error "Unknown argument: $1"
            usage >&2
            exit 1
            ;;
    esac
done

if [[ "$COMMAND" == "token" && "$USE_CACHE" == true ]]; then
    log_warn "--cache has no effect on 'token' (there is no device flow to tell to cache); ignoring it."
    USE_CACHE=false
fi

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------

for bin in gcloud curl jq; do
    if ! command -v "$bin" >/dev/null 2>&1; then
        log_error "Required command not found on PATH: $bin"
        exit 1
    fi
done

if [[ -z "$PROJECT_ID" ]]; then
    log_info "No --project-id given; resolving from gcloud config."
    PROJECT_ID="$(gcloud config get-value project 2>/dev/null || true)"
fi

if [[ -z "$PROJECT_ID" ]]; then
    log_error "Could not determine a GCP project ID."
    log_error "Pass --project-id or run 'gcloud config set project <id>'."
    exit 1
fi

log_info "Resolving Cloud Run URL for service '${SERVICE_NAME}' in '${REGION}' (project '${PROJECT_ID}')..."

if ! SERVICE_URL="$(
    gcloud run services describe "$SERVICE_NAME" \
        --project "$PROJECT_ID" \
        --region "$REGION" \
        --format='value(status.url)'
)"; then
    log_error "gcloud failed to describe Cloud Run service '${SERVICE_NAME}' in '${REGION}'."
    exit 1
fi

if [[ -z "$SERVICE_URL" ]]; then
    log_error "Cloud Run service URL came back empty."
    exit 1
fi

log_info "Service URL resolved: $SERVICE_URL"

echo "Project:      $PROJECT_ID"
echo "Service:      $SERVICE_NAME"
echo "Region:       $REGION"
echo "Service URL:  $SERVICE_URL"
echo "Cache:        $USE_CACHE"
echo

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

get_identity_token() {
    local token
    local impersonate_args=()

    if [[ -n "$IMPERSONATE_SERVICE_ACCOUNT" ]]; then
        impersonate_args=(--impersonate-service-account="$IMPERSONATE_SERVICE_ACCOUNT")
    fi

    if ! token="$(gcloud auth print-identity-token "${impersonate_args[@]}" --audiences="$SERVICE_URL" 2>&1)"; then
        log_error "Failed to obtain an identity token for audience ${SERVICE_URL}:"
        log_error "$token"
        return 1
    fi
    printf '%s' "$token"
}

# Sets API_RESPONSE (body) and API_STATUS (HTTP status code) on return.
# Always logs the status; logs and returns non-zero on 4xx/5xx, so the
# caller (and the user) can see exactly why the service rejected a request
# instead of the failure being masked by a bare curl exit code.
api_call() {
    local method="$1" path="$2" data="${3:-}"
    local token raw status body

    log_info "Requesting identity token for ${SERVICE_URL}..."
    if ! token="$(get_identity_token)"; then
        return 1
    fi

    log_info "-> ${method} ${path}"

    if [[ -n "$data" ]]; then
        raw="$(
            curl --silent --show-error -w '\n%{http_code}' \
                --request "$method" "${SERVICE_URL}${path}" \
                --header "Authorization: Bearer ${token}" \
                --header "Content-Type: application/json" \
                --data "$data"
        )"
    else
        raw="$(
            curl --silent --show-error -w '\n%{http_code}' \
                --request "$method" "${SERVICE_URL}${path}" \
                --header "Authorization: Bearer ${token}" \
                --header "Content-Type: application/json"
        )"
    fi

    status="${raw##*$'\n'}"
    body="${raw%$'\n'"$status"}"

    API_STATUS="$status"
    API_RESPONSE="$body"

    log_info "<- HTTP ${status}"

    if [[ "$status" -ge 400 ]]; then
        log_error "${method} ${path} failed with HTTP ${status}."
        if echo "$body" | jq -e . >/dev/null 2>&1; then
            log_error "Response body: $(echo "$body" | jq -c .)"
        else
            log_error "Response body: ${body}"
        fi
        return 1
    fi

    if echo "$body" | jq -e . >/dev/null 2>&1; then
        echo "$body" | jq .
    else
        log_warn "Response body was not JSON:"
        echo "$body"
    fi

    return 0
}

# Writes a token as a masked step output when running inside GitHub Actions,
# so composite actions built on this script can pass it to later steps
# without printing it into the workflow log.
emit_output() {
    local name="$1" value="$2"

    if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
        echo "::add-mask::${value}"
    fi

    if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
        echo "${name}=${value}" >>"$GITHUB_OUTPUT"
    fi
}

print_token_status() {
    local label="$1" value="$2"

    if [[ "$SHOW_TOKENS" == true ]]; then
        echo "${label}: ${value}"
    else
        echo "${label}: yes"
    fi
}

# ---------------------------------------------------------------------------
# SSE event handling
# ---------------------------------------------------------------------------

STREAM_OUTCOME="" # set to "done" once a terminal event (token/cached/error) lands

handle_device_code_event() {
    local data="$1"
    local verification_uri user_code expires_in interval

    verification_uri="$(jq -r '.verification_uri_complete // .verification_uri // empty' <<<"$data")"
    user_code="$(jq -r '.user_code // empty' <<<"$data")"
    expires_in="$(jq -r '.expires_in // empty' <<<"$data")"
    interval="$(jq -r '.interval // empty' <<<"$data")"

    if [[ -z "$user_code" || -z "$verification_uri" ]]; then
        log_error "device_code event missing user_code/verification_uri: $data"
        exit 1
    fi

    log_info "Device code obtained (user_code=${user_code})."

    echo
    echo "========================================"
    echo "GitHub Device Flow Started (SSE)"
    echo "========================================"
    echo "Open:      $verification_uri"
    echo "Enter:     $user_code"
    echo "Expires:   ${expires_in}s (server polls every ${interval}s)"
    echo "========================================"
    echo
    echo "Complete authorization in GitHub - waiting on the stream..."
    echo
}

handle_token_event() {
    local data="$1"

    ACCESS_TOKEN="$(jq -r '.access_token // empty' <<<"$data")"
    REFRESH_TOKEN="$(jq -r '.refresh_token // empty' <<<"$data")"

    if [[ -z "$ACCESS_TOKEN" ]]; then
        log_error "token event missing access_token: $data"
        exit 1
    fi

    log_info "GitHub authentication succeeded."
    echo
    print_token_status "Access token received" "$ACCESS_TOKEN"
    print_token_status "Refresh token received" "${REFRESH_TOKEN:-<none>}"

    emit_output "access-token" "$ACCESS_TOKEN"
    if [[ -n "$REFRESH_TOKEN" ]]; then
        emit_output "refresh-token" "$REFRESH_TOKEN"
    fi

    STREAM_OUTCOME="done"
}

handle_cached_event() {
    local data="$1"
    local bucket object

    bucket="$(jq -r '.bucket // empty' <<<"$data")"
    object="$(jq -r '.object // empty' <<<"$data")"

    log_info "GitHub authentication succeeded and was cached."
    echo
    echo "========================================"
    echo "Token Cached"
    echo "========================================"
    echo "Bucket:  $bucket"
    echo "Object:  $object"
    echo "========================================"
    echo
    log_info "Fetching the cached token back via /auth/github/token, to prove the round trip..."
    echo

    if ! api_call POST /auth/github/token; then
        log_error "Fetching the cached token failed; see the response body above."
        exit 1
    fi

    ACCESS_TOKEN="$(jq -r '.access_token // empty' <<<"$API_RESPONSE")"

    if [[ -z "$ACCESS_TOKEN" ]]; then
        log_error "No access_token in /auth/github/token response."
        exit 1
    fi

    echo
    print_token_status "Cached access token" "$ACCESS_TOKEN"
    emit_output "access-token" "$ACCESS_TOKEN"

    STREAM_OUTCOME="done"
}

handle_error_event() {
    local data="$1"
    local message

    message="$(jq -r '.message // empty' <<<"$data")"
    log_error "Device flow failed: ${message:-$data}"
    exit 1
}

# Dispatches one complete SSE frame (event + data) to the matching handler.
# Any event type not listed here (there is none today besides the four
# below) is logged and ignored, rather than treated as fatal - forwards
# compatible with a server that starts sending something new.
dispatch_sse_event() {
    local event="$1" data="$2"

    case "$event" in
        device_code) handle_device_code_event "$data" ;;
        token)       handle_token_event "$data" ;;
        cached)      handle_cached_event "$data" ;;
        error)       handle_error_event "$data" ;;
        *)           log_warn "Ignoring unrecognized SSE event '${event}': ${data}" ;;
    esac
}

# ---------------------------------------------------------------------------
# Flow
# ---------------------------------------------------------------------------

# token_only_flow calls POST /auth/github/token directly, without running
# a device flow first, to exercise RequireValidCachedToken's expiry-check/
# refresh/reauth-required state machine against whatever is already cached.
token_only_flow() {
    log_info "Checking the cached GitHub token via /auth/github/token..."
    echo

    if ! api_call POST /auth/github/token; then
        case "$API_STATUS" in
            404)
                log_error "No cached token found. Run '$SCRIPT_NAME auth --cache' first."
                ;;
            401)
                log_error "The cached refresh token is expired. Run '$SCRIPT_NAME auth --cache' again."
                ;;
            *)
                # api_call already logged the status and response body above.
                ;;
        esac
        exit 1
    fi

    ACCESS_TOKEN="$(jq -r '.access_token // empty' <<<"$API_RESPONSE")"

    if [[ -z "$ACCESS_TOKEN" ]]; then
        log_error "No access_token in /auth/github/token response."
        exit 1
    fi

    log_info "Cached GitHub token is valid (refreshed transparently if it needed to be)."
    echo
    print_token_status "Access token" "$ACCESS_TOKEN"
    emit_output "access-token" "$ACCESS_TOKEN"
}

sse_device_flow() {
    local token query event="" data=""

    log_info "Starting GitHub device flow over SSE..."
    echo

    if ! token="$(get_identity_token)"; then
        exit 1
    fi

    query="full"
    if [[ "$USE_CACHE" == true ]]; then
        query="full&cache"
    fi

    # --no-buffer: curl must hand each line to the loop as it arrives, not
    # only once the whole (long-lived) response finishes - this connection
    # stays open for as long as the device flow takes (up to ~15 minutes),
    # streamed via SSE frames separated by a blank line (see
    # router/sse.go's writeSSEEvent). ": keepalive" comment lines
    # (router/device_full.go's sseKeepAliveInterval) match neither the
    # event:/data: prefix nor the blank-line boundary, so they're
    # naturally skipped.
    while IFS= read -r line; do
        line="${line%$'\r'}"

        case "$line" in
            event:*)
                event="${line#event:}"
                event="${event# }"
                ;;
            data:*)
                data="${line#data:}"
                data="${data# }"
                ;;
            "")
                if [[ -n "$event" ]]; then
                    dispatch_sse_event "$event" "$data"
                fi
                event=""
                data=""
                ;;
        esac
    done < <(
        curl --silent --show-error --no-buffer \
            --request POST "${SERVICE_URL}/auth/github/device?${query}" \
            --header "Authorization: Bearer ${token}"
    )

    if [[ "$STREAM_OUTCOME" != "done" ]]; then
        log_error "Stream closed before a token/cached/error event arrived."
        exit 1
    fi
}

case "$COMMAND" in
    auth)
        sse_device_flow
        ;;
    token)
        token_only_flow
        ;;
esac

log_info "Done."
