package quota

import "time"

type Window struct {
	ID             string
	UsedRatio      float64
	RemainingRatio float64
	ResetUnix      int64
}

type Account struct {
	Provider    string
	AuthIndex   string
	Status      string
	Email       string
	AccountType string
	Supported   bool
	Windows     []Window
	FetchedAt   time.Time
	Error       string
}

type Credential struct {
	Provider      string
	AuthIndex     string
	Status        string
	Email         string
	AccountType   string
	Disabled      bool
	Unavailable   bool
	Success       int64
	Failed        int64
	NextRetryUnix int64
	Models        []ModelAvailability
}

type ModelAvailability struct {
	Provider    string
	AuthIndex   string
	Email       string
	Model       string
	Status      string
	Unavailable bool
}

type RuntimeAuth struct {
	Email       string
	AccountType string
	Models      []ModelAvailability
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
