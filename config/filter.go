package config

import "time"

type FilterConfiguration struct {
	MapHardlinksFor []string
	Ignore          []string
	Remove          []string
	Pause           []string
	DeleteData      *bool
	Orphan          struct {
		GracePeriod time.Duration `yaml:"grace_period" koanf:"grace_period"`
		IgnorePaths []string      `yaml:"ignore_paths" koanf:"ignore_paths"`
	} `yaml:"orphan" koanf:"orphan"`
	Label []struct {
		Name   string
		Update []string
	}
	Tag []struct {
		Name     string
		Mode     string
		UploadKb *int `mapstructure:"uploadKb"`
		Update   []string
	}
}
