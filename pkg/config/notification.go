package config

type NotificationsConfig struct {
	Detailed bool
	Service  NotificationService
}

type NotificationService struct {
	Discord string
}
