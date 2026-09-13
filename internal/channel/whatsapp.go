package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"notification-engine/internal/domain"
)

// whatsappAPIBase é o host fixo da WhatsApp Cloud API (Meta) — nunca vem do
// usuário, então não há superfície de SSRF aqui (diferente de webhook/discord).
const whatsappAPIBase = "https://graph.facebook.com/v21.0"

// defaultWhatsAppLocale é usado quando a notificação não especifica
// TemplateLocale.
const defaultWhatsAppLocale = "pt_BR"

// WhatsAppConfig agrupa as credenciais da WhatsApp Cloud API.
type WhatsAppConfig struct {
	PhoneNumberID string
	AccessToken   string
}

// WhatsAppSender dispara mensagens via WhatsApp Cloud API (Meta).
// Docs: https://developers.facebook.com/docs/whatsapp/cloud-api/reference/messages
type WhatsAppSender struct {
	client        *http.Client
	apiBase       string
	phoneNumberID string
	accessToken   string
}

func NewWhatsAppSender(client *http.Client, cfg WhatsAppConfig) *WhatsAppSender {
	return &WhatsAppSender{
		client:        client,
		apiBase:       whatsappAPIBase,
		phoneNumberID: cfg.PhoneNumberID,
		accessToken:   cfg.AccessToken,
	}
}

func (s *WhatsAppSender) Channel() domain.ChannelType {
	return domain.ChannelWhatsApp
}

// Send envia a notificação via WhatsApp. Se n.TemplateName estiver
// preenchido, envia como mensagem de template (obrigatório para mensagens
// de negócio iniciadas pela empresa, fora de uma janela de conversa de
// 24h — é o caminho confiável para alertas/notificações proativas). Caso
// contrário, envia como texto livre, o que só é aceito pela API dentro de
// uma janela de conversa ativa (o usuário precisa ter mandado mensagem nas
// últimas 24h).
func (s *WhatsAppSender) Send(ctx context.Context, n *domain.Notification) error {
	to, err := normalizePhoneNumber(n.Target)
	if err != nil {
		return fmt.Errorf("whatsapp: %w", err)
	}

	var payload map[string]any
	if n.TemplateName != "" {
		payload = buildWhatsAppTemplatePayload(to, n)
	} else {
		payload = map[string]any{
			"messaging_product": "whatsapp",
			"to":                to,
			"type":              "text",
			"text":              map[string]any{"body": n.Message},
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("whatsapp: falha ao serializar payload: %w", err)
	}

	url := fmt.Sprintf("%s/%s/messages", s.apiBase, s.phoneNumberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("whatsapp: falha ao criar requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.accessToken)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp: falha na requisição HTTP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("whatsapp: resposta com status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func buildWhatsAppTemplatePayload(to string, n *domain.Notification) map[string]any {
	locale := n.TemplateLocale
	if locale == "" {
		locale = defaultWhatsAppLocale
	}

	components := []map[string]any{}
	if len(n.TemplateParams) > 0 {
		params := make([]map[string]any, len(n.TemplateParams))
		for i, p := range n.TemplateParams {
			params[i] = map[string]any{"type": "text", "text": p}
		}
		components = append(components, map[string]any{
			"type":       "body",
			"parameters": params,
		})
	}

	return map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "template",
		"template": map[string]any{
			"name":       n.TemplateName,
			"language":   map[string]any{"code": locale},
			"components": components,
		},
	}
}

// normalizePhoneNumber remove espaços, parênteses e traços comuns em
// números de telefone digitados por humanos, mantendo um "+" inicial
// opcional. A WhatsApp Cloud API espera o número em formato E.164 sem o "+"
// (ex: "5511999999999").
func normalizePhoneNumber(raw string) (string, error) {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()

	if len(digits) < 8 {
		return "", fmt.Errorf("número de telefone inválido: %q", raw)
	}

	return digits, nil
}
