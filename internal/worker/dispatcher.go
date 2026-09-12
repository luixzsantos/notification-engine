// Package worker contém a lógica de processamento de notificações usada
// pelo cmd/worker: rotear ao conector certo, aplicar rate limit, decidir
// retry/DLQ e persistir o resultado.
package worker

import (
	"context"
	"log"
	"time"

	"notification-engine/internal/channel"
	"notification-engine/internal/domain"
	"notification-engine/internal/metrics"
	"notification-engine/internal/ratelimit"
	"notification-engine/internal/retry"
)

// Dispatcher processa uma notificação lida da fila. Nunca devolve erro ao
// consumer do Redis: o resultado (sucesso, retry agendado ou DLQ) fica
// registrado no banco, que é a única fonte de verdade sobre o que fazer a
// seguir — a mensagem do stream é sempre confirmada (ACK) depois de passar
// por aqui.
type Dispatcher struct {
	Registry          *channel.Registry
	Repo              domain.Repository
	Limiters          *ratelimit.Limiters
	MaxBackoffSeconds int
}

func (d *Dispatcher) Handle(ctx context.Context, n *domain.Notification) error {
	sender, err := d.Registry.Get(n.Channel)
	if err != nil {
		d.fail(ctx, n, err, true)
		return nil
	}

	if err := d.Limiters.Wait(ctx, n.Channel); err != nil {
		return err // contexto cancelado (shutdown do worker): mensagem some do PEL, sem problema no encerramento
	}

	start := time.Now()
	sendErr := sender.Send(ctx, n)
	metrics.RecordSend(string(n.Channel), sendErr == nil, time.Since(start))

	if sendErr != nil {
		d.fail(ctx, n, sendErr, false)
		return nil
	}

	n.Status = domain.StatusSuccess
	n.LastError = ""
	n.UpdatedAt = time.Now().UTC()
	if err := d.Repo.Update(ctx, n); err != nil {
		log.Printf("[dispatcher] falha ao persistir sucesso de %s: %v", n.ID, err)
	}

	log.Printf("[dispatcher] sucesso id=%s channel=%s target=%s", n.ID, n.Channel, n.Target)
	return nil
}

// fail registra uma falha de envio e decide entre agendar retry ou mover
// para a DLQ, conforme MaxAttempts da notificação. permanent=true pula
// direto para a DLQ (usado para erros que retry não resolveria, como canal
// não configurado).
func (d *Dispatcher) fail(ctx context.Context, n *domain.Notification, cause error, permanent bool) {
	n.Attempts++
	n.LastError = cause.Error()
	n.UpdatedAt = time.Now().UTC()

	if permanent || n.Attempts >= n.MaxAttempts {
		n.Status = domain.StatusDLQ
		n.NextRetryAt = nil
		metrics.RecordDLQ(string(n.Channel))
		log.Printf("[dispatcher] DLQ id=%s channel=%s tentativas=%d/%d erro=%v",
			n.ID, n.Channel, n.Attempts, n.MaxAttempts, cause)
	} else {
		next := time.Now().UTC().Add(retry.NextBackoff(n.Attempts, d.MaxBackoffSeconds))
		n.Status = domain.StatusRetrying
		n.NextRetryAt = &next
		metrics.RecordRetry(string(n.Channel))
		log.Printf("[dispatcher] retry agendado id=%s channel=%s tentativa=%d/%d em=%s erro=%v",
			n.ID, n.Channel, n.Attempts, n.MaxAttempts, next.Format(time.RFC3339), cause)
	}

	if err := d.Repo.Update(ctx, n); err != nil {
		log.Printf("[dispatcher] falha ao persistir falha de %s: %v", n.ID, err)
	}
}
