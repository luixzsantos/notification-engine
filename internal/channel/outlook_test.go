package channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"notification-engine/internal/domain"
)

func newTestOutlookSender(t *testing.T, loginServer, graphServer *httptest.Server) *OutlookSender {
	t.Helper()
	return &OutlookSender{
		client:       http.DefaultClient,
		loginBase:    loginServer.URL,
		graphBase:    graphServer.URL,
		tenantID:     "tenant-123",
		clientID:     "client-abc",
		clientSecret: "secret-xyz",
		senderEmail:  "alerts@empresa.com",
	}
}

func tokenServer(t *testing.T, tokenCalls *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*tokenCalls++

		body, _ := url.ParseQuery(readBody(r))
		if body.Get("grant_type") != "client_credentials" {
			t.Errorf("esperava grant_type=client_credentials, obteve %q", body.Get("grant_type"))
		}

		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-token",
			"expires_in":   3600,
		})
	}))
}

func readBody(r *http.Request) string {
	buf, _ := io.ReadAll(r.Body)
	return string(buf)
}

func TestOutlookSender_Send_Success(t *testing.T) {
	tokenCalls := 0
	login := tokenServer(t, &tokenCalls)
	defer login.Close()

	var capturedAuth string
	var capturedPath string
	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	defer graph.Close()

	sender := newTestOutlookSender(t, login, graph)

	n := &domain.Notification{
		Channel: domain.ChannelOutlook,
		Target:  "destinatario@empresa.com",
		Subject: "Assunto de teste",
		Message: "corpo do e-mail",
	}

	if err := sender.Send(context.Background(), n); err != nil {
		t.Fatalf("Send falhou: %v", err)
	}

	if capturedAuth != "Bearer fake-token" {
		t.Errorf("esperava Authorization com o token obtido, obteve %q", capturedAuth)
	}
	if !strings.Contains(capturedPath, "alerts@empresa.com") {
		t.Errorf("esperava path com o e-mail remetente, obteve %q", capturedPath)
	}
	if tokenCalls != 1 {
		t.Errorf("esperava 1 chamada ao endpoint de token, obteve %d", tokenCalls)
	}
}

func TestOutlookSender_Send_ReusesCachedToken(t *testing.T) {
	tokenCalls := 0
	login := tokenServer(t, &tokenCalls)
	defer login.Close()

	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer graph.Close()

	sender := newTestOutlookSender(t, login, graph)
	n := &domain.Notification{Target: "x@empresa.com", Subject: "s", Message: "m"}

	for i := 0; i < 3; i++ {
		if err := sender.Send(context.Background(), n); err != nil {
			t.Fatalf("Send #%d falhou: %v", i, err)
		}
	}

	if tokenCalls != 1 {
		t.Errorf("esperava reaproveitar o token cacheado (1 chamada ao endpoint de token), obteve %d chamadas", tokenCalls)
	}
}

func TestOutlookSender_Send_RefreshesExpiredToken(t *testing.T) {
	tokenCalls := 0
	login := tokenServer(t, &tokenCalls)
	defer login.Close()

	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer graph.Close()

	sender := newTestOutlookSender(t, login, graph)
	n := &domain.Notification{Target: "x@empresa.com", Subject: "s", Message: "m"}

	if err := sender.Send(context.Background(), n); err != nil {
		t.Fatalf("primeiro Send falhou: %v", err)
	}

	// Força o token cacheado a já estar vencido.
	sender.mu.Lock()
	sender.tokenExpiry = time.Now().Add(-time.Minute)
	sender.mu.Unlock()

	if err := sender.Send(context.Background(), n); err != nil {
		t.Fatalf("segundo Send falhou: %v", err)
	}

	if tokenCalls != 2 {
		t.Errorf("esperava renovar o token expirado (2 chamadas ao endpoint de token), obteve %d", tokenCalls)
	}
}

func TestOutlookSender_Send_PropagatesGraphAPIError(t *testing.T) {
	tokenCalls := 0
	login := tokenServer(t, &tokenCalls)
	defer login.Close()

	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"sem permissão Mail.Send"}}`))
	}))
	defer graph.Close()

	sender := newTestOutlookSender(t, login, graph)
	n := &domain.Notification{Target: "x@empresa.com", Subject: "s", Message: "m"}

	if err := sender.Send(context.Background(), n); err == nil {
		t.Fatal("esperava erro propagado do Graph API")
	}
}

func TestOutlookSender_Send_PropagatesTokenError(t *testing.T) {
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer login.Close()

	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("não deveria chamar o Graph API sem um token válido")
	}))
	defer graph.Close()

	sender := newTestOutlookSender(t, login, graph)
	n := &domain.Notification{Target: "x@empresa.com", Subject: "s", Message: "m"}

	if err := sender.Send(context.Background(), n); err == nil {
		t.Fatal("esperava erro ao falhar a obtenção do token")
	}
}

func TestOutlookSender_Channel(t *testing.T) {
	sender := NewOutlookSender(&http.Client{Timeout: time.Second}, OutlookConfig{})
	if sender.Channel() != domain.ChannelOutlook {
		t.Errorf("esperava canal outlook, obteve %s", sender.Channel())
	}
}
