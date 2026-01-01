package metrics

import (
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Enabled controls whether metrics collection is active at runtime.
// Checked by middleware, collector, event listener, and scheduler.
var Enabled atomic.Bool

const namespace = "nasnet"

// Registry is the custom Prometheus registry used by all metrics.
var Registry *prometheus.Registry

// HTTP metrics
var (
	HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "http_requests_total",
		Help:      "Total HTTP requests.",
	}, []string{"method", "path", "status"})

	HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path", "status"})

	HTTPRequestsInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "http_requests_in_flight",
		Help:      "Number of HTTP requests currently being processed.",
	})
)

// Business metrics
var (
	UsersTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "users_total",
		Help:      "User counts by category.",
	}, []string{"category"})

	SubscriptionsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "subscriptions_total",
		Help:      "Subscription counts by status.",
	}, []string{"status"})
)

// Node metrics
var (
	NodesTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "nodes_total",
		Help:      "Node counts by status.",
	}, []string{"status"})

	NodeCPUPercent = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "node_cpu_percent",
		Help:      "Per-node CPU usage percentage.",
	}, []string{"node_id", "node_name"})

	NodeMemoryPercent = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "node_memory_percent",
		Help:      "Per-node memory usage percentage.",
	}, []string{"node_id", "node_name"})

	NodeDiskPercent = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "node_disk_percent",
		Help:      "Per-node disk usage percentage.",
	}, []string{"node_id", "node_name"})

	NodeTCPConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "node_tcp_connections",
		Help:      "Per-node TCP connection count.",
	}, []string{"node_id", "node_name"})

	NodeUDPConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "node_udp_connections",
		Help:      "Per-node UDP connection count.",
	}, []string{"node_id", "node_name"})

	NodeTrafficBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "node_traffic_bytes",
		Help:      "Per-node cumulative traffic in bytes.",
	}, []string{"node_id", "node_name", "direction"})

	NodeOnlineUsers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "node_online_users",
		Help:      "Per-node connected user count.",
	}, []string{"node_id", "node_name"})

	NodeXrayUptimeSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "node_xray_uptime_seconds",
		Help:      "Per-node Xray process uptime in seconds.",
	}, []string{"node_id", "node_name"})
)

// Provisioning metrics
var (
	ProvisioningQueueDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "provisioning_queue_depth",
		Help:      "Provisioning queue size by status.",
	}, []string{"status"})

	ProvisioningTasksProcessed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "provisioning_tasks_processed_total",
		Help:      "Total provisioning tasks processed.",
	}, []string{"type", "result"})
)

// Certificate metrics
var (
	CertificateExpirySeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "certificate_expiry_seconds",
		Help:      "Seconds until certificate expiry.",
	}, []string{"domain"})
)

// Database metrics
var (
	DBOpenConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "db_open_connections",
		Help:      "Database connection pool usage.",
	}, []string{"state"})

	DBMaxConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "db_max_connections",
		Help:      "Database connection pool maximum.",
	})
)

// Scheduler metrics
var (
	SchedulerTaskDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "scheduler_task_duration_seconds",
		Help:      "Duration of scheduler tasks.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"task"})

	SchedulerTaskErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "scheduler_task_errors_total",
		Help:      "Total scheduler task failures.",
	}, []string{"task"})
)

// Event bus metrics
var (
	EventsPublished = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "events_published_total",
		Help:      "Total events published via EventBus.",
	}, []string{"type"})

	EventsDropped = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "events_dropped_total",
		Help:      "Total events dropped due to a full subscriber buffer.",
	}, []string{"type", "subscriber_id"})

	EventBusSubscribers = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "eventbus_subscribers",
		Help:      "Active SSE subscriber count.",
	})
)

// Init creates the custom registry and registers all metrics.
// Must be called once at startup before any metric is used.
func Init() {
	Enabled.Store(true)
	Registry = prometheus.NewRegistry()

	// Go runtime + process collectors
	Registry.MustRegister(collectors.NewGoCollector())
	Registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	// HTTP
	Registry.MustRegister(HTTPRequestsTotal)
	Registry.MustRegister(HTTPRequestDuration)
	Registry.MustRegister(HTTPRequestsInFlight)

	// Business
	Registry.MustRegister(UsersTotal)
	Registry.MustRegister(SubscriptionsTotal)

	// Nodes
	Registry.MustRegister(NodesTotal)
	Registry.MustRegister(NodeCPUPercent)
	Registry.MustRegister(NodeMemoryPercent)
	Registry.MustRegister(NodeDiskPercent)
	Registry.MustRegister(NodeTCPConnections)
	Registry.MustRegister(NodeUDPConnections)
	Registry.MustRegister(NodeTrafficBytes)
	Registry.MustRegister(NodeOnlineUsers)
	Registry.MustRegister(NodeXrayUptimeSeconds)

	// Provisioning
	Registry.MustRegister(ProvisioningQueueDepth)
	Registry.MustRegister(ProvisioningTasksProcessed)

	// Certificates
	Registry.MustRegister(CertificateExpirySeconds)

	// Database
	Registry.MustRegister(DBOpenConnections)
	Registry.MustRegister(DBMaxConnections)

	// Scheduler
	Registry.MustRegister(SchedulerTaskDuration)
	Registry.MustRegister(SchedulerTaskErrors)

	// Events
	Registry.MustRegister(EventsPublished)
	Registry.MustRegister(EventsDropped)
	Registry.MustRegister(EventBusSubscribers)
}
