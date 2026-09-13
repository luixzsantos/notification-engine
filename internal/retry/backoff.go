// Package retry calcula o atraso entre tentativas de reenvio de uma
// notificação que falhou (exponential backoff com jitter) e decide, a
// partir do número de tentativas já feitas, se a próxima ação é agendar
// um novo retry ou mover a notificação para a DLQ.
package retry

import (
	"math/rand"
	"time"

	"notification-engine/internal/domain"
)

// NextBackoff retorna o atraso até a próxima tentativa, dado o número de
// tentativas já feitas (attempt >= 1). O teto cresce exponencialmente (2s,
// 4s, 8s, 16s...) até maxSeconds, e o valor efetivo é sorteado uniformemente
// em [1s, teto] ("full jitter"). Isso evita que um lote de notificações que
// falhou no mesmo instante seja reenviado todo de uma vez, concentrando um
// pico de requisições exatamente quando o canal externo pode já estar
// instável.
func NextBackoff(attempt int, maxSeconds int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if maxSeconds < 1 {
		maxSeconds = 1
	}

	capSeconds := 1 << attempt // 2^attempt
	if capSeconds <= 0 || capSeconds > maxSeconds {
		capSeconds = maxSeconds
	}

	jittered := 1 + rand.Intn(capSeconds)
	return time.Duration(jittered) * time.Second
}

// ApplyFailure registra uma tentativa de entrega malsucedida em n: incrementa
// Attempts, grava o erro e decide entre agendar um novo retry (com backoff)
// ou mover a notificação para a DLQ, caso MaxAttempts tenha sido atingido.
// É uma função pura (não persiste nada) para poder ser reutilizada tanto
// pelo worker (falha ao entregar ao canal) quanto pela API (falha ao
// enfileirar logo após persistir a notificação).
func ApplyFailure(n *domain.Notification, cause error, maxBackoffSeconds int) {
	n.Attempts++
	n.LastError = cause.Error()
	n.UpdatedAt = time.Now().UTC()

	if n.Attempts >= n.MaxAttempts {
		n.Status = domain.StatusDLQ
		n.NextRetryAt = nil
		return
	}

	next := time.Now().UTC().Add(NextBackoff(n.Attempts, maxBackoffSeconds))
	n.Status = domain.StatusRetrying
	n.NextRetryAt = &next
}
