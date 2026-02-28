package cache

// MemoryCacheConfig configures the in-memory LRU cache.
type MemoryCacheConfig struct {
	InitialCapacity int    `json:"initial-capacity" yaml:"initial-capacity"`
	MaxCapacity     int    `json:"max-capacity" yaml:"max-capacity"`
	TTL             int    `json:"ttl" yaml:"ttl"`
	EvictionPolicy  string `json:"eviction-policy" yaml:"eviction-policy"`
}

// RedisCacheConfig configures the Redis cache client.
type RedisCacheConfig struct {
	Nodes             []string `json:"nodes" yaml:"nodes"`
	Username          string   `json:"username" yaml:"username"`
	Password          string   `json:"password" yaml:"password"`
	ConnectionTimeout int      `json:"connection-timeout" yaml:"connection-timeout"`
	ResponseTimeout   int      `json:"response-timeout" yaml:"response-timeout"`
	Retries           int      `json:"retries" yaml:"retries"`
	MaxRetryWait      int      `json:"max-retry-wait" yaml:"max-retry-wait"`
	MinRetryWait      int      `json:"min-retry-wait" yaml:"min-retry-wait"`
	ReadFromReplica   bool     `json:"read-from-replica" yaml:"read-from-replica"`
	UseSSL            bool     `json:"use-ssl" yaml:"use-ssl"`
}
