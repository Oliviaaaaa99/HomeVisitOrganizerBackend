// Package clients wraps S3 (or LocalStack) for media-svc.
package clients

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Config holds the parameters for an S3 client.
type Config struct {
	Region       string
	Bucket       string
	Endpoint     string // empty for real AWS; "http://localhost:4566" for LocalStack
	AccessKey    string // empty in prod (rely on IAM role); set for LocalStack
	SecretKey    string
	UsePathStyle bool // true for LocalStack
	PresignTTL   time.Duration
}

// S3 wraps the AWS SDK v2 client and the bucket name for our use cases.
type S3 struct {
	cli        *s3.Client
	presign    *s3.PresignClient
	bucket     string
	endpoint   string // empty for real AWS; the LocalStack URL for dev
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

// EnsureBucket creates the bucket if it doesn't already exist AND applies a
// public-read policy (dev convenience for LocalStack — lets the mobile client
// load images by direct URL without signing every GET).
//
// On real AWS this should be a Terraform/CDK-managed resource with a private
// bucket + CloudFront + signed cookies. We still issue presigned PUTs for
// uploads either way; the public-read here is only about GETting back what's
// already public-by-intent (a user's own photos in the dev sandbox).
func (s *S3) EnsureBucket(ctx context.Context) error {
	_, err := s.cli.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		// Already-exists is fine.
		var owned *types.BucketAlreadyOwnedByYou
		var exists *types.BucketAlreadyExists
		if !(errors.As(err, &owned) || errors.As(err, &exists) || isAlreadyExists(err)) {
			return fmt.Errorf("ensure bucket %q: %w", s.bucket, err)
		}
	}

	// Best-effort public-read policy. LocalStack accepts this; on real AWS the
	// bucket would be CDK-managed and we'd skip this codepath entirely.
	policy := fmt.Sprintf(`{
		"Version": "2012-10-17",
		"Statement": [{
			"Sid": "PublicRead",
			"Effect": "Allow",
			"Principal": "*",
			"Action": "s3:GetObject",
			"Resource": "arn:aws:s3:::%s/*"
		}]
	}`, s.bucket)
	if _, perr := s.cli.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(s.bucket),
		Policy: aws.String(policy),
	}); perr != nil {
		// Don't fail boot if policy set fails — uploads still work via presign.
		// Worst case images come back 403 and the UI shows the broken-image state.
		return nil
	}
	return nil
}

// PublicURL builds a direct-GET URL for an object key. Only meaningful when
// the bucket has a public-read policy (true in dev via LocalStack; in prod
// this should not be called — use a CloudFront/signed-cookie strategy).
func (s *S3) PublicURL(key string) string {
	if s.endpoint == "" {
		return fmt.Sprintf("https://%s.s3.amazonaws.com/%s", s.bucket, key)
	}
	// Path-style for LocalStack & co.
	return fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, key)
}

func isAlreadyExists(err error) bool {
	// LocalStack and some non-canonical S3 servers surface a string the SDK
	// doesn't map to a typed error. Best-effort substring match is fine here
	// since this code path is only reachable from EnsureBucket (dev convenience).
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "bucketalreadyownedbyyou") ||
		strings.Contains(msg, "bucketalreadyexists")
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

// HeadObject returns nil if the object exists, error otherwise. Used by the
// commit endpoint to confirm the client actually uploaded what they claim.
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

// DeleteObject removes an object — used by the soft-delete grace expiry job
// (M4) and by user-initiated deletes when we choose hard-delete semantics.
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

// PresignTTL returns the configured presign expiry (for echoing back to the
// client so it knows the deadline).
func (s *S3) PresignTTL() time.Duration { return s.presignTTL }
