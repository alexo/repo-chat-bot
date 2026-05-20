// Package oci implements kbsync's provider.Provider against OCI Object Storage.
package oci

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alexo/repo-chat-bot/kbsync/provider"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/objectstorage"
)

// Config drives the OCI Object Storage provider. Auth comes from one of
// three places, in order of precedence:
//
//  1. Auth == "instance" — use instance principal (works on OCI compute VMs
//     with no config file required).
//  2. ConfigFile / Profile set — read those from disk.
//  3. Default chain (~/.oci/config with DEFAULT profile, or OCI_CONFIG_FILE).
type Config struct {
	Namespace  string
	Bucket     string
	Prefix     string
	Region     string // overrides region from config file if set
	Auth       string // "instance" | "config" (default "config")
	ConfigFile string
	Profile    string
}

type Provider struct {
	cfg    Config
	client objectstorage.ObjectStorageClient
}

func NewProvider(ctx context.Context, c Config) (*Provider, error) {
	if c.Bucket == "" || c.Namespace == "" {
		return nil, fmt.Errorf("kbsync/oci: namespace and bucket required")
	}

	var auther common.ConfigurationProvider
	switch c.Auth {
	case "instance":
		p, err := auth.InstancePrincipalConfigurationProvider()
		if err != nil {
			return nil, fmt.Errorf("kbsync/oci: instance principal: %w", err)
		}
		auther = p
	default:
		switch {
		case c.ConfigFile != "":
			profile := c.Profile
			if profile == "" {
				profile = "DEFAULT"
			}
			p, err := common.ConfigurationProviderFromFileWithProfile(c.ConfigFile, profile, "")
			if err != nil {
				return nil, fmt.Errorf("kbsync/oci: config file: %w", err)
			}
			auther = p
		default:
			auther = common.DefaultConfigProvider()
		}
	}

	client, err := objectstorage.NewObjectStorageClientWithConfigurationProvider(auther)
	if err != nil {
		return nil, fmt.Errorf("kbsync/oci: client: %w", err)
	}
	if c.Region != "" {
		client.SetRegion(c.Region)
	}
	return &Provider{cfg: c, client: client}, nil
}

func (p *Provider) List(ctx context.Context) ([]provider.ObjectMeta, error) {
	var out []provider.ObjectMeta
	var start *string
	for {
		req := objectstorage.ListObjectsRequest{
			NamespaceName: &p.cfg.Namespace,
			BucketName:    &p.cfg.Bucket,
			Fields:        common.String("name,size,etag"),
			Start:         start,
		}
		if p.cfg.Prefix != "" {
			req.Prefix = &p.cfg.Prefix
		}
		resp, err := p.client.ListObjects(ctx, req)
		if err != nil {
			return nil, err
		}
		for _, o := range resp.Objects {
			name := ""
			if o.Name != nil {
				name = *o.Name
			}
			if strings.HasSuffix(name, "/") {
				continue
			}
			etag := ""
			if o.Etag != nil {
				etag = *o.Etag
			}
			var size int64
			if o.Size != nil {
				size = *o.Size
			}
			out = append(out, provider.ObjectMeta{
				Key:  strings.TrimPrefix(name, p.cfg.Prefix),
				ETag: etag,
				Size: size,
			})
		}
		if resp.NextStartWith == nil || *resp.NextStartWith == "" {
			break
		}
		start = resp.NextStartWith
	}
	return out, nil
}

func (p *Provider) Download(ctx context.Context, key string, w io.Writer) error {
	full := p.cfg.Prefix + key
	resp, err := p.client.GetObject(ctx, objectstorage.GetObjectRequest{
		NamespaceName: &p.cfg.Namespace,
		BucketName:    &p.cfg.Bucket,
		ObjectName:    &full,
	})
	if err != nil {
		return err
	}
	defer resp.Content.Close()
	_, err = io.Copy(w, resp.Content)
	return err
}

// ConfigFromEnv reads provider config from environment variables.
func ConfigFromEnv() Config {
	return Config{
		Namespace:  os.Getenv("KB_OCI_NAMESPACE"),
		Bucket:     os.Getenv("KB_STORAGE_BUCKET"),
		Prefix:     os.Getenv("KB_STORAGE_PREFIX"),
		Region:     os.Getenv("KB_STORAGE_REGION"),
		Auth:       os.Getenv("KB_OCI_AUTH"),
		ConfigFile: os.Getenv("KB_OCI_CONFIG_FILE"),
		Profile:    os.Getenv("KB_OCI_PROFILE"),
	}
}
