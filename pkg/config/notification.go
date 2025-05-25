package config

type NotificationsConfig struct {
	Detailed     bool
	SkipEmptyRun bool `yaml:"skip_empty_run" koanf:"skip_empty_run"`
	Service      NotificationService
}

type NotificationService struct {
	Discord string
}
