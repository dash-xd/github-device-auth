package router

import (
	"context"
	"net/http"
	"time"

	"github.com/dash-xd/github-device-auth/internal/ghdeviceflow"
	"github.com/dash-xd/github-device-auth/internal/tenantstorage"
	"github.com/dash-xd/github-device-auth/internal/tokencache"
)

// accessTokenRefreshBuffer is how much validity an access token must
// still have left to be served as-is; anything inside this window is
// treated as already expired, so a caller is never handed a token that
// dies moments after it's returned.
const accessTokenRefreshBuffer = 60 * time.Second

type cacheConfirmation struct {
	Cached bool   `json:"cached"`
	Bucket string `json:"bucket"`
	Object string `json:"object"`
}

// cacheObjectKey is the fixed cache slot for a given GitHub OAuth
// client: one deployment, one "latest" token. Not caller-controlled -
// letting a request pick its own object name would mean any caller of a
// shared deployment could read or clobber another's cached token.
func cacheObjectKey(clientID string) string {
	return clientID + "/latest.json"
}

// parseCacheRequest inspects the cache query param and resolves the
// tenant-scoped cache bucket for this deployment.
//
// If cache wasn't requested, requested is false and ok is true - there's
// nothing more to do. If cache was requested but the bucket can't be
// resolved (TENANT_ID missing, or the runtime region can't be
// determined - see tenantstorage), ok is false and an error response
// has already been written to w; the caller must return immediately
// without writing anything else. Otherwise requested and ok are both
// true, and bucket/key are ready to pass to storeCachedToken.
func parseCacheRequest(w http.ResponseWriter, r *http.Request, clientID string) (bucket, key string, requested, ok bool) {
	if !r.URL.Query().Has("cache") {
		return "", "", false, true
	}

	bucket, err := resolveCacheBucket(r.Context())
	if err != nil {
		http.Error(
			w,
			"token cache bucket is not configured: "+err.Error(),
			http.StatusInternalServerError,
		)
		return "", "", true, false
	}

	return bucket, cacheObjectKey(clientID), true, true
}

// resolveCacheBucket derives this deployment's tenant-scoped token
// cache bucket. There is no bucket name in configuration to read - see
// the tenantstorage package doc for why.
func resolveCacheBucket(ctx context.Context) (string, error) {
	return tenantstorage.ResolveCacheBucket(ctx)
}

// cachedToken is the on-disk cache format: a GitHub token plus the
// absolute times it expires at. GitHub's TokenResponse only reports
// expires_in/refresh_token_expires_in relative to the moment it was
// issued, so those have to be converted to absolute timestamps at
// write time - a later reader has no other way to tell if they've
// lapsed.
type cachedToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type,omitempty"`
	Scope        string `json:"scope,omitempty"`

	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at,omitempty"`
}

func newCachedToken(token *ghdeviceflow.TokenResponse, issuedAt time.Time) cachedToken {
	entry := cachedToken{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Scope:        token.Scope,
	}

	if token.ExpiresIn > 0 {
		entry.AccessTokenExpiresAt = issuedAt.Add(time.Duration(token.ExpiresIn) * time.Second)
	}

	if token.RefreshTokenExpiresIn > 0 {
		entry.RefreshTokenExpiresAt = issuedAt.Add(time.Duration(token.RefreshTokenExpiresIn) * time.Second)
	}

	return entry
}

// AccessTokenValid reports whether the access token is still usable at
// now, with buffer of headroom required before expiry. A zero/unknown
// expiry is treated as invalid - this repo's flow always expects GitHub
// to report one, so an absent value means something's off and is worth
// a refresh check rather than trusting a token of unknown age.
func (c cachedToken) AccessTokenValid(now time.Time, buffer time.Duration) bool {
	if c.AccessTokenExpiresAt.IsZero() {
		return false
	}

	return now.Add(buffer).Before(c.AccessTokenExpiresAt)
}

// RefreshTokenValid reports whether the refresh token is still usable
// at now. A zero/unknown expiry is treated as valid (fail open): if
// it's actually bad, GitHub will say so immediately via
// ErrInvalidRefreshToken when it's used.
func (c cachedToken) RefreshTokenValid(now time.Time) bool {
	if c.RefreshTokenExpiresAt.IsZero() {
		return true
	}

	return now.Before(c.RefreshTokenExpiresAt)
}

type tokenResponsePublic struct {
	AccessToken          string    `json:"access_token"`
	AccessTokenExpiresAt time.Time `json:"access_token_expires_at,omitempty"`
}

// publicResponse is what a caller of /auth/github/token gets back -
// never the refresh token, which stays purely a server-side cache
// concern once caching is in play.
func (c cachedToken) publicResponse() tokenResponsePublic {
	return tokenResponsePublic{
		AccessToken:          c.AccessToken,
		AccessTokenExpiresAt: c.AccessTokenExpiresAt,
	}
}

func loadCachedToken(ctx context.Context, bucket, key string) (cachedToken, error) {
	var cached cachedToken
	err := tokencache.Load(ctx, bucket, key, &cached)
	return cached, err
}

func storeCachedToken(ctx context.Context, bucket, key string, cached cachedToken) error {
	return tokencache.Store(ctx, bucket, key, cached)
}

// tokenAction is the pure decision of what /auth/github/token should do
// with a cached entry, kept separate from any I/O so it can be unit
// tested directly.
type tokenAction int

const (
	actionServeCached tokenAction = iota
	actionRefresh
	actionReauthRequired
)

func decideTokenAction(cached cachedToken, now time.Time) tokenAction {
	if cached.AccessTokenValid(now, accessTokenRefreshBuffer) {
		return actionServeCached
	}

	if !cached.RefreshTokenValid(now) {
		return actionReauthRequired
	}

	return actionRefresh
}
