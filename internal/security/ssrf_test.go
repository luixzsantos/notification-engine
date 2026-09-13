package security

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateTargetURL_BlocksKnownBadTargets(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1/hook",
		"http://localhost/hook",
		"http://localhost:8080/hook",
		"http://169.254.169.254/latest/meta-data", // metadados de nuvem (AWS/GCP/Azure)
		"http://10.0.0.5/internal",
		"http://172.16.0.1/internal",
		"http://192.168.1.1/internal",
		"http://[::1]/hook",
		"ftp://example.com/hook", // esquema não-http
		"não é uma url",
	}

	for _, target := range blocked {
		if err := ValidateTargetURL(target, false); err == nil {
			t.Errorf("esperava bloqueio para %q, mas passou na validação", target)
		} else if !errors.Is(err, ErrBlockedTarget) {
			t.Errorf("esperava erro do tipo ErrBlockedTarget para %q, obteve: %v", target, err)
		}
	}
}

func TestValidateTargetURL_AllowsPublicTargets(t *testing.T) {
	allowed := []string{
		"https://discord.com/api/webhooks/123/abc",
		"https://example.com/hook",
		"http://8.8.8.8/hook", // IP público (DNS do Google)
	}

	for _, target := range allowed {
		if err := ValidateTargetURL(target, false); err != nil {
			t.Errorf("esperava %q permitido, mas foi bloqueado: %v", target, err)
		}
	}
}

func TestValidateTargetURL_AllowPrivateNetworksBypassesCheck(t *testing.T) {
	if err := ValidateTargetURL("http://127.0.0.1/hook", true); err != nil {
		t.Fatalf("com allowPrivateNetworks=true, esperava nenhum erro, obteve: %v", err)
	}
}

func TestSafeHTTPClient_BlocksLoopbackAtDialTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := SafeHTTPClient(2*time.Second, false)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	_, err := client.Do(req)

	if err == nil {
		t.Fatal("esperava erro ao conectar em servidor de teste local (loopback), mas a requisição passou")
	}
	if !strings.Contains(err.Error(), "security:") && !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("esperava erro relacionado a SSRF, obteve: %v", err)
	}
}

func TestSafeHTTPClient_AllowsPublicHost(t *testing.T) {
	// Não faz uma chamada de rede real (ambiente de CI pode não ter
	// internet); apenas garante que allowPrivateNetworks=true não quebra a
	// construção do client e permite requisições locais, prova de que o
	// bloqueio é condicional e não hardcoded.
	client := SafeHTTPClient(2*time.Second, true)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("com allowPrivateNetworks=true, esperava sucesso ao acessar loopback, obteve erro: %v", err)
	}
	resp.Body.Close()
}
