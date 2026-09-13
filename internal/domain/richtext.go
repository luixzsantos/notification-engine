package domain

import "strings"

// ParseRichMessage interpreta uma sintaxe leve, estilo Notion, embutida no
// texto livre de uma notificação: uma linha começando com "/h1 ", "/h2 " ou
// "/h3 " vira um título (o marcador some, o resto da linha é normalizado
// para o Markdown canônico "#"/"##"/"###", que cada canal depois renderiza
// do seu jeito — nativamente no Discord, como negrito no Telegram/WhatsApp,
// como <h1>/<h2>/<h3> no e-mail e como TextBlock dedicado no Teams); um
// bloco "/table" (uma linha por linha da tabela, células separadas por
// vírgula, terminando na primeira linha em branco ou no fim da mensagem)
// vira uma domain.Table estruturada e é removido do texto. Mensagens sem
// nenhum desses marcadores voltam inalteradas.
func ParseRichMessage(raw string) (message string, table *Table) {
	lines := strings.Split(raw, "\n")
	var out []string
	var tableRows [][]string
	inTable := false

	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r")

		if inTable {
			if strings.TrimSpace(trimmed) == "" {
				inTable = false
				continue
			}
			tableRows = append(tableRows, splitCSVLine(trimmed))
			continue
		}

		if strings.TrimSpace(trimmed) == "/table" {
			inTable = true
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "/h1 "):
			out = append(out, "# "+strings.TrimPrefix(trimmed, "/h1 "))
		case strings.HasPrefix(trimmed, "/h2 "):
			out = append(out, "## "+strings.TrimPrefix(trimmed, "/h2 "))
		case strings.HasPrefix(trimmed, "/h3 "):
			out = append(out, "### "+strings.TrimPrefix(trimmed, "/h3 "))
		default:
			out = append(out, trimmed)
		}
	}

	if len(tableRows) > 0 {
		table = &Table{Headers: tableRows[0], Rows: tableRows[1:]}
	}

	message = strings.TrimSpace(strings.Join(out, "\n"))
	return message, table
}

func splitCSVLine(line string) []string {
	parts := strings.Split(line, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}
