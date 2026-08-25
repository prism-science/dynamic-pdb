package s3

import "time"

type BucketConfig struct {
	Endpoint          string        `mapstructure:"endpoint"`
	Region            string        `mapstructure:"region"`
	Bucket            string        `mapstructure:"bucket"`
	AccessKeyID       string        `mapstructure:"access_key_id"`
	SecretAccessKey   string        `mapstructure:"secret_access_key"`
	UploadMaxFileSize int64         `mapstructure:"upload_max_file_size"`
	UploadURLTTL      time.Duration `mapstructure:"upload_url_ttl"`
}
