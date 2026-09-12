// Package retry calcula o atraso entre tentativas de reenvio de uma
// notificação que falhou (exponential backoff).
package retry

import "time"

// NextBackoff retorna o atraso até a próxima tentativa, dado o número de
// tentativas já feitas (attempt >= 1). Cresce exponencialmente (2s, 4s,
// 8s, 16s...) até o teto de maxSeconds.
func NextBackoff(attempt int, maxSeconds int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	seconds := 1 << attempt // 2^attempt
	if seconds <= 0 || seconds > maxSeconds {
		seconds = maxSeconds
	}

	return time.Duration(seconds) * time.Second
}
