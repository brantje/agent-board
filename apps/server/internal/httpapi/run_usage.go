package httpapi

import (
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/app"
)

type RunUsageEvidenceDTO struct {
	ContextTokens      int64    `json:"contextTokens"`
	ContextLimitTokens *int64   `json:"contextLimitTokens"`
	InputTokens        int64    `json:"inputTokens"`
	OutputTokens       int64    `json:"outputTokens"`
	CacheReadTokens    int64    `json:"cacheReadTokens"`
	AverageWaitMs      *float64 `json:"averageWaitMs"`
	TokensPerSecond    *float64 `json:"tokensPerSecond"`
}

func (a *api) getRunUsage(w http.ResponseWriter, r *http.Request) {
	projectID, runID, ok := evidenceRunPath(w, r)
	if !ok {
		return
	}
	value, err := a.runEvidence.Usage(r.Context(), projectID, runID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runUsageEvidenceDTO(value))
}

func runUsageEvidenceDTO(value *app.RunUsage) *RunUsageEvidenceDTO {
	if value == nil {
		return nil
	}
	return &RunUsageEvidenceDTO{
		ContextTokens:      value.ContextTokens,
		ContextLimitTokens: value.ContextLimitTokens,
		InputTokens:        value.InputTokens,
		OutputTokens:       value.OutputTokens,
		CacheReadTokens:    value.CacheReadTokens,
		AverageWaitMs:      value.AverageWaitMs,
		TokensPerSecond:    value.TokensPerSecond,
	}
}
