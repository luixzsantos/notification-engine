package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"notification-engine/internal/domain"
	"notification-engine/internal/security"
)

// TeamsSender dispara mensagens para o Microsoft Teams via um Workflow do
// Power Automate ("When a Teams webhook request is received"), o
// substituto oficial dos antigos Incoming Webhooks do Office 365
// Connectors — a Microsoft descontinuou o modelo antigo, então n.Target
// deve ser a URL do gatilho do Workflow, não uma URL de "connector" clássica.
//
// O corpo enviado é um Adaptive Card. Como o Power Automate infere o schema
// esperado a partir de um payload de exemplo configurado por quem cria o
// Workflow, o body exato abaixo deve ser usado como "exemplo" ao configurar
// o gatilho (documentado no README) para o fluxo aceitar esse formato.
type TeamsSender struct {
	client *http.Client
	// validateHost é extraído como campo (em vez de chamar validateTeamsHost
	// diretamente) só para permitir testar o restante de Send() com um
	// httptest.Server (host 127.0.0.1) sem precisar de uma URL real do Power
	// Automate; em produção é sempre validateTeamsHost.
	validateHost func(string) error
}

func NewTeamsSender(client *http.Client) *TeamsSender {
	return &TeamsSender{client: client, validateHost: ValidateTeamsHost}
}

func (s *TeamsSender) Channel() domain.ChannelType {
	return domain.ChannelTeams
}

// Send envia a notificação para o Teams. n.Target deve ser a URL do
// gatilho do Workflow do Power Automate.
func (s *TeamsSender) Send(ctx context.Context, n *domain.Notification) error {
	if err := s.validateHost(n.Target); err != nil {
		return err
	}

	body, err := json.Marshal(buildAdaptiveCardEnvelope(n))
	if err != nil {
		return fmt.Errorf("teams: falha ao serializar payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.Target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("teams: falha ao criar requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("teams: falha na requisição HTTP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("teams: resposta com status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ValidateTeamsHost restringe o destino a URLs de Workflow do Power
// Automate (host terminado em ".logic.azure.com", o domínio real usado
// pelos gatilhos de Workflow/Logic Apps). Além de mitigar SSRF (já
// coberto de forma genérica pelo http.Client hardened do registry), evita
// que o canal "teams" seja usado como proxy genérico para outro servidor
// público. Exportada (chamada também pelo service, na criação) para
// rejeitar um host errado com 400 na hora, em vez de só descobrir isso
// depois de 5 tentativas de retry inúteis no worker.
func ValidateTeamsHost(target string) error {
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("%w: URL malformada: %v", security.ErrBlockedTarget, err)
	}

	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".logic.azure.com") {
		return fmt.Errorf("%w: destino deve ser a URL de um Workflow do Power Automate (*.logic.azure.com), recebido: %s", security.ErrBlockedTarget, host)
	}

	return nil
}

// buildAdaptiveCardEnvelope monta o envelope "message" com um Adaptive
// Card — o formato que a ação "Post card in a chat or channel" do Power
// Automate espera. Inclui o(s) TextBlock(s) da mensagem — um por título
// ("#"/"##"/"###", normalizado por domain.ParseRichMessage a partir de
// "/h1"/"/h2"/"/h3"), com tamanho e peso de acordo com o nível, mais um
// parágrafo para o texto comum — e uma tabela nativa do Adaptive Card (se
// n.Table estiver presente).
func buildAdaptiveCardEnvelope(n *domain.Notification) map[string]any {
	var body []map[string]any

	body = append(body, buildMessageBlocks(n.Message)...)

	if n.Table != nil && len(n.Table.Rows) > 0 {
		body = append(body, buildAdaptiveCardTable(n.Table))
	}

	card := map[string]any{
		"type":    "AdaptiveCard",
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"version": "1.5",
		"body":    body,
	}

	return map[string]any{
		"type": "message",
		"attachments": []map[string]any{
			{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content":     card,
			},
		},
	}
}

// buildMessageBlocks converte a mensagem em uma lista de TextBlock do
// Adaptive Card: cada linha de título vira seu próprio TextBlock, com
// tamanho maior e negrito de acordo com o nível (a única renderização
// realmente nativa de título entre os canais suportados); linhas comuns
// consecutivas são agrupadas num único TextBlock de parágrafo, sem negrito
// forçado (diferente do título, que carrega toda a ênfase visual agora).
func buildMessageBlocks(message string) []map[string]any {
	if message == "" {
		return nil
	}

	lines := headingLines(message)
	var blocks []map[string]any
	var paragraph []string

	flush := func() {
		if len(paragraph) == 0 {
			return
		}
		blocks = append(blocks, map[string]any{
			"type": "TextBlock",
			"text": strings.Join(paragraph, "\n"),
			"wrap": true,
		})
		paragraph = nil
	}

	for _, l := range lines {
		if l.Level == 0 {
			paragraph = append(paragraph, l.Text)
			continue
		}
		flush()

		size := "Medium"
		switch l.Level {
		case 1:
			size = "ExtraLarge"
		case 2:
			size = "Large"
		}
		blocks = append(blocks, map[string]any{
			"type":   "TextBlock",
			"text":   l.Text,
			"wrap":   true,
			"size":   size,
			"weight": "Bolder",
		})
	}
	flush()

	return blocks
}

// buildAdaptiveCardTable converte domain.Table para o elemento "Table" do
// Adaptive Card 1.5, com a primeira linha em negrito quando há cabeçalho.
func buildAdaptiveCardTable(t *domain.Table) map[string]any {
	columns := len(t.Headers)
	for _, row := range t.Rows {
		if len(row) > columns {
			columns = len(row)
		}
	}

	columnDefs := make([]map[string]any, columns)
	for i := range columnDefs {
		columnDefs[i] = map[string]any{"width": 1}
	}

	var rows []map[string]any
	if len(t.Headers) > 0 {
		rows = append(rows, adaptiveTableRow(t.Headers, columns, true))
	}
	for _, row := range t.Rows {
		rows = append(rows, adaptiveTableRow(row, columns, false))
	}

	return map[string]any{
		"type":                           "Table",
		"columns":                        columnDefs,
		"rows":                           rows,
		"firstRowAsHeaders":              false, // já formatamos o header manualmente em negrito
		"showGridLines":                  true,
		"gridStyle":                      "default",
		"horizontalCellContentAlignment": "left",
	}
}

func adaptiveTableRow(cells []string, columns int, bold bool) map[string]any {
	tableCells := make([]map[string]any, columns)
	for i := 0; i < columns; i++ {
		text := ""
		if i < len(cells) {
			text = cells[i]
		}

		textBlock := map[string]any{"type": "TextBlock", "text": text, "wrap": true}
		if bold {
			textBlock["weight"] = "Bolder"
		}

		tableCells[i] = map[string]any{
			"type":  "TableCell",
			"items": []map[string]any{textBlock},
		}
	}

	return map[string]any{
		"type":  "TableRow",
		"cells": tableCells,
	}
}
