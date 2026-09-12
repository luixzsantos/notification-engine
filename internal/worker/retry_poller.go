package worker

import (
	"context"
	"log"
	"time"

	"notification-engine/internal/domain"
)

// RetryPoller varre periodicamente o banco em busca de notificações com
// retry vencido (status=retrying, next_retry_at <= agora) e as reenfileira
// no Redis para o worker processar de novo.
type RetryPoller struct {
	Repo      domain.Repository
	Producer  domain.Producer
	Interval  time.Duration
	BatchSize int
}

// Run bloqueia varrendo o banco a cada Interval, até o contexto ser
// cancelado (shutdown do worker).
func (p *RetryPoller) Run(ctx context.Context) {
	log.Printf("[retry-poller] iniciado (intervalo=%s, lote=%d)", p.Interval, p.BatchSize)

	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

func (p *RetryPoller) pollOnce(ctx context.Context) {
	due, err := p.Repo.FindDueForRetry(ctx, time.Now().UTC(), p.BatchSize)
	if err != nil {
		log.Printf("[retry-poller] falha ao buscar retries pendentes: %v", err)
		return
	}

	for _, n := range due {
		// Marca como "pending" antes de reenfileirar: evita que o próximo
		// tick selecione a mesma linha antes do worker processá-la.
		n.Status = domain.StatusPending
		n.NextRetryAt = nil
		n.UpdatedAt = time.Now().UTC()

		if err := p.Producer.Enqueue(ctx, n); err != nil {
			log.Printf("[retry-poller] falha ao reenfileirar %s: %v", n.ID, err)
			continue
		}
		if err := p.Repo.Update(ctx, n); err != nil {
			log.Printf("[retry-poller] falha ao atualizar status de %s: %v", n.ID, err)
			continue
		}

		log.Printf("[retry-poller] reenfileirado id=%s channel=%s tentativa=%d/%d",
			n.ID, n.Channel, n.Attempts, n.MaxAttempts)
	}
}
