package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"notification-engine/internal/domain"
	"notification-engine/internal/service"
)

// NotificationHandler expõe os endpoints HTTP relacionados a notificações.
type NotificationHandler struct {
	service *service.NotificationService
}

func NewNotificationHandler(s *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{service: s}
}

// createNotificationResponse é o contrato de resposta devolvido ao cliente
// imediatamente após o enfileiramento (HTTP 202 Accepted).
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
