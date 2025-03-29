package config

type FilterConfiguration struct {
	MapHardlinksFor []string
	Ignore          []string
	Remove          []string
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
