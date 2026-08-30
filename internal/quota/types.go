package quota

import "time"

type Window struct {
	ID             string
	UsedRatio      float64
	RemainingRatio float64
	ResetUnix      int64
}

type Account struct {
	Provider  string
	AuthIndex string
	Status    string
	Supported bool
	Windows   []Window
	FetchedAt time.Time
	Error     string
}

type Credential struct {
	Provider      string
	AuthIndex     string
	Status        string
	Disabled      bool
	Unavailable   bool
	Success       int64
	Failed        int64
	NextRetryUnix int64
}

type Config struct {
	Interval        time.Duration
	RequestTimeout  time.Duration
	IncludeDisabled bool
	MaxConcurrency  int
}

func DefaultConfig() Config {
	return Config{
		Interval:       5 * time.Minute,
		RequestTimeout: 20 * time.Second,
		MaxConcurrency: 4,
	}
}
