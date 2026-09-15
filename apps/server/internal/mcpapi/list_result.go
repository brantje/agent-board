package mcpapi

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListResult[T any] struct {
	Items []T `json:"items"`
}

func objectListHandler[In, Out any](handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, []Out, error)) func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, ListResult[Out], error) {
	return func(ctx context.Context, request *mcp.CallToolRequest, input In) (*mcp.CallToolResult, ListResult[Out], error) {
		result, items, err := handler(ctx, request, input)
		if err != nil {
			return result, ListResult[Out]{}, err
		}
		return result, ListResult[Out]{Items: items}, nil
	}
}
