package tenantstorage

import (
	"context"
	"errors"
	"testing"
)

func TestBucketName(t *testing.T) {
	got := BucketName("abc123", "us-central1", "")
	want := "abc123-us-central1-github-token-cache"

	if got != want {
		t.Errorf("BucketName() = %q, want %q", got, want)
	}
}

func TestBucketName_CustomPurpose(t *testing.T) {
	got := BucketName("abc123", "us-central1", "other-cache")
	want := "abc123-us-central1-other-cache"

	if got != want {
		t.Errorf("BucketName() = %q, want %q", got, want)
	}
}

func TestResolveCacheBucket_MissingTenantID(t *testing.T) {
	t.Setenv("TENANT_ID", "")
	t.Setenv("REGION", "us-central1")

	_, err := ResolveCacheBucket(context.Background())
	if !errors.Is(err, ErrTenantIDNotConfigured) {
		t.Fatalf("ResolveCacheBucket() error = %v, want ErrTenantIDNotConfigured", err)
	}
}

func TestResolveCacheBucket_UsesRegionEnvVarOffGCE(t *testing.T) {
	t.Setenv("TENANT_ID", "abc123")
	t.Setenv("REGION", "us-west1")

	got, err := ResolveCacheBucket(context.Background())
	if err != nil {
		t.Fatalf("ResolveCacheBucket() error = %v", err)
	}

	want := "abc123-us-west1-github-token-cache"
	if got != want {
		t.Errorf("ResolveCacheBucket() = %q, want %q", got, want)
	}
}

func TestResolveCacheBucket_MissingRegionOffGCE(t *testing.T) {
	t.Setenv("TENANT_ID", "abc123")
	t.Setenv("REGION", "")

	// This test only exercises the off-GCE path; on a real GCE/Cloud
	// Functions runtime metadata.OnGCE() would be true and the metadata
	// server would answer instead. CI runners aren't GCE, so this is
	// safe to assert unconditionally here.
	if _, err := ResolveCacheBucket(context.Background()); err == nil {
		t.Fatal("ResolveCacheBucket() error = nil, want an error when neither REGION nor GCE metadata is available")
	}
}
