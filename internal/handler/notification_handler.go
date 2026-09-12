package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"notification-engine/internal/domain"
	"notification-engine/internal/service"
)

// maxBulkSize limita o tamanho de um lote em /notifications/bulk, evitando
// que uma única requisição sobrecarregue a fila.
const maxBulkSize = 500

// NotificationHandler expõe os endpoints HTTP relacionados a notificações.
type NotificationHandler struct {
	service *service.NotificationService
}

func NewNotificationHandler(s *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{service: s}
}

type createNotificationResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// Create trata POST /api/v1/notifications
func (h *NotificationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input service.CreateNotificationInput

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "corpo da requisição inválido (JSON malformado)"})
		return
	}
	defer r.Body.Close()

	notification, err := h.service.CreateNotification(r.Context(), input)
	if err != nil {
		status := http.StatusInternalServerError
		if isValidationError(err) {
			status = http.StatusBadRequest
		} else {
			log.Printf("[handler] erro interno ao criar notificação: %v", err)
		}
		writeJSON(w, status, errorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, createNotificationResponse{
		ID:      notification.ID,
		Status:  string(notification.Status),
		Message: "notificação aceita e enfileirada para processamento",
	})
}

// Bulk trata POST /api/v1/notifications/bulk
func (h *NotificationHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	var inputs []service.CreateNotificationInput

	if err := json.NewDecoder(r.Body).Decode(&inputs); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "corpo da requisição inválido (esperado um array de notificações)"})
		return
	}
	defer r.Body.Close()

	if len(inputs) == 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "o array de notificações não pode ser vazio"})
		return
	}
	if len(inputs) > maxBulkSize {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: fmt.Sprintf("máximo de %d notificações por lote", maxBulkSize)})
		return
	}

	results := h.service.CreateBulk(r.Context(), inputs)
	writeJSON(w, http.StatusAccepted, results)
}

// Get trata GET /api/v1/notifications/{id}
func (h *NotificationHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	n, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "notificação não encontrada"})
		return
	}

	writeJSON(w, http.StatusOK, n)
}

// List trata GET /api/v1/notifications?status=&channel=&limit=
func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	filter := domain.ListFilter{
		Status:  domain.Status(r.URL.Query().Get("status")),
		Channel: domain.ChannelType(r.URL.Query().Get("channel")),
		Limit:   50,
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if limit, err := strconv.Atoi(raw); err == nil && limit > 0 && limit <= 500 {
			filter.Limit = limit
		}
	}

	items, err := h.service.List(r.Context(), filter)
	if err != nil {
		log.Printf("[handler] erro ao listar notificações: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "falha ao consultar notificações"})
		return
	}

	writeJSON(w, http.StatusOK, items)
}

// Stats trata GET /api/v1/stats
func (h *NotificationHandler) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.service.Stats(r.Context())
	if err != nil {
		log.Printf("[handler] erro ao calcular estatísticas: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "falha ao calcular estatísticas"})
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// Retry trata POST /api/v1/notifications/{id}/retry
func (h *NotificationHandler) Retry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	n, err := h.service.Retry(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, n)
}

// HealthCheck trata GET /health
func (h *NotificationHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func isValidationError(err error) bool {
	return errors.Is(err, domain.ErrInvalidChannel) ||
		errors.Is(err, domain.ErrEmptyTarget) ||
		errors.Is(err, domain.ErrEmptyMessage)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("[handler] falha ao escrever resposta JSON: %v", err)
	}
}
