package cdn

import "dynamic-pdb/backend/internal/integrations/s3"

type Config struct {
	S3         s3.BucketConfig  `mapstructure:"s3"`
	CloudFront CloudFrontConfig `mapstructure:"cloudfront"`
}

type CloudFrontConfig struct {
	BaseURL string `mapstructure:"base_url"`
}
