// Package clients wraps S3 (or LocalStack) for user-svc avatars.
//
// This is a near-copy of media-svc's clients/s3.go. We're copying instead of
// extracting into shared/s3x for now because:
//
//   - The two services may diverge (avatars want a smaller TTL, different
//     bucket policy, etc) and pre-emptive sharing is the wrong abstraction
//     given the tiny surface area.
//   - Pulling a shared package needs a coordinated multi-service PR.
//
// Revisit if a third service needs S3 access.
package clients

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Config holds the parameters for an S3 client.
type Config struct {
	Region       string
	Bucket       string
	Endpoint     string // empty for real AWS; "http://localhost:4566" for LocalStack
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
	PresignTTL   time.Duration
}

// S3 wraps the AWS SDK v2 client and bucket name for our use cases.
type S3 struct {
	cli        *s3.Client
	presign    *s3.PresignClient
	bucket     string
	endpoint   string
	presignTTL time.Duration
}

// NewS3 constructs an S3 client. If `Endpoint` is set we configure for
// LocalStack-style usage (path addressing + dummy creds).
func NewS3(ctx context.Context, cfg Config) (*S3, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("s3 bucket required")
	}
	if cfg.PresignTTL == 0 {
		cfg.PresignTTL = 5 * time.Minute
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}
	if cfg.AccessKey != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	cli := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		if cfg.UsePathStyle {
			o.UsePathStyle = true
		}
	})

	return &S3{
		cli:        cli,
		presign:    s3.NewPresignClient(cli),
		bucket:     cfg.Bucket,
		endpoint:   cfg.Endpoint,
		presignTTL: cfg.PresignTTL,
	}, nil
}

// PresignPut returns a presigned URL the client can PUT to directly.
func (s *S3) PresignPut(ctx context.Context, key string) (string, error) {
	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(s.presignTTL))
	if err != nil {
		return "", fmt.Errorf("presign put: %w", err)
	}
	return req.URL, nil
}

// HeadObject returns nil if the object exists, error otherwise.
func (s *S3) HeadObject(ctx context.Context, key string) error {
	_, err := s.cli.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("head object %q: %w", key, err)
	}
	return nil
}

// DeleteObject removes an object — used when a user replaces their avatar.
func (s *S3) DeleteObject(ctx context.Context, key string) error {
	_, err := s.cli.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete object %q: %w", key, err)
	}
	return nil
}

// PublicURL builds a direct-GET URL for an object key. Assumes the bucket
// already has a public-read policy (set by media-svc.EnsureBucket in dev;
// CDK-managed in prod). user-svc does NOT manage the bucket policy.
func (s *S3) PublicURL(key string) string {
	if s.endpoint == "" {
		return fmt.Sprintf("https://%s.s3.amazonaws.com/%s", s.bucket, key)
	}
	return fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, key)
}

// presignGetTTL — see media-svc's matching constant. 1h is long enough for an
// iOS session, short enough that a leaked URL stops working before the user
// closes the app.
const presignGetTTL = 1 * time.Hour

// PresignGet returns a short-lived signed URL for fetching the avatar. Use
// this instead of PublicURL when the bucket is private.
func (s *S3) PresignGet(ctx context.Context, key string) (string, error) {
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(presignGetTTL))
	if err != nil {
		return "", fmt.Errorf("presign get: %w", err)
	}
	return req.URL, nil
}

// PresignTTL returns the configured presign expiry.
func (s *S3) PresignTTL() time.Duration { return s.presignTTL }
