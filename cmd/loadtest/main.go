// loadtest é uma ferramenta de benchmark para o Notification Engine: cria N
// notificações via POST /api/v1/notifications com um nível de concorrência
// configurável, mede a latência de aceitação (tempo até o 202) e depois
// acompanha /api/v1/stats até todas as notificações criadas nesta rodada
// saírem de pending/retrying (sucesso ou DLQ), medindo a taxa de
// processamento fim-a-fim do worker. Não faz parte do runtime de produção —
// existe só para validar as características de desempenho documentadas em
// BENCHMARKS.md.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type stats struct {
	Total    int `json:"total"`
	Pending  int `json:"pending"`
	Success  int `json:"success"`
	Retrying int `json:"retrying"`
	DLQ      int `json:"dlq"`
}

func main() {
	apiURL := flag.String("api", "http://localhost:8080", "URL base da API")
	apiKey := flag.String("apikey", "", "API key, se a API exigir autenticação")
	n := flag.Int("n", 500, "total de notificações a criar")
	concurrency := flag.Int("concurrency", 50, "requisições de criação simultâneas")
	target := flag.String("target", "http://localhost:8090/", "target do webhook (aponte para o echoserver)")
	timeout := flag.Duration("timeout", 2*time.Minute, "tempo máximo de espera pela conclusão fim-a-fim")
	flag.Parse()

	client := &http.Client{Timeout: 10 * time.Second}

	baseline, err := fetchStats(client, *apiURL, *apiKey)
	if err != nil {
		log.Fatalf("falha ao obter stats iniciais: %v", err)
	}

	fmt.Printf("=== loadtest: N=%d concorrência=%d target=%s ===\n", *n, *concurrency, *target)
	fmt.Printf("baseline: total=%d success=%d dlq=%d\n\n", baseline.Total, baseline.Success, baseline.DLQ)

	latencies := make([]time.Duration, *n)
	var createErrors int64

	jobs := make(chan int, *n)
	for i := 0; i < *n; i++ {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	createStart := time.Now()

	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				lat, err := createNotification(client, *apiURL, *apiKey, *target)
				if err != nil {
					atomic.AddInt64(&createErrors, 1)
					log.Printf("erro ao criar notificação %d: %v", idx, err)
					continue
				}
				latencies[idx] = lat
			}
		}()
	}
	wg.Wait()
	createElapsed := time.Since(createStart)

	fmt.Println("--- Fase 1: criação (ingestão da API) ---")
	fmt.Printf("duração: %s | throughput: %.1f req/s | erros: %d\n",
		createElapsed.Round(time.Millisecond), float64(*n)/createElapsed.Seconds(), createErrors)
	printLatencyPercentiles(latencies)

	created := *n - int(createErrors)
	fmt.Println("\n--- Fase 2: processamento fim-a-fim (worker) ---")
	e2eStart := time.Now()
	deadline := time.Now().Add(*timeout)
	var final stats
	for {
		final, err = fetchStats(client, *apiURL, *apiKey)
		if err != nil {
			log.Fatalf("falha ao consultar stats: %v", err)
		}
		resolved := (final.Success - baseline.Success) + (final.DLQ - baseline.DLQ)
		if resolved >= created {
			break
		}
		if time.Now().After(deadline) {
			fmt.Printf("TIMEOUT após %s: apenas %d/%d notificações resolvidas (success=%d dlq=%d pending=%d retrying=%d)\n",
				*timeout, resolved, created, final.Success-baseline.Success, final.DLQ-baseline.DLQ, final.Pending, final.Retrying)
			os.Exit(1)
		}
		time.Sleep(100 * time.Millisecond)
	}
	e2eElapsed := time.Since(e2eStart)
	totalElapsed := time.Since(createStart)

	fmt.Printf("duração até resolver todas: %s (contando a partir do início da criação: %s)\n",
		e2eElapsed.Round(time.Millisecond), totalElapsed.Round(time.Millisecond))
	fmt.Printf("throughput fim-a-fim: %.1f notificações/s\n", float64(created)/totalElapsed.Seconds())
	fmt.Printf("resultado: criadas=%d success=%d dlq=%d\n", created, final.Success-baseline.Success, final.DLQ-baseline.DLQ)
}

func createNotification(client *http.Client, apiURL, apiKey, target string) (time.Duration, error) {
	body, _ := json.Marshal(map[string]string{
		"channel": "webhook",
		"target":  target,
		"message": "loadtest",
	})

	req, err := http.NewRequest(http.MethodPost, apiURL+"/api/v1/notifications", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusAccepted {
		return 0, fmt.Errorf("status inesperado: %d", resp.StatusCode)
	}
	return elapsed, nil
}

func fetchStats(client *http.Client, apiURL, apiKey string) (stats, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL+"/api/v1/stats", nil)
	if err != nil {
		return stats{}, err
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return stats{}, err
	}
	defer resp.Body.Close()

	var s stats
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return stats{}, err
	}
	return s, nil
}

func printLatencyPercentiles(latencies []time.Duration) {
	valid := make([]time.Duration, 0, len(latencies))
	for _, l := range latencies {
		if l > 0 {
			valid = append(valid, l)
		}
	}
	if len(valid) == 0 {
		fmt.Println("sem amostras de latência válidas")
		return
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i] < valid[j] })

	pct := func(p float64) time.Duration {
		idx := int(float64(len(valid)-1) * p)
		return valid[idx]
	}

	fmt.Printf("latência de criação: min=%s p50=%s p95=%s p99=%s max=%s\n",
		valid[0].Round(time.Millisecond),
		pct(0.50).Round(time.Millisecond),
		pct(0.95).Round(time.Millisecond),
		pct(0.99).Round(time.Millisecond),
		valid[len(valid)-1].Round(time.Millisecond),
	)
}
