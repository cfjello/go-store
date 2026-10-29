package config

// Config represents the application configuration
type Config struct {
	Nodes           NodeConfig      `json:"nodes"`
	MonitorDefaults MonitorDefaults `json:"monitorDefaults"`
	Names           NamesConfig     `json:"names"`
	KvDatabase      KvDatabase      `json:"kvDatabase"`
	Sqlite3         Sqlite3         `json:"sqlite3"`
	// Logs            Logs            `json:"logs"`
	DefaultEnv map[string]string `json:"defaultEnv"`
}

// NodeConfig represents node configuration settings
type NodeConfig struct {
	Name         string `json:"name"`
	JobThreshold int    `json:"jobThreshold"`
	Minimum      int    `json:"minimum"`
	Maximum      int    `json:"maximum"`
	Approach     string `json:"approach"`
	TimerMS      int    `json:"timerMS"`
	SkipFirst    int    `json:"skipFirst"`
}

type NamesConfig struct {
	DataName  string `json:"default"`
	UpdMethod string `json:"updMethod"`
}

// MonitorDefaults represents monitor configuration settings
type MonitorDefaults struct {
	Name      string `json:"name"`
	Port      int    `json:"port"`
	RunServer bool   `json:"runServer"`
}

// KvDatabase represents database connection settings
type KvDatabase struct {
	URL               string `json:"url"`
	DenoKvAccessToken string `json:"DENO_KV_ACCESS_TOKEN"`
}

// Sqlite3 represents SQLite configuration
type Sqlite3 struct {
	Flags string `json:"flags"`
	File  string `json:"file"`
}

// Logs represents logging configuration
type Logs struct {
	StoreLogDir string `json:"STORE_LOGDIR"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		Nodes: NodeConfig{
			Name:         "NodeDefaults",
			JobThreshold: 10,
			Minimum:      5,
			Maximum:      20,
			Approach:     "binary",
			TimerMS:      120000,
			SkipFirst:    1,
		},
		MonitorDefaults: MonitorDefaults{
			Name:      "MonitorDefaults",
			Port:      9999,
			RunServer: true,
		},
		Names: NamesConfig{
			DataName:  "Data",
			UpdMethod: "setData",
		},
		// Sqlite3: Sqlite3{
		// 	Flags: "?_journal_mode=WAL&_synchronous=normal&_journal_size_limit=6144000", &_synchronous=normal&_journal_size_limit=6144000
		// 	File:  "F:/sqlite3/go-store.db",
		// },
		DefaultEnv: map[string]string{
			"PORT":               "9090",
			"APP_ENV":            "local",
			"SQLITE_DB_URL":      "file:F:/Sqlite3/go-store.db",
			"SQLITE_DB_FLAGS":    "?_cache=shared&_journal_mode=WAL&_sync=normal&_mode=rwc",
			"FILE_DB_ALIAS":      "persist",
			"CGO_ENABLED":        "1",
			"ENV_SOURCED":        "true",
			"HIERARCHY_KEY_SIZE": "4",
		},
	}
}
