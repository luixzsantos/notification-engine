package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"notification-engine/internal/domain"
)

func TestValidateTeamsHost(t *testing.T) {
	valid := []string{
		"https://prod-00.westus.logic.azure.com:443/workflows/abc/triggers/manual/paths/invoke",
		"https://prod-12.eastus2.logic.azure.com/workflows/xyz",
	}
	for _, target := range valid {
		if err := ValidateTeamsHost(target); err != nil {
			t.Errorf("esperava %q válido, obteve erro: %v", target, err)
		}
	}

	invalid := []string{
		"https://evil.com/workflows/abc",
		"https://outlook.office.com/webhook/abc", // webhook clássico descontinuado
		"http://169.254.169.254/latest/meta-data",
		"não é uma url",
	}
	for _, target := range invalid {
		if err := ValidateTeamsHost(target); err == nil {
			t.Errorf("esperava %q inválido (fora de *.logic.azure.com), mas passou", target)
		}
	}
}

func TestBuildAdaptiveCardEnvelope_MessageOnly(t *testing.T) {
	n := &domain.Notification{Message: "Alerta de teste"}
	envelope := buildAdaptiveCardEnvelope(n)

	if envelope["type"] != "message" {
		t.Fatalf("esperava type=message, obteve %v", envelope["type"])
	}

	attachments := envelope["attachments"].([]map[string]any)
	if len(attachments) != 1 {
		t.Fatalf("esperava 1 attachment, obteve %d", len(attachments))
	}
	if attachments[0]["contentType"] != "application/vnd.microsoft.card.adaptive" {
		t.Errorf("contentType incorreto: %v", attachments[0]["contentType"])
	}

	card := attachments[0]["content"].(map[string]any)
	body := card["body"].([]map[string]any)
	if len(body) != 1 {
		t.Fatalf("esperava 1 elemento no body (TextBlock), obteve %d", len(body))
	}
	if body[0]["text"] != "Alerta de teste" {
		t.Errorf("texto incorreto: %v", body[0]["text"])
	}
}

func TestBuildAdaptiveCardEnvelope_WithTable(t *testing.T) {
	n := &domain.Notification{
		Message: "Resumo",
		Table: &domain.Table{
			Headers: []string{"Nome", "Status"},
			Rows:    [][]string{{"João", "Aprovado"}},
		},
	}

	envelope := buildAdaptiveCardEnvelope(n)
	attachments := envelope["attachments"].([]map[string]any)
	card := attachments[0]["content"].(map[string]any)
	body := card["body"].([]map[string]any)

	if len(body) != 2 {
		t.Fatalf("esperava 2 elementos (TextBlock + Table), obteve %d", len(body))
	}
	if body[1]["type"] != "Table" {
		t.Fatalf("esperava segundo elemento do tipo Table, obteve %v", body[1]["type"])
	}

	rows := body[1]["rows"].([]map[string]any)
	if len(rows) != 2 { // header row + 1 data row
		t.Fatalf("esperava 2 linhas (header + 1 dado), obteve %d", len(rows))
	}
}

func TestBuildMessageBlocks_HeadingsGetOwnTextBlockWithSize(t *testing.T) {
	blocks := buildMessageBlocks("# Título\ntexto do parágrafo\n## Sub")

	if len(blocks) != 3 {
		t.Fatalf("esperava 3 blocks (h1, parágrafo, h2), obteve %d: %+v", len(blocks), blocks)
	}
	if blocks[0]["text"] != "Título" || blocks[0]["size"] != "ExtraLarge" || blocks[0]["weight"] != "Bolder" {
		t.Errorf("block de h1 incorreto: %+v", blocks[0])
	}
	if blocks[1]["text"] != "texto do parágrafo" {
		t.Errorf("block de parágrafo incorreto: %+v", blocks[1])
	}
	if _, hasWeight := blocks[1]["weight"]; hasWeight {
		t.Errorf("parágrafo comum não deveria ter weight forçado: %+v", blocks[1])
	}
	if blocks[2]["text"] != "Sub" || blocks[2]["size"] != "Large" {
		t.Errorf("block de h2 incorreto: %+v", blocks[2])
	}
}

func TestBuildMessageBlocks_Empty(t *testing.T) {
	if blocks := buildMessageBlocks(""); blocks != nil {
		t.Errorf("esperava nil para mensagem vazia, obteve %+v", blocks)
	}
}

func TestTeamsSender_Send_Success(t *testing.T) {
	var capturedBody map[string]any
	var capturedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// httptest usa 127.0.0.1 como host — substituímos a checagem de host
	// (testada isoladamente em TestValidateTeamsHost) por uma que sempre
	// aceita, pra poder exercitar o restante de Send() de ponta a ponta.
	sender := &TeamsSender{
		client:       server.Client(),
		validateHost: func(string) error { return nil },
	}

	n := &domain.Notification{Target: server.URL, Message: "Alerta de teste"}
	if err := sender.Send(context.Background(), n); err != nil {
		t.Fatalf("Send falhou: %v", err)
	}

	if capturedContentType != "application/json" {
		t.Errorf("esperava Content-Type application/json, obteve %q", capturedContentType)
	}
	if capturedBody["type"] != "message" {
		t.Errorf("esperava body type=message, obteve %v", capturedBody["type"])
	}
}

func TestTeamsSender_Send_PropagatesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("payload inválido"))
	}))
	defer server.Close()

	sender := &TeamsSender{
		client:       server.Client(),
		validateHost: func(string) error { return nil },
	}

	n := &domain.Notification{Target: server.URL, Message: "x"}
	if err := sender.Send(context.Background(), n); err == nil {
		t.Fatal("esperava erro propagado da resposta do Workflow")
	}
}

func TestTeamsSender_Send_RejectsInvalidHost(t *testing.T) {
	sender := NewTeamsSender(http.DefaultClient)
	n := &domain.Notification{
		Target:  "https://evil.com/hook",
		Message: "teste",
	}

	if err := sender.Send(context.Background(), n); err == nil {
		t.Fatal("esperava erro para host fora de *.logic.azure.com")
	}
}
