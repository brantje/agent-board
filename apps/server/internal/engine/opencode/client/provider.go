package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// ModelContextLimit returns OpenCode's runtime-resolved context window for the
// selected provider/model. Unknown models return nil without failing the Run.
func (c *Client) ModelContextLimit(ctx context.Context, providerID, modelID string) (*int64, error) {
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	if providerID == "" || modelID == "" {
		return nil, fmt.Errorf("opencode: provider id and model id are required")
	}
	var response struct {
		All []struct {
			ID     string `json:"id"`
			Models map[string]struct {
				Limit struct {
					Context int64 `json:"context"`
				} `json:"limit"`
			} `json:"models"`
		} `json:"all"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/provider", nil, &response); err != nil {
		return nil, err
	}
	for _, provider := range response.All {
		if provider.ID != providerID {
			continue
		}
		model, ok := provider.Models[modelID]
		if !ok || model.Limit.Context <= 0 {
			return nil, nil
		}
		limit := model.Limit.Context
		return &limit, nil
	}
	return nil, nil
}
