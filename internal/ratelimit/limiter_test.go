package ratelimit

import (
	"context"
	"testing"
	"time"

	"notification-engine/internal/domain"
)

func TestLimiters_WaitConsumesBurstImmediately(t *testing.T) {
	l := New(map[domain.ChannelType]float64{domain.ChannelWebhook: 5})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// burst = 5: as primeiras 5 chamadas devem liberar quase instantaneamente.
	start := time.Now()
	for i := 0; i < 5; i++ {
		if err := l.Wait(ctx, domain.ChannelWebhook); err != nil {
			t.Fatalf("chamada %d dentro do burst falhou: %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("esperava que o burst inicial fosse quase instantâneo, levou %s", elapsed)
	}
}

func TestLimiters_WaitBlocksBeyondBurst(t *testing.T) {
	l := New(map[domain.ChannelType]float64{domain.ChannelWebhook: 2}) // 2 rps, burst=2

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Esgota o burst.
	for i := 0; i < 2; i++ {
		if err := l.Wait(ctx, domain.ChannelWebhook); err != nil {
			t.Fatalf("chamada dentro do burst falhou: %v", err)
		}
	}

	// A 3ª chamada precisa esperar pelo menos ~1/rps segundos (aprox 500ms).
	start := time.Now()
	if err := l.Wait(ctx, domain.ChannelWebhook); err != nil {
		t.Fatalf("chamada além do burst falhou: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("esperava que a chamada além do burst esperasse por um token, levou apenas %s", elapsed)
	}
}

func TestLimiters_WaitRespectsContextCancellation(t *testing.T) {
	l := New(map[domain.ChannelType]float64{domain.ChannelWebhook: 0.001}) // praticamente nunca libera token

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := l.Wait(ctx, domain.ChannelWebhook); err == nil {
		t.Fatal("esperava erro por contexto já cancelado")
	}
}

func TestLimiters_IndependentChannelsDoNotShareBudget(t *testing.T) {
	l := New(map[domain.ChannelType]float64{
		domain.ChannelWebhook: 1,
		domain.ChannelDiscord: 1,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Esgota o burst do webhook; discord deve continuar livre.
	if err := l.Wait(ctx, domain.ChannelWebhook); err != nil {
		t.Fatalf("primeira chamada webhook falhou: %v", err)
	}
	start := time.Now()
	if err := l.Wait(ctx, domain.ChannelDiscord); err != nil {
		t.Fatalf("chamada discord falhou: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("esperava que o canal discord tivesse seu próprio orçamento independente, levou %s", elapsed)
	}
}

func TestLimiters_ConcurrentAccessIsSafe(t *testing.T) {
	l := New(map[domain.ChannelType]float64{domain.ChannelWebhook: 1000})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() {
			done <- l.Wait(ctx, domain.ChannelWebhook)
		}()
	}

	for i := 0; i < 20; i++ {
		if err := <-done; err != nil {
			t.Errorf("chamada concorrente falhou: %v", err)
		}
	}
}
