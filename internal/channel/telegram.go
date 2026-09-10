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

// TelegramSender dispara mensagens via Telegram Bot API.
// Docs: https://core.telegram.org/bots/api#sendmessage
type TelegramSender struct {
	client   *http.Client
	botToken string
}

func NewTelegramSender(client *http.Client, botToken string) *TelegramSender {
	return &TelegramSender{client: client, botToken: botToken}
}

func (s *TelegramSender) Channel() domain.ChannelType {
	return domain.ChannelTelegram
}

type telegramPayload struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// Send envia a notificação via Telegram. n.Target deve conter o chat_id
// (numérico ou @username do canal/grupo) do destinatário.
func (s *TelegramSender) Send(ctx context.Context, n *domain.Notification) error {
	if s.botToken == "" {
		return fmt.Errorf("telegram: TELEGRAM_BOT_TOKEN não configurado")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", s.botToken)

	body, err := json.Marshal(telegramPayload{
		ChatID:    n.Target,
		Text:      n.Message,
		ParseMode: "HTML",
	})
	if err != nil {
		return fmt.Errorf("telegram: falha ao serializar payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: falha ao criar requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: falha na requisição HTTP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram: resposta com status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
