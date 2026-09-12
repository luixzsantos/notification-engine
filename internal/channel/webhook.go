package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"notification-engine/internal/domain"
)

// WebhookSender dispara um POST HTTP customizado para qualquer URL de destino,
// permitindo payload e headers arbitrários definidos pelo cliente da API.
type WebhookSender struct {
	client *http.Client
}

func NewWebhookSender(client *http.Client) *WebhookSender {
	return &WebhookSender{client: client}
}

func (s *WebhookSender) Channel() domain.ChannelType {
	return domain.ChannelWebhook
}

// Send envia a notificação para n.Target (URL arbitrária). Se n.Payload for
// informado, ele é usado como corpo da requisição; caso contrário, um corpo
// padrão { "message": "..." } é enviado. Anexos (n.Attachments), quando
// presentes, são incluídos em base64 na chave "attachments" do corpo.
// Headers customizados de n.Headers são aplicados sobre a requisição.
func (s *WebhookSender) Send(ctx context.Context, n *domain.Notification) error {
	body := make(map[string]any)
	if n.Payload != nil {
		for k, v := range n.Payload {
			body[k] = v
		}
	} else {
		body["message"] = n.Message
	}
	if len(n.Attachments) > 0 {
		if _, exists := body["attachments"]; !exists {
			body["attachments"] = n.Attachments
		}
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("webhook: falha ao serializar payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.Target, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("webhook: falha ao criar requisição: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range n.Headers {
		req.Header.Set(key, value)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: falha na requisição HTTP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook: resposta com status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
