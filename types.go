package main

import (
	"time"

	"github.com/prometheus/common/config"
)

type PromTimeseriesCardinalityManagerCfg struct {
	promConfig                    PrometheusConfig
	cardinalityManagerInterval    time.Duration  // How frequently do you want to run this loop?
	totalBudget                   int            // total number of timeseries we can support
	maxMetricCostInBytes          int            // This is used by the PrometheusTCM to drop metrics that have too many labels
	jobTotalTimeseriesCountBudget map[string]int // Total number of timeseries a given scrape_job can have before we murder them
	// Potentially we can offer mulitple drop behaviors.
	dropMode DropMode
	// a global number limiting the amount of timeseries we remote write to w
	remoteWriteLimits RemoteWriteLimits
}

// Custom Types
type RemoteWriteLimits struct {
	Destination            string
	remoteWriteLimitsByJob map[string]int
	remoteWriteSourceLimit int
}

const (
	// This drop mode implies we want to query the timeseries for each job,
	// and evaluate if we should drop them all at the same time.
	Concurrently DropMode = iota
)

type DropMode string

// Types we will import from "github.cm/prometheus/common/config"
type PrometheusConfig struct {
	GlobalConfig       *config.GlobalConfig        `yaml:"global"`
	RuleFiles          []string                    `yaml:"rule_files,omitempty"`
	Alerting           *config.AlertingConfig      `yaml:"alerting,omitempty"`
	ScrapeConfigs      []*config.ScrapeConfig      `yaml:"scrape_configs,omitempty"`
	RemoteWriteConfigs []*config.RemoteWriteConfig `yaml:"remote_write,omitempty"`
	RemoteReadConfigs  []*config.RemoteReadConfig  `yaml:"remote_read,omitempty"`
	// Other fields as needed
}
