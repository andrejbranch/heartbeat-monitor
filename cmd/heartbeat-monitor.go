package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/alecthomas/kingpin.v2"
)

type ringResponse struct {
	Ingesters map[string]IngesterResponse `json:"ingesters"`
}

var (
	memberTimeBehind = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cortex",
			Subsystem: "memberlist",
			Name:      "time_behind",
			Help:      "How far behind cortex memberlist members are",
		},
		[]string{"member"},
	)
)

type IngesterResponse struct {
	Addr      string `json:"addr"`
	Timestamp int    `json:"timestamp"`
	Tokens    []int  `json:"tokens"`
	Reg       int    `json:"registered_timestamp"`
}

func main() {
	var (
		interval       = kingpin.Flag("interval", "poll interval for querying the service").Default("3s").Duration()
		serviceAddress = kingpin.Flag("service-address", "Service address").Default("host.docker.internal:8080").String()
		metricsPort    = kingpin.Flag("metrics-port", "Port to serve metrics").Default("9957").Int()
		viewKey        = kingpin.Flag("view-key", "View key").Default("collectors/ring").String()
	)
	kingpin.Parse()
	logger := log.New(log.Writer(), "ingester-heartbeat", log.Lmicroseconds)
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	monitor := NewHeartbeatMonitor(logger, *interval, *serviceAddress, *viewKey)
	monitor.Start(ctx)
	exposeMetrics(logger, *metricsPort)
	for {
		select {
		case <-ctx.Done():
			return
		}
	}
}

type HeartbeatMonitor struct {
	logger       *log.Logger
	pollInterval time.Duration
	serviceAddr  string
	viewKey      string
}

func NewHeartbeatMonitor(logger *log.Logger, pollInterval time.Duration, serviceAddr string, viewKey string) *HeartbeatMonitor {
	return &HeartbeatMonitor{
		logger:       logger,
		serviceAddr:  serviceAddr,
		pollInterval: pollInterval,
		viewKey:      viewKey,
	}
}

// Start will run poller thread.
func (h *HeartbeatMonitor) Start(ctx context.Context) {
	// initial poll
	h.poll(ctx)
	ticker := time.NewTicker(h.pollInterval)

	go func() {
		for {
			h.logger.Println("msg", "starting poll cycle")
			select {
			case <-ticker.C:
				h.poll(ctx)
			case <-ctx.Done():
				break
			}
		}
	}()
}

func (h *HeartbeatMonitor) poll(ctx context.Context) {
	resp, err := http.Get(fmt.Sprintf("http://%s/memberlist?viewKey=%s&format=json-pretty", h.serviceAddr, h.viewKey))
	currentTime := time.Now().Unix()
	if err != nil {
		h.logger.Fatal("failed getting response from service")
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		h.logger.Fatal("failed reading response from service")
	}
	var r ringResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		h.logger.Fatal("failed unmarshalling response from service", err)
	}
	for k, ingester := range r.Ingesters {
		diff := currentTime - int64(ingester.Timestamp)
		memberTimeBehind.WithLabelValues(k).Set(float64(diff))
	}
}

func exposeMetrics(logger *log.Logger, port int) {
	server := http.Server{
		Addr: fmt.Sprintf(":%d", port),
	}
	http.Handle("/metrics", promhttp.Handler())

	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			logger.Fatal("msg", "Error exposing metrics ", "err", err)
		}
	}()
}
