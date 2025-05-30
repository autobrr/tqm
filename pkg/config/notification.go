package config

type NotificationsConfig struct {
	Detailed     bool
	SkipEmptyRun bool `yaml:"skip_empty_run" koanf:"skip_empty_run"`
	Service      NotificationService
}

type NotificationService struct {
	Discord DiscordConfig `yaml:"discord" koanf:"discord"`
}

type DiscordConfig struct {
	WebhookURL string `yaml:"webhook_url" koanf:"webhook_url"`
	Username   string `yaml:"username" koanf:"username"`
	AvatarURL  string `yaml:"avatar_url" koanf:"avatar_url"`
}
