package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"notification-engine/internal/domain"
)

// fakeRepository é uma implementação em memória de domain.Repository,
// usada para testar o service sem precisar de um PostgreSQL real.
type fakeRepository struct {
	mu          sync.Mutex
	byID        map[string]*domain.Notification
	byIdemKey   map[string]string // idempotency_key -> id
	failCreate  error
	failEnqueue error // não usado aqui, mantido no fakeProducer
	updateCalls int
	createCalls int
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		byID:      make(map[string]*domain.Notification),
		byIdemKey: make(map[string]string),
	}
}

func (r *fakeRepository) Create(ctx context.Context, n *domain.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.createCalls++
	if r.failCreate != nil {
		return r.failCreate
	}

	if n.IdempotencyKey != "" {
		if _, exists := r.byIdemKey[n.IdempotencyKey]; exists {
			return domain.ErrDuplicateIdempotencyKey
		}
		r.byIdemKey[n.IdempotencyKey] = n.ID
	}

	cp := *n
	r.byID[n.ID] = &cp
	return nil
}

func (r *fakeRepository) Update(ctx context.Context, n *domain.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.updateCalls++
	if _, ok := r.byID[n.ID]; !ok {
		return domain.ErrNotFound
	}
	cp := *n
	r.byID[n.ID] = &cp
	return nil
}

func (r *fakeRepository) FindByID(ctx context.Context, id string) (*domain.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *n
	return &cp, nil
}

func (r *fakeRepository) FindByIdempotencyKey(ctx context.Context, key string) (*domain.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id, ok := r.byIdemKey[key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *r.byID[id]
	return &cp, nil
}

func (r *fakeRepository) FindDueForRetry(ctx context.Context, before time.Time, limit int) ([]*domain.Notification, error) {
	return nil, nil
}

func (r *fakeRepository) List(ctx context.Context, filter domain.ListFilter) ([]*domain.Notification, error) {
	return nil, nil
}

func (r *fakeRepository) Stats(ctx context.Context) (domain.Stats, error) {
	return domain.Stats{}, nil
}

// fakeProducer é um domain.Producer em memória, com a opção de simular
// falha no enfileiramento (para testar o fallback de consistência).
type fakeProducer struct {
	mu        sync.Mutex
	enqueued  []*domain.Notification
	failNext  bool
	callCount int
}

func (p *fakeProducer) Enqueue(ctx context.Context, n *domain.Notification) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.callCount++
	if p.failNext {
		p.failNext = false
		return errors.New("falha simulada no redis")
	}
	p.enqueued = append(p.enqueued, n)
	return nil
}

func newService(repo *fakeRepository, producer *fakeProducer) *NotificationService {
	return NewNotificationService(producer, repo, map[domain.ChannelType]string{}, 5, 3600, false)
}

func TestCreateNotification_HappyPath(t *testing.T) {
	repo := newFakeRepository()
	producer := &fakeProducer{}
	svc := newService(repo, producer)

	n, err := svc.CreateNotification(context.Background(), CreateNotificationInput{
		Channel: domain.ChannelWebhook,
		Target:  "https://example.com/hook",
		Message: "olá",
	})
	if err != nil {
		t.Fatalf("esperava sucesso, obteve erro: %v", err)
	}
	if n.Status != domain.StatusPending {
		t.Errorf("esperava status pending, obteve %s", n.Status)
	}
	if len(producer.enqueued) != 1 {
		t.Errorf("esperava 1 mensagem enfileirada, obteve %d", len(producer.enqueued))
	}
}

func TestCreateNotification_ValidationError(t *testing.T) {
	repo := newFakeRepository()
	producer := &fakeProducer{}
	svc := newService(repo, producer)

	_, err := svc.CreateNotification(context.Background(), CreateNotificationInput{
		Channel: "sms", // canal inválido
		Target:  "x",
		Message: "olá",
	})
	if !errors.Is(err, domain.ErrInvalidChannel) {
		t.Fatalf("esperava ErrInvalidChannel, obteve: %v", err)
	}
	if repo.createCalls != 0 {
		t.Error("não deveria ter chamado repo.Create para um input inválido")
	}
}

func TestCreateNotification_BlocksSSRFTarget(t *testing.T) {
	repo := newFakeRepository()
	producer := &fakeProducer{}
	svc := newService(repo, producer)

	_, err := svc.CreateNotification(context.Background(), CreateNotificationInput{
		Channel: domain.ChannelWebhook,
		Target:  "http://169.254.169.254/latest/meta-data",
		Message: "olá",
	})
	if err == nil {
		t.Fatal("esperava erro de SSRF, mas a criação foi aceita")
	}
	if repo.createCalls != 0 {
		t.Error("não deveria ter persistido notificação com destino bloqueado")
	}
}

func TestCreateNotification_IdempotentReplayReturnsExisting(t *testing.T) {
	repo := newFakeRepository()
	producer := &fakeProducer{}
	svc := newService(repo, producer)

	input := CreateNotificationInput{
		Channel:        domain.ChannelWebhook,
		Target:         "https://example.com/hook",
		Message:        "olá",
		IdempotencyKey: "chave-123",
	}

	first, err := svc.CreateNotification(context.Background(), input)
	if err != nil {
		t.Fatalf("primeira criação falhou: %v", err)
	}

	second, err := svc.CreateNotification(context.Background(), input)
	if err != nil {
		t.Fatalf("segunda criação (replay) falhou: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("esperava o mesmo ID no replay (%s), obteve %s", first.ID, second.ID)
	}
	if repo.createCalls != 1 {
		t.Errorf("esperava apenas 1 chamada a repo.Create, obteve %d", repo.createCalls)
	}
	if len(producer.enqueued) != 1 {
		t.Errorf("esperava apenas 1 mensagem enfileirada (sem duplicar no replay), obteve %d", len(producer.enqueued))
	}
}

func TestCreateNotification_EnqueueFailureSchedulesRetryInsteadOfOrphaning(t *testing.T) {
	repo := newFakeRepository()
	producer := &fakeProducer{failNext: true}
	svc := newService(repo, producer)

	n, err := svc.CreateNotification(context.Background(), CreateNotificationInput{
		Channel: domain.ChannelWebhook,
		Target:  "https://example.com/hook",
		Message: "olá",
	})
	if err != nil {
		t.Fatalf("esperava que a falha no enqueue não propagasse erro ao cliente, obteve: %v", err)
	}
	if n.Status != domain.StatusRetrying {
		t.Fatalf("esperava status=retrying após falha no enqueue, obteve %s", n.Status)
	}
	if n.Attempts != 1 {
		t.Fatalf("esperava Attempts=1, obteve %d", n.Attempts)
	}
	if n.NextRetryAt == nil {
		t.Fatal("esperava NextRetryAt preenchido para retry futuro")
	}

	stored, err := repo.FindByID(context.Background(), n.ID)
	if err != nil {
		t.Fatalf("notificação deveria estar persistida mesmo com falha no enqueue: %v", err)
	}
	if stored.Status != domain.StatusRetrying {
		t.Fatalf("estado persistido deveria ser retrying, obteve %s", stored.Status)
	}
}

func TestRetry_ResetsAttemptsAndReenqueues(t *testing.T) {
	repo := newFakeRepository()
	producer := &fakeProducer{}
	svc := newService(repo, producer)

	n, err := svc.CreateNotification(context.Background(), CreateNotificationInput{
		Channel: domain.ChannelWebhook,
		Target:  "https://example.com/hook",
		Message: "olá",
	})
	if err != nil {
		t.Fatalf("falha ao criar notificação de teste: %v", err)
	}

	n.Attempts = 5
	n.Status = domain.StatusDLQ
	n.LastError = "erro anterior"
	if err := repo.Update(context.Background(), n); err != nil {
		t.Fatalf("falha ao preparar estado de teste: %v", err)
	}

	retried, err := svc.Retry(context.Background(), n.ID)
	if err != nil {
		t.Fatalf("Retry falhou: %v", err)
	}
	if retried.Attempts != 0 {
		t.Errorf("esperava Attempts=0 após retry manual, obteve %d", retried.Attempts)
	}
	if retried.Status != domain.StatusPending {
		t.Errorf("esperava status=pending após retry manual, obteve %s", retried.Status)
	}
	if retried.LastError != "" {
		t.Errorf("esperava LastError limpo após retry manual, obteve %q", retried.LastError)
	}
}

func TestRetry_RejectsNotificationNotInRetryOrDLQ(t *testing.T) {
	repo := newFakeRepository()
	producer := &fakeProducer{}
	svc := newService(repo, producer)

	n, err := svc.CreateNotification(context.Background(), CreateNotificationInput{
		Channel: domain.ChannelWebhook,
		Target:  "https://example.com/hook",
		Message: "olá",
	})
	if err != nil {
		t.Fatalf("falha ao criar notificação de teste: %v", err)
	}
	// n.Status == pending (não está em retrying nem dlq)

	if _, err := svc.Retry(context.Background(), n.ID); err == nil {
		t.Fatal("esperava erro ao tentar retry manual de notificação pending")
	}
}
