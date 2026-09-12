// Package metrics expõe os contadores Prometheus da engine. São variáveis
// globais por design: é o mesmo padrão usado pela lib oficial do Prometheus
// (promauto), e evita ter que passar um "metrics client" por toda a cadeia
// de chamadas só para incrementar um contador.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	Enqueued = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "notifications_enqueued_total",
		Help: "Total de notificações aceitas e enfileiradas, por canal.",
	}, []string{"channel"})

	Sent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "notifications_sent_total",
		Help: "Total de tentativas de envio ao canal externo, por canal e resultado.",
	}, []string{"channel", "result"})

	Retried = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "notifications_retried_total",
		Help: "Total de notificações agendadas para nova tentativa, por canal.",
	}, []string{"channel"})

	DLQ = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "notifications_dlq_total",
		Help: "Total de notificações movidas para a DLQ (tentativas esgotadas), por canal.",
	}, []string{"channel"})

	SendDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "notification_send_duration_seconds",
		Help:    "Duração da chamada ao canal externo (HTTP/SMTP), por canal.",
		Buckets: prometheus.DefBuckets,
	}, []string{"channel"})
)

func RecordEnqueued(channel string) {
	Enqueued.WithLabelValues(channel).Inc()
}

func RecordSend(channel string, success bool, elapsed time.Duration) {
	result := "failure"
	if success {
		result = "success"
	}
	Sent.WithLabelValues(channel, result).Inc()
	SendDuration.WithLabelValues(channel).Observe(elapsed.Seconds())
}

func RecordRetry(channel string) {
	Retried.WithLabelValues(channel).Inc()
}

func RecordDLQ(channel string) {
	DLQ.WithLabelValues(channel).Inc()
}
