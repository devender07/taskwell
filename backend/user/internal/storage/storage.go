// Package storage keeps uploaded files in S3 or an S3-compatible store (MinIO, R2), one object per file named by its
// uuid. Every setting comes from the s3_* keys in system_config, so admin edits apply without a restart.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
)

// urlTTL is how long a presigned URL works. CacheFor is how long a redirect to one may be cached: less, so a cached
// redirect never points at an expired URL.
// putTTL is how long a presigned PUT works.
const (
	urlTTL   = time.Hour
	CacheFor = 50 * time.Minute
	putTTL   = 15 * time.Minute
)

// cacheControl is what every object is stored with.
var cacheControl = fmt.Sprintf("private, max-age=%d", int(CacheFor.Seconds()))

var keys = [...]string{"s3_endpoint", "s3_region", "s3_bucket", "s3_access_key_id", "s3_secret_access_key"}

type Store struct {
	cfg    *sysconfig.Store
	mu     sync.Mutex
	conf   [len(keys)]string
	client *s3.Client
}

func New(cfg *sysconfig.Store) *Store { return &Store{cfg: cfg} }

// bucket returns a client for the current settings, building a new one when they changed, and the bucket name.
func (s *Store) bucket(ctx context.Context) (*s3.Client, string, error) {
	var c [len(keys)]string
	for i, k := range keys {
		v, err := s.cfg.Get(ctx, k)
		if err != nil {
			return nil, "", err
		}
		c[i] = v
	}
	endpoint, region, bucket, keyID, secret := c[0], c[1], c[2], c[3], c[4]
	if bucket == "" {
		return nil, "", errors.New("file storage not configured: set s3_bucket in system config")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil || c != s.conf {
		o := s3.Options{
			Region:      region,
			Credentials: credentials.NewStaticCredentialsProvider(keyID, secret, ""),
			// Only what S3 requires: some compatible stores reject the SDK's default checksums.
			RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
			ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
		}
		if endpoint != "" {
			o.BaseEndpoint, o.UsePathStyle = aws.String(endpoint), true
		}
		s.client, s.conf = s3.New(o), c
	}
	return s.client, bucket, nil
}

// Put stores body as the object id. The headers are stored with it, not set when presigning: some S3-compatible
// stores ignore response-content-disposition.
func (s *Store) Put(ctx context.Context, id, contentType, disposition string, body io.ReadSeeker) error {
	c, bucket, err := s.bucket(ctx)
	if err != nil {
		return err
	}
	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &id, Body: body, ContentType: &contentType,
		ContentDisposition: &disposition, CacheControl: &cacheControl})
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

// SetHeaders replaces the object id's stored headers, as after a rename, by storing its bytes again.
// ponytail: streams the object through a temp file because the dev store (SeaweedFS) ignores CopyObject's
// MetadataDirective REPLACE; use CopyObject once every store in use supports it.
func (s *Store) SetHeaders(ctx context.Context, id, contentType, disposition string) error {
	c, bucket, err := s.bucket(ctx)
	if err != nil {
		return err
	}
	obj, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &id})
	if err != nil {
		return fmt.Errorf("get object: %w", err)
	}
	defer obj.Body.Close()
	tmp, err := os.CreateTemp("", "taskwell-object-")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // best effort
	defer tmp.Close()
	if _, err := io.Copy(tmp, obj.Body); err != nil {
		return fmt.Errorf("read object: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind object: %w", err)
	}
	return s.Put(ctx, id, contentType, disposition, tmp)
}

// Delete removes the object id; a missing one is not an error.
func (s *Store) Delete(ctx context.Context, id string) error {
	c, bucket, err := s.bucket(ctx)
	if err != nil {
		return err
	}
	if _, err := c.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &id}); err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

// URL returns a presigned GET for the object id.
func (s *Store) URL(ctx context.Context, id string) (string, error) {
	c, bucket, err := s.bucket(ctx)
	if err != nil {
		return "", err
	}
	req, err := s3.NewPresignClient(c).PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &id},
		s3.WithPresignExpires(urlTTL))
	if err != nil {
		return "", fmt.Errorf("presign object: %w", err)
	}
	return req.URL, nil
}

// Old calls fn with the keys starting with prefix of objects last modified before cutoff, a page (up to 1000) at a time.
func (s *Store) Old(ctx context.Context, prefix string, cutoff time.Time, fn func(keys []string) error) error {
	c, bucket, err := s.bucket(ctx)
	if err != nil {
		return err
	}
	p := s3.NewListObjectsV2Paginator(c, &s3.ListObjectsV2Input{Bucket: &bucket, Prefix: &prefix})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list objects: %w", err)
		}
		var old []string
		for _, o := range page.Contents {
			if o.Key != nil && o.LastModified != nil && o.LastModified.Before(cutoff) {
				old = append(old, *o.Key)
			}
		}
		if len(old) > 0 {
			if err := fn(old); err != nil {
				return err
			}
		}
	}
	return nil
}

// StagePrefix keys a direct upload's bytes until Promote moves them to the file's id: the client only ever gets a
// PUT for the staged key, so once the API has checked a file nobody can change it.
const StagePrefix = "uploads/"

// PresignPut returns a presigned PUT for the upload id's staged key and the headers the client must send with it (the
// client sets Content-Length itself; it's signed too).
func (s *Store) PresignPut(ctx context.Context, id, contentType, disposition string, size int64) (string, map[string]string, error) {
	c, bucket, err := s.bucket(ctx)
	if err != nil {
		return "", nil, err
	}
	key := StagePrefix + id
	req, err := s3.NewPresignClient(c).PresignPutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &key,
		ContentType: &contentType, ContentLength: &size, ContentDisposition: &disposition, CacheControl: &cacheControl},
		s3.WithPresignExpires(putTTL))
	if err != nil {
		return "", nil, fmt.Errorf("presign put: %w", err)
	}
	return req.URL, map[string]string{"Content-Type": contentType, "Content-Disposition": disposition,
		"Cache-Control": cacheControl}, nil
}

// Promote copies the upload id's staged object (with its headers) to the object id and deletes the staged one. It
// reports false when nothing is staged.
func (s *Store) Promote(ctx context.Context, id string) (bool, error) {
	c, bucket, err := s.bucket(ctx)
	if err != nil {
		return false, err
	}
	_, err = c.CopyObject(ctx, &s3.CopyObjectInput{Bucket: &bucket, Key: &id, CopySource: aws.String(bucket + "/" + StagePrefix + id)})
	var nk *types.NoSuchKey
	var api smithy.APIError
	if errors.As(err, &nk) || errors.As(err, &api) && api.ErrorCode() == "NoSuchKey" {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("copy object: %w", err)
	}
	if err := s.Delete(ctx, StagePrefix+id); err != nil {
		slog.Warn("delete staged upload", "id", id, "err", err) // the sweep removes it later
	}
	return true, nil
}

// Object is what Head reports of a stored object.
type Object struct {
	Size        int64
	ContentType string
	Disposition string
}

// Head returns the object id's size and stored headers, nil when it doesn't exist.
func (s *Store) Head(ctx context.Context, id string) (*Object, error) {
	c, bucket, err := s.bucket(ctx)
	if err != nil {
		return nil, err
	}
	out, err := c.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &id})
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("head object: %w", err)
	}
	return &Object{Size: aws.ToInt64(out.ContentLength), ContentType: aws.ToString(out.ContentType),
		Disposition: aws.ToString(out.ContentDisposition)}, nil
}

// Get streams the object id; nil when it doesn't exist. The caller closes it.
func (s *Store) Get(ctx context.Context, id string) (io.ReadCloser, error) {
	c, bucket, err := s.bucket(ctx)
	if err != nil {
		return nil, err
	}
	obj, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &id})
	var nk *types.NoSuchKey
	var api smithy.APIError
	if errors.As(err, &nk) || errors.As(err, &api) && api.ErrorCode() == "NoSuchKey" {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get object: %w", err)
	}
	return obj.Body, nil
}

// Start returns up to the first n bytes of the object id.
func (s *Store) Start(ctx context.Context, id string, n int) ([]byte, error) {
	c, bucket, err := s.bucket(ctx)
	if err != nil {
		return nil, err
	}
	obj, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &id, Range: aws.String(fmt.Sprintf("bytes=0-%d", n-1))})
	if err != nil {
		return nil, fmt.Errorf("get object start: %w", err)
	}
	defer obj.Body.Close()
	b, err := io.ReadAll(io.LimitReader(obj.Body, int64(n)))
	if err != nil {
		return nil, fmt.Errorf("read object start: %w", err)
	}
	return b, nil
}

// AllowOrigin sets the bucket's CORS rules so the web app at origin can PUT (direct uploads) and GET its objects.
func (s *Store) AllowOrigin(ctx context.Context, origin string) error {
	c, bucket, err := s.bucket(ctx)
	if err != nil {
		return err
	}
	_, err = c.PutBucketCors(ctx, &s3.PutBucketCorsInput{Bucket: &bucket, CORSConfiguration: &types.CORSConfiguration{
		CORSRules: []types.CORSRule{{AllowedOrigins: []string{origin}, AllowedMethods: []string{"PUT", "GET"},
			AllowedHeaders: []string{"*"}, MaxAgeSeconds: aws.Int32(3600)}}}})
	if err != nil {
		return fmt.Errorf("put bucket cors: %w", err)
	}
	return nil
}
