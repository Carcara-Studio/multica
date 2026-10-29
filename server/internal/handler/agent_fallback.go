package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/failover"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type FallbackRevision struct {
	Generation        string `json:"generation"`
	PrimaryGeneration string `json:"primary_generation"`
	StateVersion      int64  `json:"state_version"`
}

type AgentFallbackConfiguration struct {
	AgentID          string   `json:"agent_id"`
	RuntimeID        string   `json:"runtime_id"`
	Mode             string   `json:"mode"`
	Model            string   `json:"model"`
	ThinkingLevel    string   `json:"thinking_level"`
	ServiceTier      string   `json:"service_tier"`
	EnvironmentKeys  []string `json:"environment_keys"`
	ArgumentCount    int      `json:"argument_count"`
	HasMCPConfig     bool     `json:"has_mcp_config"`
	HasRuntimeConfig bool     `json:"has_runtime_config"`
}

// Configuration intent is not execution readiness. No endpoint in this slice
// enables fallback, starts a probe or returns an execution override document.
type AgentFallbackResponse struct {
	Configured       bool                        `json:"configured"`
	ExecutionEnabled bool                        `json:"execution_enabled"`
	Revision         FallbackRevision            `json:"revision"`
	Configuration    *AgentFallbackConfiguration `json:"configuration"`
}

type SaveAgentFallbackRequest struct {
	Mode             string                       `json:"mode"`
	RuntimeID        string                       `json:"runtime_id"`
	Model            string                       `json:"model"`
	ThinkingLevel    string                       `json:"thinking_level"`
	ServiceTier      string                       `json:"service_tier"`
	ExpectedRevision *FallbackRevision            `json:"expected_revision"`
	Overrides        *failover.ExecutionOverrides `json:"overrides"`
}

type DeleteAgentFallbackRequest struct {
	ExpectedRevision *FallbackRevision `json:"expected_revision"`
}

func fallbackResponse(v failover.ConfigView, configured bool) AgentFallbackResponse {
	reply := AgentFallbackResponse{Configured: configured, Revision: FallbackRevision{Generation: uuidToString(v.Revision.Generation), PrimaryGeneration: uuidToString(v.Revision.PrimaryGeneration), StateVersion: v.Revision.StateVersion}}
	if !configured {
		return reply
	}
	mode := "strict"
	if v.NativeLoginPath {
		mode = "native_login_path"
	}
	reply.Configuration = &AgentFallbackConfiguration{
		AgentID: uuidToString(v.AgentID), RuntimeID: uuidToString(v.RuntimeID), Mode: mode,
		Model: v.Model, ThinkingLevel: v.ThinkingLevel, ServiceTier: v.ServiceTier,
		EnvironmentKeys: v.EnvironmentKeys, ArgumentCount: v.ArgumentCount,
		HasMCPConfig: v.HasMCPConfig, HasRuntimeConfig: v.HasRuntimeConfig,
	}
	return reply
}

func (h *Handler) fallbackManager(w http.ResponseWriter, r *http.Request) (failover.ConfigManager, failover.ManagementActor, db.Agent, bool) {
	if middleware.DaemonIDFromContext(r.Context()) != "" {
		writeError(w, http.StatusForbidden, "daemon tokens cannot manage fallback configuration")
		return failover.ConfigManager{}, failover.ManagementActor{}, db.Agent{}, false
	}
	a, member, ok := h.authorizeAgentPrivateConfig(w, r, "fallback configuration")
	if !ok {
		return failover.ConfigManager{}, failover.ManagementActor{}, db.Agent{}, false
	}
	if h.TxStarter == nil || h.Queries == nil {
		writeError(w, http.StatusInternalServerError, "fallback configuration unavailable")
		return failover.ConfigManager{}, failover.ManagementActor{}, db.Agent{}, false
	}
	return failover.ConfigManager{Queries: h.Queries, Transactions: h.TxStarter}, failover.ManagementActor{Kind: "member", UserID: member.UserID, WorkspaceID: a.WorkspaceID}, a, true
}

func decodeFallbackRequest(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid fallback configuration request")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "expected one JSON object")
		return false
	}
	return true
}

func parseFallbackRevision(w http.ResponseWriter, v *FallbackRevision) (failover.ConfigRevision, bool) {
	if v == nil || v.StateVersion < 0 {
		writeError(w, http.StatusBadRequest, "expected_revision is required")
		return failover.ConfigRevision{}, false
	}
	if v.Generation == "" && v.PrimaryGeneration == "" && v.StateVersion == 0 {
		return failover.ConfigRevision{}, true
	}
	if v.StateVersion == 0 {
		writeError(w, http.StatusBadRequest, "invalid expected_revision")
		return failover.ConfigRevision{}, false
	}
	var generation pgtype.UUID
	if v.Generation != "" {
		var ok bool
		generation, ok = parseUUIDOrBadRequest(w, v.Generation, "expected_revision.generation")
		if !ok {
			return failover.ConfigRevision{}, false
		}
		if generation.Bytes == [16]byte{} {
			writeError(w, http.StatusBadRequest, "revision generation must be nonzero")
			return failover.ConfigRevision{}, false
		}
	}
	primary, ok := parseUUIDOrBadRequest(w, v.PrimaryGeneration, "expected_revision.primary_generation")
	if !ok {
		return failover.ConfigRevision{}, false
	}
	if primary.Bytes == [16]byte{} {
		writeError(w, http.StatusBadRequest, "revision generations must be nonzero")
		return failover.ConfigRevision{}, false
	}
	return failover.ConfigRevision{Generation: generation, PrimaryGeneration: primary, StateVersion: v.StateVersion}, true
}

func writeFallbackError(w http.ResponseWriter, err error) {
	status, message := http.StatusInternalServerError, "fallback configuration transaction failed"
	switch {
	case errors.Is(err, failover.ErrTargetAccess):
		status, message = http.StatusForbidden, "fallback configuration access denied"
	case errors.Is(err, failover.ErrTargetConflict):
		status, message = http.StatusConflict, "fallback configuration revision changed; read it again before saving"
	case errors.Is(err, failover.ErrTargetUnavailable):
		status, message = http.StatusNotFound, "fallback configuration not found"
	case errors.Is(err, failover.ErrTargetIncompatible), errors.Is(err, failover.ErrTargetDocument):
		status, message = http.StatusBadRequest, "invalid or incompatible fallback configuration"
	}
	// Never expose persistence errors containing rejected secret-bearing rows.
	writeError(w, status, message)
}

func (h *Handler) GetAgentFallback(w http.ResponseWriter, r *http.Request) {
	m, actor, a, ok := h.fallbackManager(w, r)
	if !ok {
		return
	}
	v, configured, err := m.InspectStatus(r.Context(), actor, a.ID)
	if err != nil {
		writeFallbackError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fallbackResponse(v, configured))
}

func (h *Handler) SaveAgentFallback(w http.ResponseWriter, r *http.Request) {
	m, actor, a, ok := h.fallbackManager(w, r)
	if !ok {
		return
	}
	var req SaveAgentFallbackRequest
	if !decodeFallbackRequest(w, r, &req) {
		return
	}
	if req.Mode != "native_login_path" || req.Model == "" || req.Model != strings.TrimSpace(req.Model) || req.Overrides == nil {
		writeError(w, http.StatusBadRequest, "native_login_path mode, explicit model and overrides are required")
		return
	}
	runtime, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	revision, ok := parseFallbackRevision(w, req.ExpectedRevision)
	if !ok {
		return
	}
	v, err := m.Save(r.Context(), actor, failover.SaveTargetConfig{
		AgentID: a.ID, RuntimeID: runtime, Model: req.Model, ThinkingLevel: req.ThinkingLevel,
		ServiceTier: req.ServiceTier, NativeLoginPath: true, Expected: revision, Overrides: *req.Overrides,
	}, agent.Catalog{Fallback: true})
	// Metadata stays pending: no CLI/network operation is performed under owner
	// locks and a client cannot supply a supposedly verified runtime catalog.
	if err != nil {
		writeFallbackError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fallbackResponse(v, true))
}

func (h *Handler) DeleteAgentFallback(w http.ResponseWriter, r *http.Request) {
	m, actor, a, ok := h.fallbackManager(w, r)
	if !ok {
		return
	}
	var req DeleteAgentFallbackRequest
	if !decodeFallbackRequest(w, r, &req) {
		return
	}
	revision, ok := parseFallbackRevision(w, req.ExpectedRevision)
	if !ok {
		return
	}
	if err := m.Remove(r.Context(), actor, a.ID, revision); err != nil {
		writeFallbackError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
