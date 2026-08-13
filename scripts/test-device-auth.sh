#!/usr/bin/env bash
#
# Exercises the GitHub device-flow Cloud Run service end to end:
#   device   Request a device code and poll until the user authorizes it.
#   refresh  Exchange a refresh token for a new access/refresh token pair.
#   full     Run device, then immediately feed its refresh token into refresh.
#
# Every HTTP call logs its status code and, on failure, the response body,
# so a 4xx/5xx from the service is never silently swallowed.

set -euo pipefail

SCRIPT_NAME="$(basename "$0")"

PROJECT_ID=""
SERVICE_NAME="github-device-auth-router"
REGION="us-central1"
REFRESH_TOKEN_INPUT=""
NON_INTERACTIVE=false
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
Usage: $SCRIPT_NAME <command> [options]

Commands:
  device      Request a device code and poll until authorized. Prints
              access_token/refresh_token status (device, then poll).
  refresh     Exchange a refresh token for a new access/refresh token pair.
  full        Run 'device', then immediately run 'refresh' with the
              resulting refresh token.

Options:
  --project-id <id>       GCP project ID.
                           (default: \`gcloud config get-value project\`)
  --service-name <name>   Cloud Run service name. (default: $SERVICE_NAME)
  --region <region>       GCP region of the Cloud Run service. (default: $REGION)
  --refresh-token <tok>   Refresh token for the 'refresh' command.
                           (default: \$GITHUB_REFRESH_TOKEN)
  --impersonate-service-account <email>
                           Pass --impersonate-service-account to every
                           `gcloud auth print-identity-token` call. Needed
                           when the active credential is Workload Identity
                           Federation (gcloud rejects --audiences outright
                           on that credential type otherwise), not needed
                           for a plain service-account-keyed login.
                           (default: \$IDENTITY_TOKEN_IMPERSONATE_SERVICE_ACCOUNT)
  --non-interactive       Skip the "press enter to continue" prompt during
                           the device flow and start polling immediately.
  --show-tokens           Print the actual token values at the end instead
                           of just "yes"/"no". Tokens are secrets - only use
                           this in a trusted, non-logged terminal.
  -h, --help              Show this help text.

Examples:
  $SCRIPT_NAME device --project-id my-proj
  $SCRIPT_NAME refresh --refresh-token ghr_xxx
  GITHUB_REFRESH_TOKEN=ghr_xxx $SCRIPT_NAME refresh --service-name my-router
  $SCRIPT_NAME refresh --impersonate-service-account terraform@my-proj.iam.gserviceaccount.com
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
    device|refresh|full)
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
        --refresh-token)
            REFRESH_TOKEN_INPUT="${2:?--refresh-token requires a value}"
            shift 2
            ;;
        --impersonate-service-account)
            IMPERSONATE_SERVICE_ACCOUNT="${2:?--impersonate-service-account requires a value}"
            shift 2
            ;;
        --non-interactive)
            NON_INTERACTIVE=true
            shift
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
# Flows
# ---------------------------------------------------------------------------

device_flow() {
    log_info "Starting GitHub device flow..."
    echo

    if ! api_call POST /auth/github/device; then
        log_error "Device code request failed; see the response body above."
        exit 1
    fi
    local device_response="$API_RESPONSE"

    DEVICE_CODE="$(jq -r '.device_code // empty' <<<"$device_response")"
    VERIFICATION_URI="$(jq -r '.verification_uri // empty' <<<"$device_response")"
    USER_CODE="$(jq -r '.user_code // empty' <<<"$device_response")"

    if [[ -z "$DEVICE_CODE" ]]; then
        log_error "No device_code in response."
        exit 1
    fi

    log_info "Device code obtained (user_code=${USER_CODE})."

    echo
    echo "========================================"
    echo "GitHub Device Flow Started"
    echo "========================================"
    echo "Open:      $VERIFICATION_URI"
    echo "Enter:     $USER_CODE"
    echo "========================================"
    echo

    if [[ "$NON_INTERACTIVE" == true ]]; then
        log_info "Non-interactive mode: skipping prompt, polling immediately."
    else
        echo "Complete authorization in GitHub."
        echo "Press Enter after authorization."
        read -r
    fi

    log_info "Polling Cloud Run service for GitHub token..."
    echo

    if ! api_call POST /auth/github/poll "$(
        jq -nc --arg device_code "$DEVICE_CODE" '{device_code: $device_code}'
    )"; then
        log_error "Polling for token failed; see the response body above."
        exit 1
    fi
    local poll_response="$API_RESPONSE"

    ACCESS_TOKEN="$(jq -r '.access_token // empty' <<<"$poll_response")"
    REFRESH_TOKEN="$(jq -r '.refresh_token // empty' <<<"$poll_response")"

    if [[ -z "$ACCESS_TOKEN" ]]; then
        log_error "No access_token in poll response."
        exit 1
    fi

    if [[ -z "$REFRESH_TOKEN" ]]; then
        log_error "No refresh_token in poll response."
        exit 1
    fi

    log_info "Initial GitHub authentication succeeded."
    echo
    print_token_status "Access token received" "$ACCESS_TOKEN"
    print_token_status "Refresh token received" "$REFRESH_TOKEN"

    emit_output "access-token" "$ACCESS_TOKEN"
    emit_output "refresh-token" "$REFRESH_TOKEN"
}

refresh_flow() {
    local token_to_use="${REFRESH_TOKEN:-}"

    if [[ -z "$token_to_use" ]]; then
        token_to_use="${REFRESH_TOKEN_INPUT:-${GITHUB_REFRESH_TOKEN:-}}"
    fi

    if [[ -z "$token_to_use" ]]; then
        log_error "No refresh token available."
        log_error "Either run '$SCRIPT_NAME full' (or 'device' first), or pass"
        log_error "--refresh-token, or set \$GITHUB_REFRESH_TOKEN."
        exit 1
    fi

    log_info "Refreshing GitHub access token..."
    echo

    if ! api_call POST /auth/github/refresh "$(
        jq -nc --arg refresh_token "$token_to_use" '{refresh_token: $refresh_token}'
    )"; then
        log_error "Refresh request failed; see the response body above for"
        log_error "why GitHub or the service rejected this refresh token."
        exit 1
    fi
    local refresh_response="$API_RESPONSE"

    local new_access_token new_refresh_token
    new_access_token="$(jq -r '.access_token // empty' <<<"$refresh_response")"
    new_refresh_token="$(jq -r '.refresh_token // empty' <<<"$refresh_response")"

    if [[ -z "$new_access_token" ]]; then
        log_error "Refresh response did not contain access_token."
        exit 1
    fi

    if [[ -z "$new_refresh_token" ]]; then
        log_error "Refresh response did not contain refresh_token."
        exit 1
    fi

    ACCESS_TOKEN="$new_access_token"
    REFRESH_TOKEN="$new_refresh_token"

    log_info "GitHub token refresh succeeded. The refresh token was rotated."
    echo
    echo "========================================"
    echo "GitHub Token Refresh Succeeded"
    echo "========================================"
    print_token_status "New access token" "$ACCESS_TOKEN"
    print_token_status "New refresh token" "$REFRESH_TOKEN"
    echo
    echo "The refresh token was rotated."
    echo "Use the newly returned refresh token for the next refresh."

    emit_output "access-token" "$ACCESS_TOKEN"
    emit_output "refresh-token" "$REFRESH_TOKEN"
}

# ---------------------------------------------------------------------------
# Dispatch
# ---------------------------------------------------------------------------

case "$COMMAND" in
    device)
        device_flow
        ;;
    refresh)
        refresh_flow
        ;;
    full)
        device_flow
        refresh_flow
        ;;
esac

log_info "Done."
