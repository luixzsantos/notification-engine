package channel

import "notification-engine/internal/domain"

// appendMonospaceTable concatena a mensagem com a tabela (se houver)
// renderizada como texto monoespaçado dentro de um bloco de código —
// usado pelos canais sem suporte nativo a tabelas (Discord, Telegram,
// WhatsApp em texto livre). table pode ser nil.
func appendMonospaceTable(message string, table *domain.Table) string {
	rendered := table.FormatMonospace()
	if rendered == "" {
		return message
	}

	block := "```\n" + rendered + "\n```"
	if message == "" {
		return block
	}
	return message + "\n\n" + block
}
