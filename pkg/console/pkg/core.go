package console

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg/graphrun"
)

// ─── LLM Provider ────────────────────────────────────────────────

// ListLLMs returns all LLM providers.
func (fc *FlowgentConsole) ListLLMs() ([]entities.LlmProviderInfo, error) {
	ls := fc.getStores()
	page, err := ls.llm.List(fc.ctx, fc.namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		return nil, err
	}
	var result []entities.LlmProviderInfo
	for _, p := range page.Items {
		if p != nil {
			result = append(result, *p)
		}
	}
	return result, nil
}

// GetLLM returns a single LLM provider by ID.
func (fc *FlowgentConsole) GetLLM(id string) (*entities.LlmProviderInfo, error) {
	return fc.getStores().llm.Get(fc.ctx, fc.namespace, id)
}

// AddLLM saves a new LLM provider. It assigns an ID, namespace, and timestamps.
func (fc *FlowgentConsole) AddLLM(p *entities.LlmProviderInfo) error {
	p.ID = uuid.New().String()
	p.Namespace = fc.namespace
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	return fc.getStores().llm.Save(fc.ctx, p)
}

// RemoveLLM deletes an LLM provider by ID.
func (fc *FlowgentConsole) RemoveLLM(id string) error {
	return fc.getStores().llm.Delete(fc.ctx, fc.namespace, id)
}

// ─── Channel ─────────────────────────────────────────────────────

// ListChannels returns all notification channels.
func (fc *FlowgentConsole) ListChannels() ([]entities.NotifyChannelInfo, error) {
	ls := fc.getStores()
	page, err := ls.channels.List(fc.ctx, fc.namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		return nil, err
	}
	var result []entities.NotifyChannelInfo
	for _, ch := range page.Items {
		if ch != nil {
			result = append(result, *ch)
		}
	}
	return result, nil
}

// GetChannel returns a single channel by ID.
func (fc *FlowgentConsole) GetChannel(id string) (*entities.NotifyChannelInfo, error) {
	return fc.getStores().channels.Get(fc.ctx, fc.namespace, id)
}

// AddChannel saves a new notification channel.
func (fc *FlowgentConsole) AddChannel(ch *entities.NotifyChannelInfo) error {
	ch.ID = uuid.New().String()
	ch.Namespace = fc.namespace
	ch.CreatedAt = time.Now()
	ch.UpdatedAt = time.Now()
	if err := fc.protectChannelSecrets(ch); err != nil {
		return err
	}
	return fc.getStores().channels.Save(fc.ctx, ch)
}

// RemoveChannel deletes a channel by ID.
func (fc *FlowgentConsole) RemoveChannel(id string) error {
	return fc.getStores().channels.Delete(fc.ctx, fc.namespace, id)
}

// ─── Agent ───────────────────────────────────────────────────────

// ListAgents returns all agents.
func (fc *FlowgentConsole) ListAgents() ([]entities.AgentInfo, error) {
	ls := fc.getStores()
	page, err := ls.agents.List(fc.ctx, fc.namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		return nil, err
	}
	var result []entities.AgentInfo
	for _, a := range page.Items {
		if a != nil {
			result = append(result, *a)
		}
	}
	return result, nil
}

// GetAgent returns a single agent by name.
func (fc *FlowgentConsole) GetAgent(name string) (*entities.AgentInfo, error) {
	return fc.getStores().agents.Get(fc.ctx, fc.namespace, name)
}

// AddAgent saves a new agent.
func (fc *FlowgentConsole) AddAgent(a *entities.AgentInfo) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	a.Namespace = fc.namespace
	a.CreatedAt = time.Now()
	a.UpdatedAt = time.Now()
	return fc.getStores().agents.Save(fc.ctx, a)
}

// RemoveAgent deletes an agent by name.
func (fc *FlowgentConsole) RemoveAgent(name string) error {
	return fc.getStores().agents.Delete(fc.ctx, fc.namespace, name)
}

// ─── MCP ─────────────────────────────────────────────────────────

// ListMCPs returns all MCPs.
func (fc *FlowgentConsole) ListMCPs() ([]entities.McpInfo, error) {
	ls := fc.getStores()
	page, err := ls.mcps.List(fc.ctx, fc.namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		return nil, err
	}
	var result []entities.McpInfo
	for _, m := range page.Items {
		if m != nil {
			result = append(result, *m)
		}
	}
	return result, nil
}

// GetMCP returns a single MCP by name.
func (fc *FlowgentConsole) GetMCP(name string) (*entities.McpInfo, error) {
	return fc.getStores().mcps.Get(fc.ctx, fc.namespace, name)
}

// AddMCP saves a new MCP.
func (fc *FlowgentConsole) AddMCP(m *entities.McpInfo) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	m.Namespace = fc.namespace
	m.CreatedAt = time.Now()
	m.UpdatedAt = time.Now()
	return fc.getStores().mcps.Save(fc.ctx, m)
}

// RemoveMCP deletes an MCP by name.
func (fc *FlowgentConsole) RemoveMCP(name string) error {
	return fc.getStores().mcps.Delete(fc.ctx, fc.namespace, name)
}

// ─── AgentGraph ────────────────────────────────────────────────────────

// ListAgentGraphs returns all graph version entries.
func (fc *FlowgentConsole) ListAgentGraphs() ([]entities.AgentGraphRevision, error) {
	ls := fc.getStores()
	page, err := ls.graphs.Select(fc.ctx, fc.namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		return nil, err
	}
	var result []entities.AgentGraphRevision
	for _, fv := range page.Items {
		if fv != nil {
			result = append(result, *fv)
		}
	}
	return result, nil
}

// GetAgentGraph returns a full graph spec by ID.
func (fc *FlowgentConsole) GetAgentGraph(id string) (*entities.AgentGraph, error) {
	return fc.getStores().graphs.GetSpec(fc.ctx, fc.namespace, id)
}

// AddAgentGraph saves a new graph spec. It generates an ID if empty and defaults Kind to "graph".
func (fc *FlowgentConsole) AddAgentGraph(spec *entities.AgentGraph) error {
	if spec.ID == "" {
		spec.ID = uuid.New().String()
	}
	spec.Namespace = fc.namespace
	if spec.Kind == "" {
		spec.Kind = "graph"
	}
	return fc.getStores().graphs.SaveSpec(fc.ctx, spec, "console", "added via console")
}

// RemoveAgentGraph deletes a graph by ID.
func (fc *FlowgentConsole) RemoveAgentGraph(id string) error {
	return fc.getStores().graphs.Delete(fc.ctx, fc.namespace, id)
}

// ─── Skill ───────────────────────────────────────────────────────

// ListSkills returns all skill version entries.
func (fc *FlowgentConsole) ListSkills() ([]entities.AgentGraphRevision, error) {
	ls := fc.getStores()
	page, err := ls.graphs.Select(fc.ctx, fc.namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		return nil, err
	}
	var result []entities.AgentGraphRevision
	for _, fv := range page.Items {
		if fv == nil {
			continue
		}
		spec, _ := ls.graphs.GetSpec(fc.ctx, fc.namespace, fv.AgentGraphID)
		if spec != nil && spec.Kind == "skill" {
			result = append(result, *fv)
		}
	}
	return result, nil
}

// GetSkill returns a single skill by ID.
func (fc *FlowgentConsole) GetSkill(id string) (*entities.AgentGraph, error) {
	spec, err := fc.getStores().graphs.GetSpec(fc.ctx, fc.namespace, id)
	if err != nil || spec == nil {
		return nil, fmt.Errorf("skill not found: %s", id)
	}
	return spec, nil
}

// AddSkill saves a new skill.
func (fc *FlowgentConsole) AddSkill(spec *entities.AgentGraph) error {
	spec.Kind = "skill"
	if spec.ID == "" {
		spec.ID = uuid.New().String()
	}
	spec.Namespace = fc.namespace
	return fc.getStores().graphs.SaveSpec(fc.ctx, spec, "console", "added via console")
}

// RemoveSkill deletes a skill by ID.
func (fc *FlowgentConsole) RemoveSkill(id string) error {
	return fc.getStores().graphs.Delete(fc.ctx, fc.namespace, id)
}

// ─── Run ─────────────────────────────────────────────────────────

// ListGraphRuns returns all graph runs.
func (fc *FlowgentConsole) ListGraphRuns() ([]entities.GraphRun, error) {
	ls := fc.getStores()
	page, err := ls.runs.List(fc.ctx, graphrun.ListFilter{
		Namespace: fc.namespace,
		Page:      entities.PageRequest{Page: 1, Size: 50},
	})
	if err != nil {
		return nil, err
	}
	var result []entities.GraphRun
	for _, r := range page.Items {
		if r != nil {
			result = append(result, *r)
		}
	}
	return result, nil
}

// GetGraphRun returns a single run by ID.
func (fc *FlowgentConsole) GetGraphRun(id string) (*entities.GraphRun, error) {
	return fc.getStores().runs.Get(fc.ctx, id)
}

// CreateGraphRun creates a new pending run for the given graph ID.
func (fc *FlowgentConsole) CreateGraphRun(graphID string) (*entities.GraphRun, error) {
	run := &entities.GraphRun{
		BaseEntity:     entities.BaseEntity{ID: uuid.New().String(), Namespace: fc.namespace, CreatedAt: time.Now()},
		AgentGraphName: graphID,
		Status:         entities.GraphRunPending,
	}
	if err := fc.getStores().runs.Create(fc.ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

// StartRun transitions a run to Running status.
func (fc *FlowgentConsole) StartRun(runID string) (*entities.GraphRun, error) {
	run, err := fc.getStores().runs.Get(fc.ctx, runID)
	if err != nil || run == nil {
		return nil, fmt.Errorf("run not found: %s", runID)
	}
	run.Status = entities.GraphRunRunning
	now := time.Now()
	run.StartedAt = &now
	if err := fc.getStores().runs.Update(fc.ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

// StopRun cancels a run by ID.
func (fc *FlowgentConsole) StopRun(runID string) error {
	return fc.getStores().runs.Cancel(fc.ctx, runID)
}
