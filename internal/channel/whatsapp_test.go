package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"notification-engine/internal/domain"
)

func TestNormalizePhoneNumber(t *testing.T) {
	valid := map[string]string{
		"+55 11 99999-9999": "5511999999999",
		"(11) 99999-9999":   "11999999999",
		"5511999999999":     "5511999999999",
	}
	for input, want := range valid {
		got, err := normalizePhoneNumber(input)
		if err != nil {
			t.Errorf("normalizePhoneNumber(%q) retornou erro inesperado: %v", input, err)
		}
		if got != want {
			t.Errorf("normalizePhoneNumber(%q) = %q, esperava %q", input, got, want)
		}
	}

	if _, err := normalizePhoneNumber("123"); err == nil {
		t.Error("esperava erro para número curto demais")
	}
	if _, err := normalizePhoneNumber("não é um número"); err == nil {
		t.Error("esperava erro para entrada sem dígitos suficientes")
	}
}

func TestBuildWhatsAppTemplatePayload_WithParams(t *testing.T) {
	n := &domain.Notification{
		TemplateName:   "payment-approved",
		TemplateLocale: "pt_BR",
		TemplateParams: []string{"João", "149,90"},
	}

	payload := buildWhatsAppTemplatePayload("5511999999999", n)

	if payload["type"] != "template" {
		t.Fatalf("esperava type=template, obteve %v", payload["type"])
	}

	template := payload["template"].(map[string]any)
	if template["name"] != "payment-approved" {
		t.Errorf("esperava name=payment-approved, obteve %v", template["name"])
	}

	components := template["components"].([]map[string]any)
	if len(components) != 1 {
		t.Fatalf("esperava 1 componente (body), obteve %d", len(components))
	}

	params := components[0]["parameters"].([]map[string]any)
	if len(params) != 2 || params[0]["text"] != "João" || params[1]["text"] != "149,90" {
		t.Errorf("parâmetros do template incorretos: %+v", params)
	}
}

func TestBuildWhatsAppTemplatePayload_DefaultsLocale(t *testing.T) {
	n := &domain.Notification{TemplateName: "hello"}
	payload := buildWhatsAppTemplatePayload("5511999999999", n)

	template := payload["template"].(map[string]any)
	lang := template["language"].(map[string]any)
	if lang["code"] != defaultWhatsAppLocale {
		t.Errorf("esperava locale default %q, obteve %v", defaultWhatsAppLocale, lang["code"])
	}
}

func TestWhatsAppSender_Send_TextMessage(t *testing.T) {
	var capturedAuth string
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := &WhatsAppSender{
		client:        server.Client(),
		apiBase:       server.URL,
		phoneNumberID: "123456",
		accessToken:   "test-token",
	}

	n := &domain.Notification{
		Channel: domain.ChannelWhatsApp,
		Target:  "+55 11 99999-9999",
		Message: "olá do teste",
	}

	if err := sender.Send(context.Background(), n); err != nil {
		t.Fatalf("Send falhou: %v", err)
	}

	if capturedAuth != "Bearer test-token" {
		t.Errorf("esperava header Authorization com o token, obteve %q", capturedAuth)
	}
	if capturedBody["type"] != "text" {
		t.Errorf("esperava type=text, obteve %v", capturedBody["type"])
	}
	if capturedBody["to"] != "5511999999999" {
		t.Errorf("esperava to normalizado, obteve %v", capturedBody["to"])
	}
}

func TestWhatsAppSender_Send_PropagatesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"token inválido"}}`))
	}))
	defer server.Close()

	sender := &WhatsAppSender{
		client:        server.Client(),
		apiBase:       server.URL,
		phoneNumberID: "123456",
		accessToken:   "token-errado",
	}

	n := &domain.Notification{Target: "5511999999999", Message: "x"}
	if err := sender.Send(context.Background(), n); err == nil {
		t.Fatal("esperava erro propagado da API do WhatsApp")
	}
}

func TestWhatsAppSender_Channel(t *testing.T) {
	sender := NewWhatsAppSender(&http.Client{Timeout: time.Second}, WhatsAppConfig{})
	if sender.Channel() != domain.ChannelWhatsApp {
		t.Errorf("esperava canal whatsapp, obteve %s", sender.Channel())
	}
}
