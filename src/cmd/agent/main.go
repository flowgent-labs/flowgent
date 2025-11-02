package main

import (
	"context"
	"fmt"

	"github.com/cyberbot/cve-auto-fix/src/internal/adk/workflow/engine"
	"github.com/cyberbot/cve-auto-fix/src/internal/adk/workflow/loader"
)

func main() {
	ctx := context.Background()

	cfgLoader := loader.NewYAML()
	cfg, err := cfgLoader.Load("src/configs/workflow.yaml")
	if err != nil {
		fmt.Println("Failed to load config:", err)
		return
	}

	runner := engine.NewEngine(cfg)

	result, err := runner.Run(ctx, "security_fix", map[string]any{
		"cve": "CVE-2023-0001",
	})
	if err != nil {
		fmt.Println("Workflow failed:", err)
		return
	}

	fmt.Println("Workflow finished. Final result:", result)
}