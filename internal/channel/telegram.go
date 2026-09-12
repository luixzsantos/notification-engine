package channel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"notification-engine/internal/domain"
)

// TelegramSender dispara mensagens via Telegram Bot API. Sem anexos, usa
// sendMessage; com anexos, usa sendPhoto (imagens) ou sendDocument (demais
// arquivos) — um envio multipart por anexo, com a mensagem como caption do
// primeiro.
// Docs: https://core.telegram.org/bots/api
type TelegramSender struct {
	client *http.Client
	token  string
}

func NewTelegramSender(client *http.Client, token string) *TelegramSender {
	return &TelegramSender{client: client, token: token}
}

func (s *TelegramSender) Channel() domain.ChannelType {
	return domain.ChannelTelegram
}

type telegramPayload struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

// Send envia a notificação para o Telegram. n.Target deve ser o chat_id do
// destinatário (obtido, por exemplo, falando com @userinfobot).
func (s *TelegramSender) Send(ctx context.Context, n *domain.Notification) error {
	if s.token == "" {
		return fmt.Errorf("telegram: TELEGRAM_BOT_TOKEN não configurado")
	}

	if len(n.Attachments) == 0 {
		return s.sendMessage(ctx, n)
	}

	for i, att := range n.Attachments {
		caption := ""
		if i == 0 {
			caption = n.Message
		}
		if err := s.sendAttachment(ctx, n.Target, caption, att); err != nil {
			return err
		}
	}

	return nil
}

func (s *TelegramSender) sendMessage(ctx context.Context, n *domain.Notification) error {
	body, err := json.Marshal(telegramPayload{ChatID: n.Target, Text: n.Message})
	if err != nil {
		return fmt.Errorf("telegram: falha ao serializar payload: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", s.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("telegram: falha ao criar requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	return s.do(req)
}

func (s *TelegramSender) sendAttachment(ctx context.Context, chatID, caption string, att domain.Attachment) error {
	raw, err := base64.StdEncoding.DecodeString(att.Data)
	if err != nil {
		return fmt.Errorf("telegram: anexo %q com base64 inválido: %w", att.Filename, err)
	}

	method := "sendDocument"
	field := "document"
	if strings.HasPrefix(att.ContentType, "image/") {
		method = "sendPhoto"
		field = "photo"
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("chat_id", chatID); err != nil {
		return fmt.Errorf("telegram: falha ao escrever chat_id: %w", err)
	}
	if caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return fmt.Errorf("telegram: falha ao escrever caption: %w", err)
		}
	}

	part, err := writer.CreateFormFile(field, att.Filename)
	if err != nil {
		return fmt.Errorf("telegram: falha ao criar parte do anexo %q: %w", att.Filename, err)
	}
	if _, err := part.Write(raw); err != nil {
		return fmt.Errorf("telegram: falha ao escrever anexo %q: %w", att.Filename, err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("telegram: falha ao finalizar multipart: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", s.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return fmt.Errorf("telegram: falha ao criar requisição: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	return s.do(req)
}

func (s *TelegramSender) do(req *http.Request) error {
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
