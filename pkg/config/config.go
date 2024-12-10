package config

import (
	"fmt"

	"github.com/autobrr/tqm/pkg/logger"
	"github.com/autobrr/tqm/pkg/stringutils"
	"github.com/autobrr/tqm/pkg/tracker"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
)

type Configuration struct {
	Clients                    map[string]map[string]interface{}
	Filters                    map[string]FilterConfiguration
	Jobs                       Jobs
	Trackers                   tracker.Config
	BypassIgnoreIfUnregistered bool
}

/* Vars */

var (
	cfgPath = ""

	Delimiter = "."
	Config    *Configuration
	K         = koanf.New(Delimiter)

	// Internal
	log = logger.GetLogger("cfg")
)

/* Public */

func Init(configFilePath string) error {
	// set package variables
	cfgPath = configFilePath

	// load config
	if err := K.Load(file.Provider(configFilePath), yaml.Parser()); err != nil {
		return fmt.Errorf("load: %w", err)
	}

	// unmarshal config
	if err := K.Unmarshal("", &Config); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	return nil
}

func ShowUsing() {
	log.Infof("Using %s = %q", stringutils.LeftJust("CONFIG", " ", 10), cfgPath)

}
