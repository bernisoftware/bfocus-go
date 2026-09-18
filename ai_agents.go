package bfocus

import (
	"context"
	"net/http"
)

// AIAgentsService: agentes de IA — client.AIAgents. Escopos: ai_agents:read /
// ai_agents:preview. Exige o módulo de Atendimento: sem ele, ErrPermissionDenied com Code
// MODULE_NOT_CONTRACTED.
type AIAgentsService struct{ client *Client }

func agentPath(agentID string) (string, error) {
	seg, err := segment("agentID", agentID)
	if err != nil {
		return "", err
	}
	return "/ai-agents/" + seg, nil
}

// List lista os agentes de IA — GET /ai-agents.
func (s *AIAgentsService) List(ctx context.Context) ([]AIAgent, error) {
	return callList[AIAgent](ctx, s.client, apiRequest{method: http.MethodGet, path: "/ai-agents"})
}

// Get busca o agente (UUID) — GET /ai-agents/{agent_id}.
func (s *AIAgentsService) Get(ctx context.Context, agentID string) (*AIAgent, error) {
	path, err := agentPath(agentID)
	if err != nil {
		return nil, err
	}
	return callObject[AIAgent](ctx, s.client, apiRequest{method: http.MethodGet, path: path})
}

// Preview testa a resposta do agente a uma mensagem (1–4000), sem abrir atendimento —
// POST /ai-agents/{agent_id}/preview. Consome IA da conta. IA desligada: ErrConflict com
// Code AI_DISABLED. params pode ser nil.
func (s *AIAgentsService) Preview(ctx context.Context, agentID, message string, params *AIAgentPreviewParams, opts ...RequestOption) (*AIAgentPreview, error) {
	path, err := agentPath(agentID)
	if err != nil {
		return nil, err
	}
	var p AIAgentPreviewParams
	if params != nil {
		p = *params
	}
	body, err := marshalJSON(struct {
		Message string               `json:"message"`
		History []AIAgentPreviewTurn `json:"history,omitempty"`
	}{message, p.History})
	if err != nil {
		return nil, err
	}
	return callObject[AIAgentPreview](ctx, s.client, writeRequest(http.MethodPost, path+"/preview", body, opts))
}
