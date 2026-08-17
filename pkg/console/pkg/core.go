package console

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ─── LLM Provider ────────────────────────────────────────────────

// ListLLMs returns all LLM providers.
func (fc *FlowgentConsole) ListLLMs() ([]entities.LlmProviderInfo, error) {
	ls := fc.getStores()
	page, err := ls.llm.Select(fc.ctx, entities.PageRequest{Page: 1, Size: 1000})
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
	return fc.getStores().llm.Get(fc.ctx, id)
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
	return fc.getStores().llm.Delete(fc.ctx, id)
}

// ─── Channel ─────────────────────────────────────────────────────

// ListChannels returns all notification channels.
func (fc *FlowgentConsole) ListChannels() ([]entities.NotifyChannelInfo, error) {
	ls := fc.getStores()
	page, err := ls.channels.Select(fc.ctx, entities.PageRequest{Page: 1, Size: 1000})
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
	return fc.getStores().channels.Get(fc.ctx, id)
}

// AddChannel saves a new notification channel.
func (fc *FlowgentConsole) AddChannel(ch *entities.NotifyChannelInfo) error {
	ch.ID = uuid.New().String()
	ch.Namespace = fc.namespace
	ch.CreatedAt = time.Now()
	ch.UpdatedAt = time.Now()
	return fc.getStores().channels.Save(fc.ctx, ch)
}

// RemoveChannel deletes a channel by ID.
func (fc *FlowgentConsole) RemoveChannel(id string) error {
	return fc.getStores().channels.Delete(fc.ctx, id)
}

// ─── Agent ───────────────────────────────────────────────────────

// ListAgents returns all agents.
func (fc *FlowgentConsole) ListAgents() ([]entities.AgentInfo, error) {
	ls := fc.getStores()
	page, err := ls.agents.Select(fc.ctx, entities.PageRequest{Page: 1, Size: 1000})
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
	return fc.getStores().agents.Get(fc.ctx, name)
}

// AddAgent saves a new agent.
func (fc *FlowgentConsole) AddAgent(a *entities.AgentInfo) error {
	a.Namespace = fc.namespace
	a.CreatedAt = time.Now()
	a.UpdatedAt = time.Now()
	return fc.getStores().agents.Save(fc.ctx, a)
}

// RemoveAgent deletes an agent by name.
func (fc *FlowgentConsole) RemoveAgent(name string) error {
	return fc.getStores().agents.Delete(fc.ctx, name)
}

// ─── MCP ─────────────────────────────────────────────────────────

// ListMCPs returns all MCPs.
func (fc *FlowgentConsole) ListMCPs() ([]entities.McpInfo, error) {
	ls := fc.getStores()
	page, err := ls.mcps.Select(fc.ctx, entities.PageRequest{Page: 1, Size: 1000})
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
	return fc.getStores().mcps.Get(fc.ctx, name)
}

// AddMCP saves a new MCP.
func (fc *FlowgentConsole) AddMCP(m *entities.McpInfo) error {
	m.Namespace = fc.namespace
	m.CreatedAt = time.Now()
	m.UpdatedAt = time.Now()
	return fc.getStores().mcps.Save(fc.ctx, m)
}

// RemoveMCP deletes an MCP by name.
func (fc *FlowgentConsole) RemoveMCP(name string) error {
	return fc.getStores().mcps.Delete(fc.ctx, name)
}

// ─── Flow ────────────────────────────────────────────────────────

// ListFlows returns all flow version entries.
func (fc *FlowgentConsole) ListFlows() ([]entities.FlowVersionInfo, error) {
	ls := fc.getStores()
	page, err := ls.flows.Select(fc.ctx, fc.namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		return nil, err
	}
	var result []entities.FlowVersionInfo
	for _, fv := range page.Items {
		if fv != nil {
			result = append(result, *fv)
		}
	}
	return result, nil
}

// GetFlow returns a full flow spec by ID.
func (fc *FlowgentConsole) GetFlow(id string) (*entities.FlowInfo, error) {
	return fc.getStores().flows.GetSpec(fc.ctx, fc.namespace, id)
}

// AddFlow saves a new flow spec. It generates an ID if empty and defaults Kind to "flow".
func (fc *FlowgentConsole) AddFlow(spec *entities.FlowInfo) error {
	if spec.ID == "" {
		spec.ID = uuid.New().String()
	}
	spec.Namespace = fc.namespace
	if spec.Kind == "" {
		spec.Kind = "flow"
	}
	return fc.getStores().flows.SaveSpec(fc.ctx, spec, "console", "added via console")
}

// RemoveFlow deletes a flow by ID.
func (fc *FlowgentConsole) RemoveFlow(id string) error {
	return fc.getStores().flows.Delete(fc.ctx, fc.namespace, id)
}

// ─── Skill ───────────────────────────────────────────────────────

// ListSkills returns all skill version entries.
func (fc *FlowgentConsole) ListSkills() ([]entities.FlowVersionInfo, error) {
	ls := fc.getStores()
	page, err := ls.flows.Select(fc.ctx, fc.namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		return nil, err
	}
	var result []entities.FlowVersionInfo
	for _, fv := range page.Items {
		if fv == nil {
			continue
		}
		spec, _ := ls.flows.GetSpec(fc.ctx, fc.namespace, fv.FlowID)
		if spec != nil && spec.Kind == "skill" {
			result = append(result, *fv)
		}
	}
	return result, nil
}

// GetSkill returns a single skill by ID.
func (fc *FlowgentConsole) GetSkill(id string) (*entities.FlowInfo, error) {
	spec, err := fc.getStores().flows.GetSpec(fc.ctx, fc.namespace, id)
	if err != nil || spec == nil {
		return nil, fmt.Errorf("skill not found: %s", id)
	}
	return spec, nil
}

// AddSkill saves a new skill.
func (fc *FlowgentConsole) AddSkill(spec *entities.FlowInfo) error {
	spec.Kind = "skill"
	if spec.ID == "" {
		spec.ID = uuid.New().String()
	}
	spec.Namespace = fc.namespace
	return fc.getStores().flows.SaveSpec(fc.ctx, spec, "console", "added via console")
}

// RemoveSkill deletes a skill by ID.
func (fc *FlowgentConsole) RemoveSkill(id string) error {
	return fc.getStores().flows.Delete(fc.ctx, fc.namespace, id)
}

// ─── Run ─────────────────────────────────────────────────────────

// ListRuns returns all flow runs.
func (fc *FlowgentConsole) ListRuns() ([]entities.FlowRunInfo, error) {
	ls := fc.getStores()
	page, err := ls.runs.Select(fc.ctx, entities.PageRequest{Page: 1, Size: 50})
	if err != nil {
		return nil, err
	}
	var result []entities.FlowRunInfo
	for _, r := range page.Items {
		if r != nil {
			result = append(result, *r)
		}
	}
	return result, nil
}

// GetRun returns a single run by ID.
func (fc *FlowgentConsole) GetRun(id string) (*entities.FlowRunInfo, error) {
	return fc.getStores().runs.Get(fc.ctx, id)
}

// CreateRun creates a new pending run for the given flow ID.
func (fc *FlowgentConsole) CreateRun(flowID string) (*entities.FlowRunInfo, error) {
	run := &entities.FlowRunInfo{
		BaseEntity:  entities.BaseEntity{ID: uuid.New().String(), Namespace: fc.namespace, CreatedAt: time.Now()},
		AgentFlowID: flowID,
		Status:      entities.RunPending,
	}
	if err := fc.getStores().runs.Create(fc.ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

// StartRun transitions a run to Running status.
func (fc *FlowgentConsole) StartRun(runID string) (*entities.FlowRunInfo, error) {
	run, err := fc.getStores().runs.Get(fc.ctx, runID)
	if err != nil || run == nil {
		return nil, fmt.Errorf("run not found: %s", runID)
	}
	run.Status = entities.RunRunning
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
