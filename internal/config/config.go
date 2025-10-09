package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	Proxy struct {
		Listen       string `json:"listen"`
		ClientIdleMs int    `json:"client_idle_ms"`
		MaxClients   int    `json:"max_clients"`
		ReadBuf      int    `json:"read_buf"`
		WriteBuf     int    `json:"write_buf"`
	} `json:"proxy"`
	Upstream struct {
		Host               string `json:"host"`
		Port               int    `json:"port"`
		User               string `json:"user"`
		Pass               string `json:"pass"`
		TLS                bool   `json:"tls"`
		InsecureSkipVerify bool   `json:"insecure_skip_verify"`
		BackoffMinMs       int    `json:"backoff_min_ms"`
		BackoffMaxMs       int    `json:"backoff_max_ms"`
	} `json:"upstream"`
	HTTP struct {
		Listen string `json:"listen"`
		Pprof  bool   `json:"pprof"`
	} `json:"http"`
	VarDiff struct {
		Enabled       bool `json:"enabled"`
		TargetSeconds int  `json:"target_seconds"`
		MinDiff       int  `json:"min_diff"`
		MaxDiff       int  `json:"max_diff"`
		AdjustEveryMs int  `json:"adjust_every_ms"`
	} `json:"vardiff"`
	Compat struct {
		StrictBroadcast bool `json:"strict_broadcast"`
	} `json:"compat"`
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, err
	}

	setDefaults(&cfg)
	return &cfg, nil
}

func setDefaults(cfg *Config) {
	if cfg.Proxy.Listen == "" {
		cfg.Proxy.Listen = ":3334"
	}
	if cfg.Proxy.ClientIdleMs == 0 {
		cfg.Proxy.ClientIdleMs = 180_000
	}
	if cfg.Proxy.MaxClients == 0 {
		cfg.Proxy.MaxClients = 512
	}
	if cfg.Proxy.ReadBuf == 0 {
		cfg.Proxy.ReadBuf = 2048
	}
	if cfg.Proxy.WriteBuf == 0 {
		cfg.Proxy.WriteBuf = 2048
	}
	if cfg.Upstream.BackoffMinMs == 0 {
		cfg.Upstream.BackoffMinMs = 1000
	}
	if cfg.Upstream.BackoffMaxMs == 0 {
		cfg.Upstream.BackoffMaxMs = 20000
	}
	if cfg.HTTP.Listen == "" {
		cfg.HTTP.Listen = ":8080"
	}
	if cfg.VarDiff.TargetSeconds == 0 {
		cfg.VarDiff.TargetSeconds = 18
	}
	if cfg.VarDiff.MinDiff == 0 {
		cfg.VarDiff.MinDiff = 8
	}
	if cfg.VarDiff.MaxDiff == 0 {
		cfg.VarDiff.MaxDiff = 16384
	}
	if cfg.VarDiff.AdjustEveryMs == 0 {
		cfg.VarDiff.AdjustEveryMs = 60_000
	}
}
