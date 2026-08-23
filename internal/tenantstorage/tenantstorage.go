// Package tenantstorage derives the GCS bucket a deployment should use
// for its GitHub token cache, instead of that bucket name being handed
// to it directly as an environment variable.
//
// The problem with an explicit GITHUB_TOKEN_CACHE_BUCKET-style env var
// in a multi-tenant setup: it has to be re-entered, correctly, on every
// single deploy, and nothing stops one deployment's config from
// accidentally pointing at another tenant's bucket. Since GCS has no
// reverse lookup from "the service account running this code" to "the
// bucket it's scoped to", the fix is to make the bucket name a pure,
// deterministic function of two things a deployment already knows (or
// can discover) rather than a third thing it has to be separately told:
//
//   - TENANT_ID: the provisioning layer's own canonical identifier for
//     this tenant. It's set once and shared across every function that
//     tenant's runtime service account runs - not a per-function or
//     per-bucket value - so it changes far less often than a bucket name
//     would. It is deliberately NOT the service account's email: coupling
//     bucket naming to an IAM identifier only adds GCS naming-constraint
//     friction for no benefit, and makes the naming scheme harder to
//     reproduce from Terraform/bootstrap tooling than a tenant ID that
//     provisioning already owns as a first-class concept.
//   - the runtime region: discovered from the GCE metadata server rather
//     than configured, so it always matches wherever this instance is
//     actually running - there's no separate region setting that can
//     drift from reality.
//
// terraform/tenant-token-cache (dash-xd/huram-abi) is the other half of
// this contract: given the same tenant_id and region, it provisions a
// bucket with exactly the name BucketName computes here, and grants that
// tenant's runtime service account access to it and nothing else. As
// long as a deployment's service account is the one that module output,
// and TENANT_ID matches what was passed to it, the bucket this package
// resolves is guaranteed to be one that identity can actually read and
// write - there is no bucket name for a deploy step to get wrong.
package tenantstorage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"cloud.google.com/go/compute/metadata"
)

const (
	// tenantIDEnvVar identifies this deployment's tenant. Unlike a
	// bucket name, this is the same across every function a tenant
	// runs, so it's set once per tenant by the provisioning layer
	// rather than reinserted per deployment.
	tenantIDEnvVar = "TENANT_ID"

	// regionEnvVar is a local-development / off-GCE fallback only. A
	// real deployment never needs to set this: its region is
	// discovered from the metadata server, so it's always correct for
	// wherever the code is actually running.
	regionEnvVar = "REGION"

	// defaultPurpose distinguishes this cache's bucket from any other
	// bucket the same tenant might have in the same region for a
	// different purpose. Matches terraform/tenant-token-cache's
	// default `purpose` variable.
	defaultPurpose = "github-token-cache"
)

// ErrTenantIDNotConfigured is returned when TENANT_ID isn't set.
var ErrTenantIDNotConfigured = errors.New("TENANT_ID is not configured")

// BucketName is the naming convention shared with
// terraform/tenant-token-cache: {tenant_id}-{region}-{purpose}. It's a
// pure function so both sides (Terraform provisioning the bucket, and
// this package resolving its name at request time) can compute the
// identical name from the identical inputs without either one telling
// the other what it picked.
func BucketName(tenantID, region, purpose string) string {
	if purpose == "" {
		purpose = defaultPurpose
	}

	return fmt.Sprintf("%s-%s-%s", tenantID, region, purpose)
}

// ResolveCacheBucket derives this deployment's token-cache bucket name
// from TENANT_ID and the runtime region. It never reads a bucket name
// from configuration - see the package doc for why.
func ResolveCacheBucket(ctx context.Context) (string, error) {
	tenantID := os.Getenv(tenantIDEnvVar)
	if tenantID == "" {
		return "", ErrTenantIDNotConfigured
	}

	region, err := resolveRegion(ctx)
	if err != nil {
		return "", err
	}

	return BucketName(tenantID, region, defaultPurpose), nil
}

// resolveRegion prefers the REGION env var only because local
// development (gospace-minimal's cmd/localserve, go test) never runs on
// GCE and so has no metadata server to ask. Any real deployment should
// leave REGION unset and let the metadata server answer, since that's
// the only source that's guaranteed to match where the code is actually
// colocated with its bucket.
func resolveRegion(ctx context.Context) (string, error) {
	if region := os.Getenv(regionEnvVar); region != "" {
		return region, nil
	}

	if !metadata.OnGCE() {
		return "", fmt.Errorf("determining runtime region: not running on GCE and %s is not set", regionEnvVar)
	}

	client := metadata.NewClient(nil)

	// The metadata server reports this as a full resource name, e.g.
	// "projects/123456789012/regions/us-central1" - only the trailing
	// segment is the region identifier Terraform/GCS naming expects.
	raw, err := client.GetWithContext(ctx, "instance/region")
	if err != nil {
		return "", fmt.Errorf("reading instance/region from the metadata server: %w", err)
	}

	region := raw[strings.LastIndex(raw, "/")+1:]
	if region == "" {
		return "", fmt.Errorf("unexpected instance/region value from the metadata server: %q", raw)
	}

	return region, nil
}
