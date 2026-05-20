// Package provider defines the storage-backend abstraction used by kbsync.
// Concrete backends live in subpackages (provider/s3, provider/oci).
package provider

import (
	"context"
	"io"
)

// ObjectMeta describes a single remote object. ETag is opaque — any string
// that uniquely changes when the object's contents change is fine (the S3
// ETag header and OCI's Object md5 are both suitable).
type ObjectMeta struct {
	Key  string
	ETag string
	Size int64
}

// Provider abstracts the object-storage backend that kbsync mirrors.
type Provider interface {
	List(ctx context.Context) ([]ObjectMeta, error)
	Download(ctx context.Context, key string, w io.Writer) error
}
