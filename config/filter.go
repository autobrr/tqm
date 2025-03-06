package config

type FilterConfiguration struct {
	MapHardlinksFor []string
	Ignore          []string
	Remove          []string
	DeleteData      bool `yaml:"delete_data,omitempty" default:"true"`
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
