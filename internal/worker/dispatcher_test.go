package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"notification-engine/internal/channel"
	"notification-engine/internal/domain"
	"notification-engine/internal/ratelimit"
)

// fakeSender simula um channel.Sender controlável pelo teste.
type fakeSender struct {
	channel domain.ChannelType
	err     error
}

func (s *fakeSender) Channel() domain.ChannelType { return s.channel }
func (s *fakeSender) Send(ctx context.Context, n *domain.Notification) error {
	return s.err
}

// fakeRepo é um domain.Repository mínimo em memória, só com o necessário
// para os testes do dispatcher (Update).
type fakeRepo struct {
	mu    sync.Mutex
	saved map[string]*domain.Notification
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{saved: make(map[string]*domain.Notification)}
}

func (r *fakeRepo) Create(ctx context.Context, n *domain.Notification) error { return nil }
func (r *fakeRepo) Update(ctx context.Context, n *domain.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *n
	r.saved[n.ID] = &cp
	return nil
}
func (r *fakeRepo) FindByID(ctx context.Context, id string) (*domain.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.saved[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return n, nil
}
func (r *fakeRepo) FindByIdempotencyKey(ctx context.Context, key string) (*domain.Notification, error) {
	return nil, domain.ErrNotFound
}
func (r *fakeRepo) FindDueForRetry(ctx context.Context, before time.Time, limit int) ([]*domain.Notification, error) {
	return nil, nil
}
func (r *fakeRepo) List(ctx context.Context, filter domain.ListFilter) ([]*domain.Notification, error) {
	return nil, nil
}
func (r *fakeRepo) Stats(ctx context.Context) (domain.Stats, error) { return domain.Stats{}, nil }

func newTestRegistry(senders ...channel.Sender) *channel.Registry {
	reg := channel.NewEmptyRegistry()
	for _, s := range senders {
		reg.Register(s)
	}
	return reg
}

func newTestNotification() *domain.Notification {
	return &domain.Notification{
		ID:          "n1",
		Channel:     domain.ChannelWebhook,
		Target:      "https://example.com/hook",
		Message:     "olá",
		Status:      domain.StatusPending,
		MaxAttempts: 3,
	}
}

func TestDispatcher_Handle_Success(t *testing.T) {
	repo := newFakeRepo()
	dispatcher := &Dispatcher{
		Registry:          newTestRegistry(&fakeSender{channel: domain.ChannelWebhook}),
		Repo:              repo,
		Limiters:          ratelimit.New(map[domain.ChannelType]float64{domain.ChannelWebhook: 1000}),
		MaxBackoffSeconds: 3600,
	}

	n := newTestNotification()
	if err := dispatcher.Handle(context.Background(), n); err != nil {
		t.Fatalf("Handle não deveria retornar erro em operação normal: %v", err)
	}

	saved, _ := repo.FindByID(context.Background(), n.ID)
	if saved.Status != domain.StatusSuccess {
		t.Fatalf("esperava status=success, obteve %s", saved.Status)
	}
}

func TestDispatcher_Handle_TransientFailureSchedulesRetry(t *testing.T) {
	repo := newFakeRepo()
	dispatcher := &Dispatcher{
		Registry:          newTestRegistry(&fakeSender{channel: domain.ChannelWebhook, err: errors.New("falha de rede")}),
		Repo:              repo,
		Limiters:          ratelimit.New(map[domain.ChannelType]float64{domain.ChannelWebhook: 1000}),
		MaxBackoffSeconds: 3600,
	}

	n := newTestNotification()
	n.MaxAttempts = 5
	if err := dispatcher.Handle(context.Background(), n); err != nil {
		t.Fatalf("Handle não deveria retornar erro (falha vai para retry, não para o consumer): %v", err)
	}

	saved, _ := repo.FindByID(context.Background(), n.ID)
	if saved.Status != domain.StatusRetrying {
		t.Fatalf("esperava status=retrying, obteve %s", saved.Status)
	}
	if saved.Attempts != 1 {
		t.Fatalf("esperava Attempts=1, obteve %d", saved.Attempts)
	}
}

func TestDispatcher_Handle_ExhaustsAttemptsGoesToDLQ(t *testing.T) {
	repo := newFakeRepo()
	dispatcher := &Dispatcher{
		Registry:          newTestRegistry(&fakeSender{channel: domain.ChannelWebhook, err: errors.New("falha persistente")}),
		Repo:              repo,
		Limiters:          ratelimit.New(map[domain.ChannelType]float64{domain.ChannelWebhook: 1000}),
		MaxBackoffSeconds: 3600,
	}

	n := newTestNotification()
	n.MaxAttempts = 1
	if err := dispatcher.Handle(context.Background(), n); err != nil {
		t.Fatalf("Handle não deveria retornar erro: %v", err)
	}

	saved, _ := repo.FindByID(context.Background(), n.ID)
	if saved.Status != domain.StatusDLQ {
		t.Fatalf("esperava status=dlq após esgotar tentativas, obteve %s", saved.Status)
	}
}

func TestDispatcher_Handle_UnregisteredChannelGoesDirectlyToDLQ(t *testing.T) {
	repo := newFakeRepo()
	dispatcher := &Dispatcher{
		Registry:          newTestRegistry(), // nenhum sender registrado
		Repo:              repo,
		Limiters:          ratelimit.New(map[domain.ChannelType]float64{}),
		MaxBackoffSeconds: 3600,
	}

	n := newTestNotification()
	n.MaxAttempts = 5 // mesmo com várias tentativas ainda disponíveis
	if err := dispatcher.Handle(context.Background(), n); err != nil {
		t.Fatalf("Handle não deveria retornar erro: %v", err)
	}

	saved, _ := repo.FindByID(context.Background(), n.ID)
	if saved.Status != domain.StatusDLQ {
		t.Fatalf("canal sem sender registrado deveria ir direto para DLQ (falha não é recuperável por retry), obteve %s", saved.Status)
	}
}

func TestDispatcher_Handle_ContextCanceledDuringRateLimitPropagatesError(t *testing.T) {
	repo := newFakeRepo()
	dispatcher := &Dispatcher{
		Registry:          newTestRegistry(&fakeSender{channel: domain.ChannelWebhook}),
		Repo:              repo,
		Limiters:          ratelimit.New(map[domain.ChannelType]float64{domain.ChannelWebhook: 0.001}), // praticamente nunca libera token
		MaxBackoffSeconds: 3600,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // já cancelado: Limiters.Wait deve retornar erro imediatamente

	n := newTestNotification()
	if err := dispatcher.Handle(ctx, n); err == nil {
		t.Fatal("esperava erro de contexto cancelado propagado pelo rate limiter")
	}
}
