# Webhook & Notification Engine

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version" />
  <img src="https://img.shields.io/badge/Redis-Streams-DC382D?style=for-the-badge&logo=redis&logoColor=white" alt="Redis" />
  <img src="https://img.shields.io/badge/Docker-Ready-2496ED?style=for-the-badge&logo=docker&logoColor=white" alt="Docker" />
  <img src="https://img.shields.io/badge/License-MIT-green.svg?style=for-the-badge" alt="License" />
</p>

Serviço assíncrono de alto desempenho para disparo de notificações multicanais (**Discord**, **Telegram**, **Gmail** e **Webhooks genéricos**), construído em **Go** seguindo os princípios de **Clean Architecture**, com concorrência nativa via goroutines e fila de processamento no **Redis Streams**.

---

## 📌 Índice

- [Arquitetura](#-arquitetura)
- [Stack Tecnológica](#-stack-tecnológica)
- [Estrutura de Pastas](#-estrutura-de-pastas)
- [Pré-requisitos](#-pré-requisitos)
- [Como Rodar o Projeto](#-como-rodar-o-projeto)
- [Variáveis de Ambiente](#-variáveis-de-ambiente)
- [Uso da API (Endpoints)](#-uso-da-api-endpoints)
- [Roadmap (V2)](#-roadmap-v2)
- [Autor](#-autor)
- [Licença](#-licença)

---

## 🏗️ Arquitetura

```text
Cliente → POST /api/v1/notifications → API (Go)
                                         │
                                         ▼
                                  Redis Stream (fila)
                                         │
                                         ▼
                                Worker (N goroutines consumidoras)
                                         │
                          ┌──────────────┼──────────────┬──────────────┐
                          ▼              ▼              ▼              ▼
                       Discord        Telegram        Gmail         Webhook
                       Webhook        Bot API        (SMTP)        genérico
