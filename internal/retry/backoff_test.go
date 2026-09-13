package retry

import (
	"errors"
	"testing"
	"time"

	"notification-engine/internal/domain"
)

func TestNextBackoff_WithinBounds(t *testing.T) {
	tests := []struct {
		attempt    int
		maxSeconds int
	}{
		{attempt: 1, maxSeconds: 3600},
		{attempt: 2, maxSeconds: 3600},
		{attempt: 5, maxSeconds: 3600},
		{attempt: 10, maxSeconds: 3600}, // 2^10=1024 < 3600
		{attempt: 20, maxSeconds: 3600}, // 2^20 estoura o teto, deve ser limitado
		{attempt: 0, maxSeconds: 3600},  // normalizado para 1
		{attempt: -5, maxSeconds: 3600}, // normalizado para 1
	}

	for _, tt := range tests {
		for i := 0; i < 50; i++ { // várias amostras por causa do jitter aleatório
			d := NextBackoff(tt.attempt, tt.maxSeconds)
			if d < time.Second {
				t.Fatalf("attempt=%d: backoff %s abaixo do mínimo de 1s", tt.attempt, d)
			}
			if d > time.Duration(tt.maxSeconds)*time.Second {
				t.Fatalf("attempt=%d: backoff %s excede o teto de %ds", tt.attempt, d, tt.maxSeconds)
			}
		}
	}
}

func TestNextBackoff_JitterVaries(t *testing.T) {
	seen := make(map[time.Duration]bool)
	for i := 0; i < 30; i++ {
		seen[NextBackoff(6, 3600)] = true
	}
	if len(seen) < 2 {
		t.Fatalf("esperava valores variados por causa do jitter, mas obteve só %d valor(es) distintos em 30 amostras", len(seen))
	}
}

func newTestNotification(attempts, maxAttempts int) *domain.Notification {
	return &domain.Notification{
		ID:          "test-id",
		Channel:     domain.ChannelWebhook,
		Attempts:    attempts,
		MaxAttempts: maxAttempts,
		Status:      domain.StatusPending,
	}
}

func TestApplyFailure_SchedulesRetryBeforeMaxAttempts(t *testing.T) {
	n := newTestNotification(0, 5)
	cause := errors.New("falha simulada")

	ApplyFailure(n, cause, 3600)

	if n.Attempts != 1 {
		t.Fatalf("esperava Attempts=1, obteve %d", n.Attempts)
	}
	if n.Status != domain.StatusRetrying {
		t.Fatalf("esperava status=retrying, obteve %s", n.Status)
	}
	if n.NextRetryAt == nil {
		t.Fatal("esperava NextRetryAt preenchido")
	}
	if n.LastError != cause.Error() {
		t.Fatalf("esperava LastError=%q, obteve %q", cause.Error(), n.LastError)
	}
}

func TestApplyFailure_MovesToDLQAtMaxAttempts(t *testing.T) {
	n := newTestNotification(4, 5) // próxima falha é a 5ª tentativa == MaxAttempts
	ApplyFailure(n, errors.New("última falha"), 3600)

	if n.Attempts != 5 {
		t.Fatalf("esperava Attempts=5, obteve %d", n.Attempts)
	}
	if n.Status != domain.StatusDLQ {
		t.Fatalf("esperava status=dlq, obteve %s", n.Status)
	}
	if n.NextRetryAt != nil {
		t.Fatal("esperava NextRetryAt nil para notificação em DLQ")
	}
}
