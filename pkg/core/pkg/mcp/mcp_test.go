package mcp

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	protocol "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestCallToolRejectsMCPApplicationErrors(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "success"
		if failure {
			name = "application-error"
		}
		t.Run(name, func(t *testing.T) {
			backend := server.NewMCPServer("fixture", "test")
			backend.AddTool(protocol.NewTool("exercise"), func(context.Context, protocol.CallToolRequest) (*protocol.CallToolResult, error) {
				if failure {
					return protocol.NewToolResultError("operation rejected"), nil
				}
				return protocol.NewToolResultText(`{"ok":true}`), nil
			})
			httpServer := httptest.NewServer(server.NewStreamableHTTPServer(backend))
			t.Cleanup(httpServer.Close)
			manager := NewMcpManager(nil)
			manager.Register("fixture", httpServer.URL, nil)
			t.Cleanup(func() {
				for _, client := range manager.clients {
					_ = client.Close()
				}
			})
			output, err := manager.CallTool(t.Context(), "fixture", "exercise", nil)
			if failure {
				if err == nil || !strings.Contains(err.Error(), "execution error") || output != nil {
					t.Fatalf("MCP failure was accepted: output=%v err=%v", output, err)
				}
			} else if err != nil || output["text"] != `{"ok":true}` {
				t.Fatalf("successful MCP result changed: output=%v err=%v", output, err)
			}
		})
	}
}
