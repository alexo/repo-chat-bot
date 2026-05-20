// Package s3 implements kbsync's provider.Provider against S3 and
// S3-compatible services (MinIO, Cloudflare R2, OCI's S3-compat endpoint).
package s3

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alexo/repo-chat-bot/kbsync/provider"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Config drives the S3 provider. Credentials come from the standard AWS
// chain (env vars, ~/.aws/credentials, IAM role) — we do not parse them
// ourselves. Endpoint overrides let this reach S3-compatible services.
type Config struct {
	Bucket   string
	Prefix   string
	Region   string
	Endpoint string // optional; e.g. "https://<account>.r2.cloudflarestorage.com"
}

type Provider struct {
	cfg    Config
	client *s3.Client
}

func NewProvider(ctx context.Context, c Config) (*Provider, error) {
	if c.Bucket == "" {
		return nil, fmt.Errorf("kbsync/s3: bucket required")
	}
	opts := []func(*config.LoadOptions) error{}
	if c.Region != "" {
		opts = append(opts, config.WithRegion(c.Region))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("kbsync/s3: aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if c.Endpoint != "" {
			o.BaseEndpoint = aws.String(c.Endpoint)
			o.UsePathStyle = true
		}
	})
	return &Provider{cfg: c, client: client}, nil
}

func (p *Provider) List(ctx context.Context) ([]provider.ObjectMeta, error) {
	var out []provider.ObjectMeta
	in := &s3.ListObjectsV2Input{
		Bucket: aws.String(p.cfg.Bucket),
	}
	if p.cfg.Prefix != "" {
		in.Prefix = aws.String(p.cfg.Prefix)
	}
	pager := s3.NewListObjectsV2Paginator(p.client, in)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			key := aws.ToString(o.Key)
			// Skip "directory" markers (zero-byte keys ending in /).
			if strings.HasSuffix(key, "/") {
				continue
			}
			out = append(out, provider.ObjectMeta{
				Key:  strings.TrimPrefix(key, p.cfg.Prefix),
				ETag: strings.Trim(aws.ToString(o.ETag), `"`),
				Size: aws.ToInt64(o.Size),
			})
		}
	}
	return out, nil
}

func (p *Provider) Download(ctx context.Context, key string, w io.Writer) error {
	resp, err := p.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(p.cfg.Bucket),
		Key:    aws.String(p.cfg.Prefix + key),
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(w, resp.Body)
	return err
}

// ConfigFromEnv reads provider config from environment variables.
func ConfigFromEnv() Config {
	return Config{
		Bucket:   os.Getenv("KB_STORAGE_BUCKET"),
		Prefix:   os.Getenv("KB_STORAGE_PREFIX"),
		Region:   os.Getenv("KB_STORAGE_REGION"),
		Endpoint: os.Getenv("KB_STORAGE_ENDPOINT"),
	}
}
