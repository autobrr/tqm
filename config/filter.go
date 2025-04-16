package config

type FilterConfiguration struct {
	MapHardlinksFor []string
	Ignore          []string
	Remove          []string
	Pause           []string // Added for pause functionality
	DeleteData      *bool
	Label           []struct {
		Name   string
		Update []string
	}
	Tag []struct {
		Name   string
		Mode   string
		Update []string
	}
}
