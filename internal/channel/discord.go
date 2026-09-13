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
	"net/url"
	"strings"

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
// do webhook (ex: https://discord.com/api/webhooks/{id}/{token}). Quando
// n.Attachments está presente, envia como multipart/form-data (files[n] +
// payload_json), do contrário usa o corpo JSON simples.
func (s *DiscordSender) Send(ctx context.Context, n *domain.Notification) error {
	if err := validateDiscordHost(n.Target); err != nil {
		return err
	}

	var req *http.Request
	var err error

	if len(n.Attachments) > 0 {
		req, err = s.buildMultipartRequest(ctx, n)
	} else {
		req, err = s.buildJSONRequest(ctx, n)
	}
	if err != nil {
		return err
	}

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

// validateDiscordHost restringe o destino a hosts realmente pertencentes ao
// Discord (discord.com/discordapp.com). Além de mitigar SSRF (que já é
// tratado de forma genérica pelo http.Client hardened do registry), isso
// impede que o canal "discord" seja usado como um proxy genérico para
// disparar requisições HTTP a qualquer outro servidor público.
func validateDiscordHost(target string) error {
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("discord: URL de destino inválida: %w", err)
	}

	host := strings.ToLower(u.Hostname())
	if host != "discord.com" && !strings.HasSuffix(host, ".discord.com") &&
		host != "discordapp.com" && !strings.HasSuffix(host, ".discordapp.com") {
		return fmt.Errorf("discord: destino deve ser um webhook em discord.com ou discordapp.com, recebido: %s", host)
	}

	return nil
}

func (s *DiscordSender) buildJSONRequest(ctx context.Context, n *domain.Notification) (*http.Request, error) {
	body, err := json.Marshal(discordPayload{Content: n.Message})
	if err != nil {
		return nil, fmt.Errorf("discord: falha ao serializar payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.Target, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("discord: falha ao criar requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

func (s *DiscordSender) buildMultipartRequest(ctx context.Context, n *domain.Notification) (*http.Request, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	payloadJSON, err := json.Marshal(discordPayload{Content: n.Message})
	if err != nil {
		return nil, fmt.Errorf("discord: falha ao serializar payload: %w", err)
	}
	if err := writer.WriteField("payload_json", string(payloadJSON)); err != nil {
		return nil, fmt.Errorf("discord: falha ao escrever payload_json: %w", err)
	}

	for i, att := range n.Attachments {
		raw, err := base64.StdEncoding.DecodeString(att.Data)
		if err != nil {
			return nil, fmt.Errorf("discord: anexo %q com base64 inválido: %w", att.Filename, err)
		}

		part, err := writer.CreateFormFile(fmt.Sprintf("files[%d]", i), att.Filename)
		if err != nil {
			return nil, fmt.Errorf("discord: falha ao criar parte do anexo %q: %w", att.Filename, err)
		}
		if _, err := part.Write(raw); err != nil {
			return nil, fmt.Errorf("discord: falha ao escrever anexo %q: %w", att.Filename, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("discord: falha ao finalizar multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.Target, &buf)
	if err != nil {
		return nil, fmt.Errorf("discord: falha ao criar requisição: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	return req, nil
}
