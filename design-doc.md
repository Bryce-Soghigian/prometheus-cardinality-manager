# Prometheus Timeseries Cardinality Manager
The Prometheus Timeseries Cardinality Manager is necessary to optimize the use of resources for Prometheus. When Prometheus is scraping too many timeseries, it can lead to resource exhaustion and slow performance. The tool ensures that only the most critical metrics are being scraped, preventing overspending and maximizing resource usage.

One key factor in managing resource allocation is the budget for the number of timeseries that can be scraped. By setting a budget, the tool can prevent overspending and ensure that the most critical metrics are prioritized. Additionally, the tool takes into account the cardinality of each metric, which can have a significant impact on resource usage. By dropping metrics that have a high cardinality, the tool can further optimize resource usage.

The tool also provides a metrics endpoint that Prometheus can scrape, allowing for alerts on frequently limited scrape targets. This enables users to monitor and manage resource usage, ensuring that the most critical metrics are being prioritized.

Finally, the tool prevents problematic metrics from being scraped further, ensuring that the resources are allocated to the most critical metrics. This is achieved by writing a controller that watches for high-cardinality metrics and modifies the Prometheus scrape config to prevent these metrics from being scraped.

Overall, the Prometheus Timeseries Cardinality Manager is critical for optimizing resource usage and ensuring that Prometheus continues to function efficiently and effectively.

## That sounds great, but like how do we define the constraints
We need to define some type of configmap for the Manager that stores the following config 

```go
type PromTimeseriesCardinalityManagerCfg struct {
    promConfig PrometheusConfig
    cardinalityManagerInterval time.Duration // How frequently do you want to run this loop? 
    totalBudget int // total number of timeseries we can support
    maxMetricCostInBytes int // This is used by the PrometheusTCM to drop metrics that have too many labels 
    jobTotalTimeseriesCountBudget map[string]int // Total number of timeseries a given scrape_job can have before we murder them 
    // Potentially we can offer mulitple drop behaviors.
    dropMode DropMode 
   // a global number limiting the amount of timeseries we remote write to w 
    remoteWriteLimits RemoteWriteLimits
}


// Custom Types
type RemoteWriteLimits struct {
    Destination string 
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
    GlobalConfig       *GlobalConfig       `yaml:"global"`
    RuleFiles          []string            `yaml:"rule_files,omitempty"`
    Alerting           *AlertingConfig     `yaml:"alerting,omitempty"`
    ScrapeConfigs      []*ScrapeConfig     `yaml:"scrape_configs,omitempty"`
    RemoteWriteConfigs []*RemoteWriteConfig `yaml:"remote_write,omitempty"`
    RemoteReadConfigs  []*RemoteReadConfig `yaml:"remote_read,omitempty"`
    // Other fields as needed
}

```
This should give us everything we need to perform all of the functionality specified in the top of the document.
## Drop Metrics Loop 
1. Check For Metrics we have to keep based on alerting rules, and recording rules and add them to a keep map
2. For each Job:
2a. Check if the metrics of this scrape job have more than the allocated budget we specified 
2b. if true Query The timeseries that have the label for job="current_job" and this __name__ is not one we have in the keep map 
2c. group them by the "__name__" label 
2d. Append that grouping to a MaxHeap() called canidatesForRemoval. we sort based on bytes each Timeseries of that __name__ costs. 
3. For {
3a. While we are exceeding the budget for our scrape job, delete all time series that match __name__, and update the configmap `high-cardinality-metrics`, with the __name__ of the metric
4. Update our metrics endpoint so we can alert on scrape jobs we frequently have to drop from

something like this 
```go 

func dropMetricsLoop(cfg PromTimeseriesCardinalityManagerCfg) {
    // Initialize the keep map with metrics we want to keep based on alerting and recording rules
    keepMetrics := getKeepMetrics(cfg.promConfig)

    // Start the loop to drop metrics
    for {
        for _, scrapeCfg := range cfg.promConfig.ScrapeConfigs {
            jobName := scrapeCfg.JobName

            // Calculate the current number of timeseries for this job
            currentTimeseriesCount := getCurrentTimeseriesCount(scrapeCfg)

            // Check if the current number of timeseries is above the budget
            if currentTimeseriesCount > cfg.jobTotalTimeseriesCountBudget[jobName] {
                // Get the high-cardinality metrics for this job
                highCardinalityMetrics := getHighCardinalityMetrics(scrapeCfg, keepMetrics, cfg.maxMetricCostInBytes)

                // Sort the high-cardinality metrics by the number of bytes they cost
                sort.Slice(highCardinalityMetrics, func(i, j int) bool {
                    return highCardinalityMetrics[i].bytesCost > highCardinalityMetrics[j].bytesCost
                })

                // Drop metrics until we're within the budget
                for _, metric := range highCardinalityMetrics {
                    if currentTimeseriesCount <= cfg.jobTotalTimeseriesCountBudget[jobName] {
                        break
                    }

                    // Update the configmap to drop this metric
                    updateConfigMap(metric, scrapeCfg)

                    // Emit a Prometheus metric for the number of metrics dropped from this scrape job
                    prometheus.Counter("prometheus_tcm_metrics_dropped_total", "The total number of metrics dropped by the Prometheus Timeseries Cardinality Manager", prometheus.Labels{
                        "job":    jobName,
                        "metric": metric.metricName,
                    }).Inc()

                    // Decrement the current timeseries count and move on to the next metric
                    currentTimeseriesCount--
                }
            }
        }

        // Sleep for the specified interval before running the loop again
        time.Sleep(cfg.cardinalityManagerInterval)
    }
}


```

## How do we Prevent metrics we drop from being scraped again?
I propose we write a controller, that watches a configmap `high-cardinality-metrics`.

Our main Prometheus Timeseries Cardinality Manager application will check each cleanup_interval, for our problematic metrics, and write them to this configmap. 

Then the metrics-consolidation-controller, will take these metrics, and modify the prometheus scrape config, to allow us to redefine what metrics we want to scrape.


This controller needs to be aware of two configmaps.

1. high-cardinality-metrics
2. prometheus-config 

This controller will watch for a change in high-cardinality-metrics, and remove all of the metrics in question from the configmap for the prometheus config.


## How do we assign budget?
TBD: Not complete
You and your team can evaluate a variety of factors such as, which metrics endpoints are we scraping that can help all components? IN the case of kubernetes for example kube-state-metrics would be a good endpoint to give a lot of budget, 
as many people can benefit from this endpoint. 

This will have to be something that is discussed across many teams but i reccomend you focus on things in the following order

1. Core Service Stability, availability, and uptime
