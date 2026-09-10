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

// DiscordSender dispara mensagens para um Discord Webhook URL.
// Docs: https://discord.com/developers/docs/resources/webhook#execute-webhook
type DiscordSender struct {
	client *http.Client
}

func NewDiscordSender(client *http.Client) *DiscordSender {
	return &DiscordSender{client: client}
}

func (s *DiscordSender) Channel() domain.ChannelType {
	return domain.ChannelDiscord
}

type discordPayload struct {
	Content string `json:"content"`
}

// Send envia a notificação para o Discord. n.Target deve ser a URL completa
// do webhook (ex: https://discord.com/api/webhooks/{id}/{token}).
func (s *DiscordSender) Send(ctx context.Context, n *domain.Notification) error {
	body, err := json.Marshal(discordPayload{Content: n.Message})
	if err != nil {
		return fmt.Errorf("discord: falha ao serializar payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.Target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("discord: falha ao criar requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("discord: falha na requisição HTTP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord: resposta com status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
