package console

import (
	"strings"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ExportAll collects all Flowgent-owned resources from the database.
func (fc *FlowgentConsole) ExportAll() (*ExportData, error) {
	return fc.ExportKinds(nil)
}

// ExportKinds exports only the specified resource kinds. An empty or nil
// kinds slice exports all supported kinds.
// Valid kinds: llm, channel, mcp, skill, agent, flow, flowrun.
func (fc *FlowgentConsole) ExportKinds(kinds []string) (*ExportData, error) {
	ls := fc.getStores()
	data := &ExportData{}

	all := len(kinds) == 0
	kindSet := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		kindSet[strings.ToLower(strings.TrimSpace(k))] = true
	}
	include := func(k string) bool { return all || kindSet[k] }

	if include("llm") {
		if page, err := ls.llm.Select(fc.ctx, entities.PageRequest{Page: 1, Size: 10000}); err == nil {
			for _, p := range page.Items {
				if p != nil {
					data.LLMs = append(data.LLMs, *p)
				}
			}
		}
	}
	if include("channel") {
		if page, err := ls.channels.Select(fc.ctx, entities.PageRequest{Page: 1, Size: 10000}); err == nil {
			for _, ch := range page.Items {
				if ch != nil {
					data.Channels = append(data.Channels, *ch)
				}
			}
		}
	}
	if include("mcp") {
		if page, err := ls.mcps.Select(fc.ctx, entities.PageRequest{Page: 1, Size: 10000}); err == nil {
			for _, m := range page.Items {
				if m != nil {
					data.MCPs = append(data.MCPs, *m)
				}
			}
		}
	}
	if include("agent") {
		if page, err := ls.agents.Select(fc.ctx, entities.PageRequest{Page: 1, Size: 10000}); err == nil {
			for _, a := range page.Items {
				if a != nil {
					data.AgentDefs = append(data.AgentDefs, *a)
				}
			}
		}
	}
	if include("flow") || include("skill") {
		if page, err := ls.flows.Select(fc.ctx, fc.namespace, entities.PageRequest{Page: 1, Size: 10000}); err == nil {
			for _, fv := range page.Items {
				if fv == nil {
					continue
				}
				spec, _ := ls.flows.GetSpec(fc.ctx, fc.namespace, fv.FlowID)
				if spec == nil {
					continue
				}
				if spec.Kind == "skill" {
					if include("skill") {
						data.Skills = append(data.Skills, *spec)
					}
				} else {
					if include("flow") {
						data.AgentFlows = append(data.AgentFlows, *spec)
					}
				}
			}
		}
	}
	if include("flowrun") {
		if page, err := ls.runs.Select(fc.ctx, entities.PageRequest{Page: 1, Size: 10000}); err == nil {
			for _, r := range page.Items {
				if r != nil {
					data.FlowRuns = append(data.FlowRuns, *r)
				}
			}
		}
	}
	return data, nil
}
