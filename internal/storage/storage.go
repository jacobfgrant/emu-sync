package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jacobfgrant/emu-sync/internal/config"
)

const ManifestKey = "emu-sync-manifest.json"

// Backend defines the operations that upload and sync workflows need.
// storage.Client implements this; tests can substitute a mock.
type Backend interface {
	Ping(ctx context.Context) error
	UploadFile(ctx context.Context, key, localPath string) error
	UploadBytes(ctx context.Context, key string, data []byte) error
	DownloadFile(ctx context.Context, key, localPath string) error
	DownloadBytes(ctx context.Context, key string) ([]byte, error)
	DeleteObject(ctx context.Context, key string) error
	DownloadManifest(ctx context.Context) ([]byte, error)
	UploadManifest(ctx context.Context, data []byte) error
}

// Client wraps an S3 client for bucket operations.
type Client struct {
	s3     *s3.Client
	bucket string
}

// NewClient creates a storage client from config.
func NewClient(cfg *config.StorageConfig) *Client {
	opts := s3.Options{
		Region:       cfg.Region,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.KeyID, cfg.SecretKey, ""),
		UsePathStyle: true,
	}
	if cfg.EndpointURL != "" {
		opts.BaseEndpoint = aws.String(cfg.EndpointURL)
	}

	return &Client{
		s3:     s3.New(opts),
		bucket: cfg.Bucket,
	}
}

// Ping verifies that the credentials and bucket are valid.
// Uses ListObjectsV2 with MaxKeys=0 so it only requires the listFiles
// capability on B2, which emu-sync already needs for normal operation.
func (c *Client) Ping(ctx context.Context) error {
	maxKeys := int32(0)
	_, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(c.bucket),
		MaxKeys: &maxKeys,
	})
	if err != nil {
		return fmt.Errorf("verifying bucket access: %w", err)
	}
	return nil
}

// UploadFile uploads a local file to the given key in the bucket.
func (c *Client) UploadFile(ctx context.Context, key, localPath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", localPath, err)
	}
	defer f.Close()

	_, err = c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   f,
	})
	if err != nil {
		return fmt.Errorf("uploading %s: %w", key, err)
	}

	return nil
}

// UploadBytes uploads raw bytes to the given key.
func (c *Client) UploadBytes(ctx context.Context, key string, data []byte) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("uploading %s: %w", key, err)
	}

	return nil
}

// DownloadFile downloads an object to a local file path.
func (c *Client) DownloadFile(ctx context.Context, key, localPath string) error {
	result, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("downloading %s: %w", key, err)
	}
	defer result.Body.Close()

	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", localPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, result.Body); err != nil {
		return fmt.Errorf("writing %s: %w", localPath, err)
	}

	return nil
}

// DownloadBytes downloads an object and returns its contents as bytes.
func (c *Client) DownloadBytes(ctx context.Context, key string) ([]byte, error) {
	result, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", key, err)
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", key, err)
	}

	return data, nil
}

// DeleteObject deletes an object from the bucket.
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("deleting %s: %w", key, err)
	}

	return nil
}

// DownloadManifest downloads the remote manifest from the bucket.
func (c *Client) DownloadManifest(ctx context.Context) ([]byte, error) {
	return c.DownloadBytes(ctx, ManifestKey)
}

// UploadManifest uploads a manifest to the bucket.
func (c *Client) UploadManifest(ctx context.Context, data []byte) error {
	return c.UploadBytes(ctx, ManifestKey, data)
}
