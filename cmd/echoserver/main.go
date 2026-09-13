// echoserver é um HTTP server mínimo usado como alvo de webhook nos
// benchmarks (cmd/loadtest): responde 200 OK imediatamente a qualquer
// requisição, isolando a medição da capacidade da própria engine em vez de
// misturar com a latência/rate limit de um serviço externo real (Discord,
// webhook.site etc). Não faz parte do runtime de produção.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

func main() {
	port := flag.String("port", "8090", "porta em que o echoserver escuta")
	latency := flag.Duration("latency", 0, "atraso artificial antes de responder (simula um serviço externo lento)")
	flag.Parse()

	var count int64

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if *latency > 0 {
			time.Sleep(*latency)
		}
		atomic.AddInt64(&count, 1)
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/__count", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s total recebido: %d\n", time.Now().Format(time.RFC3339), atomic.LoadInt64(&count))
	})

	log.Printf("[echoserver] escutando em :%s (latência artificial: %s)", *port, *latency)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
