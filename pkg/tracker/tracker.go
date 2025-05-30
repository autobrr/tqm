package tracker

var (
	trackers []Interface
)

func Init(cfg Config) error {
	trackers = make([]Interface, 0)

	// load trackers
	if cfg.BHD.Key != "" {
		trackers = append(trackers, NewBHD(cfg.BHD))
	}
	if cfg.BTN.Key != "" {
		trackers = append(trackers, NewBTN(cfg.BTN))
	}
	if cfg.PTP.User != "" && cfg.PTP.Key != "" {
		trackers = append(trackers, NewPTP(cfg.PTP))
	}
	if cfg.RED.Key != "" {
		trackers = append(trackers, NewRED(cfg.RED))
	}
	if cfg.OPS.Key != "" {
		trackers = append(trackers, NewOPS(cfg.OPS))
	}
	if cfg.HDB.Username != "" && cfg.HDB.Passkey != "" {
		trackers = append(trackers, NewHDB(cfg.HDB))
	}
	for name, unit3dCfg := range cfg.UNIT3D {
		if unit3dCfg.APIKey != "" && unit3dCfg.Domain != "" {
			trackers = append(trackers, NewUNIT3D(name, unit3dCfg))
		}
	}
	return nil
}

func Get(host string) Interface {
	// find tracker for this host
	for _, tracker := range trackers {
		if tracker.Check(host) {
			return tracker
		}
	}

	// no tracker found
	return nil
}

func Loaded() int {
	return len(trackers)
}
