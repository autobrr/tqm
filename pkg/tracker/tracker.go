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
	if cfg.PTP.User != "" && cfg.PTP.Key != "" {
		trackers = append(trackers, NewPTP(cfg.PTP))
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
