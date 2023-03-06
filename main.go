
import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog/v2"
)

var (
	masterURL  string
	kubeconfig string
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	cfg, err := clientConfig()
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %v", err)
	}

	controller := NewController(clientset)

	// Run the controller loop indefinitely.
	go wait.Forever(func() { controller.Run() }, time.Second)

	// Wait forever.
	select {}
}

func clientConfig() (*rest.Config, error) {
	if kubeconfig == "" {
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}

	if _, err := os.Stat(kubeconfig); os.IsNotExist(err) {
		return nil, fmt.Errorf("kubeconfig file not found: %s", kubeconfig)
	}

	config, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		return nil, err
	}

	return config, nil
}

type Controller struct {
	clientset kubernetes.Interface
}

func NewController(clientset kubernetes.Interface) *Controller {
	return &Controller{
		clientset: clientset,
	}
}

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

func (c *Controller) Run() {
	klog.Infof("Starting controller")
	// Connect to the Prometheus server
	client, err := api.NewClient(api.Config{
		Address: "http://localhost:9090",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to Prometheus server: %s\n", err)
		os.Exit(1)
	}
	v1api := v1.NewAPI(client)
	dropMetricsLoop()
	klog.Infof("Stopping controller")
}
