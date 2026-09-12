package ratelimit

import (
	"context"
	"sync"

	"golang.org/x/time/rate"

	"notification-engine/internal/domain"
)

// Limiters aplica um limite de requisições por segundo (token bucket)
// independente por canal, evitando que o worker sature APIs externas
// (ex: rate limit do Discord/Telegram) em picos de volume.
type Limiters struct {
	mu       sync.Mutex
	limiters map[domain.ChannelType]*rate.Limiter
	rps      map[domain.ChannelType]float64
}

// New recebe o limite de requisições/segundo configurado por canal. Canais
// ausentes do mapa usam o default de defaultRPS.
func New(rps map[domain.ChannelType]float64) *Limiters {
	return &Limiters{
		limiters: make(map[domain.ChannelType]*rate.Limiter),
		rps:      rps,
	}
}

// Wait bloqueia até haver um token disponível para o canal informado,
// respeitando o cancelamento do contexto (ex: shutdown do worker).
func (l *Limiters) Wait(ctx context.Context, ch domain.ChannelType) error {
	return l.limiterFor(ch).Wait(ctx)
}

const defaultRPS = 10

func (l *Limiters) limiterFor(ch domain.ChannelType) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	if lim, ok := l.limiters[ch]; ok {
		return lim
	}

	rps := l.rps[ch]
	if rps <= 0 {
		rps = defaultRPS
	}

	burst := int(rps)
	if burst < 1 {
		burst = 1
	}

	lim := rate.NewLimiter(rate.Limit(rps), burst)
	l.limiters[ch] = lim
	return lim
}
