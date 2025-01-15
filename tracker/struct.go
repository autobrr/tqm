package tracker

type Config struct {
	BHD    BHDConfig
	BTN    BTNConfig
	PTP    PTPConfig
	HDB    HDBConfig
	RED    REDConfig
	OPS    OPSConfig
	UNIT3D map[string]UNIT3DConfig
}

type Torrent struct {
	// torrent
	Hash            string `json:"Hash"`
	Name            string `json:"Name"`
	TotalBytes      int64  `json:"TotalBytes"`
	DownloadedBytes int64  `json:"DownloadedBytes"`
	State           string `json:"State"`
	Downloaded      bool   `json:"Downloaded"`
	Seeding         bool   `json:"Seeding"`

	// tracker
	TrackerName   string
	TrackerStatus string
	Comment       string
}
