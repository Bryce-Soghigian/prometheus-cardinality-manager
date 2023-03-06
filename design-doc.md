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

something like this 
```go 
func dropMetricsLoop(config PromTimeseriesCardinalityManagerCfg) {
    keepMetrics := getKeepMetricsFromRules(config.promConfig) // get metrics to keep based on alerting and recording rules
    for _, scrapeJob := range config.promConfig.ScrapeConfigs {
        totalJobTimeseriesCount := getTotalJobTimeseriesCount(scrapeJob) // get total number of timeseries for this scrape job
        jobTimeseriesCountBudget := config.jobTotalTimeseriesCountBudget[scrapeJob.JobName] // get budget for this scrape job
        if totalJobTimeseriesCount <= jobTimeseriesCountBudget {
            continue // no need to drop metrics for this scrape job
        }
        candidatesForRemoval := getMaxHeapOfMetricsToRemove(scrapeJob, keepMetrics, config.maxMetricCostInBytes) // get a heap of metrics to remove
        for totalJobTimeseriesCount > jobTimeseriesCountBudget {
            metricToRemove := candidatesForRemoval.Pop() // get the metric with highest cost
            removeTimeseriesWithMetric(metricToRemove, scrapeJob) // remove all timeseries with this metric
            totalJobTimeseriesCount-- // decrement the total timeseries count for this scrape job
            updateHighCardinalityMetricsConfigMap(metricToRemove) // update the configmap with the dropped metric
        }
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




## How do we assign budget? 
You and your team can evaluate a variety of factors such as, which metrics endpoints are we scraping that can help all components? IN the case of kubernetes for example kube-state-metrics would be a good endpoint to give a lot of budget, 
as many people can benefit from this endpoint. 

This will have to be something that is discussed across many teams but i reccomend you focus on things in the following order

1. Core Service Stability, availability, and uptime
