// Package tokencache writes JSON-serializable values to Google Cloud
// Storage, for callers that want a minted GitHub token handed to a
// private bucket instead of returned in an HTTP response.
package tokencache

import (
	"context"
	"encoding/json"
	"fmt"

	"cloud.google.com/go/storage"
)

// Store JSON-encodes value and writes it as an object to
// gs://bucket/object using Application Default Credentials. The caller's
// runtime identity must already have write access to the bucket (e.g.
// roles/storage.objectUser or roles/storage.objectCreator granted at the
// bucket level, not the project level) - this package never handles a
// service-account key itself.
func Store(ctx context.Context, bucket, object string, value any) error {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("creating storage client: %w", err)
	}
	defer client.Close()

	w := client.Bucket(bucket).Object(object).NewWriter(ctx)
	w.ContentType = "application/json"

	if err := json.NewEncoder(w).Encode(value); err != nil {
		_ = w.Close()
		return fmt.Errorf("encoding cached value: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("writing object: %w", err)
	}

	return nil
}

// Load reads the object at gs://bucket/object and JSON-decodes it into
// dest, using the same Application Default Credentials as Store. If the
// object doesn't exist, the returned error wraps storage.ErrObjectNotExist
// (checkable with errors.Is), so callers can distinguish "nothing cached
// yet" from other failures.
func Load(ctx context.Context, bucket, object string, dest any) error {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("creating storage client: %w", err)
	}
	defer client.Close()

	r, err := client.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		return fmt.Errorf("reading object: %w", err)
	}
	defer r.Close()

	if err := json.NewDecoder(r).Decode(dest); err != nil {
		return fmt.Errorf("decoding cached value: %w", err)
	}

	return nil
}
