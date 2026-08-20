package console

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/secretref"
	"github.com/google/uuid"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"gopkg.in/yaml.v3"
)

// --- Canonical single-resource parsing ---

// replaceEnvVars substitutes ${VAR} patterns only when VAR exists in the environment.
// Unknown patterns (flow template vars, DAG references) are left unchanged.
func replaceEnvVars(s string) string {
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-1]
		if val, ok := os.LookupEnv(key); ok && val != "" {
			return val
		}
		return match // keep original if env var not set
	})
}

// ParseResourceImport parses the single canonical
// {consoleVersion, kind, metadata, data} resource envelope.
// replaceEnvVars substitutes ${VAR} patterns only when VAR exists in the environment.
// Unknown patterns (flow template vars, DAG references) are left unchanged.
func ParseResourceImport(raw []byte, _ string) (*ResourceImport, bool) {
	// Runtime credential resources persist environment references. Expanding
	// them here would copy plaintext secrets into the database and make safe API
	// reads impossible. Other resource kinds expand non-secret deploy-time values.
	var probe struct {
		Kind string `yaml:"kind" json:"kind"`
	}
	_ = yaml.Unmarshal(raw, &probe)
	if !strings.EqualFold(probe.Kind, "llmprovider") && !strings.EqualFold(probe.Kind, "mcp") {
		raw = []byte(replaceEnvVars(string(raw)))
	}
	var envelope struct {
		ConsoleVersion string            `yaml:"consoleVersion"`
		Kind           string            `yaml:"kind"`
		Metadata       *ResourceMetadata `yaml:"metadata"`
		Data           any               `yaml:"data"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&envelope); err != nil {
		return nil, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, false
	}
	if envelope.ConsoleVersion != ConsoleAPIVersion || envelope.Kind == "" ||
		envelope.Metadata == nil || envelope.Metadata.Name == "" || envelope.Data == nil {
		return nil, false
	}
	dataJSON, err := json.Marshal(envelope.Data)
	if err != nil {
		return nil, false
	}
	return &ResourceImport{
		ConsoleVersion: envelope.ConsoleVersion,
		Kind:           envelope.Kind,
		Metadata:       envelope.Metadata,
		Data:           dataJSON,
	}, true
}

// applyWrapperMeta applies metadata (namespace, labels, description, status) from
// the K8s wrapper to a resource that embeds BaseEntity and has a Labels field.
func applyWrapperMeta(ri *ResourceImport, base *entities.BaseEntity, labels *map[string]string, defaultNamespace string) {
	md := ri.Metadata
	if md == nil {
		base.Namespace = defaultNamespace
		return
	}
	if md.Namespace != "" {
		base.Namespace = md.Namespace
	} else {
		base.Namespace = defaultNamespace
	}
	if md.Description != "" {
		base.Description = md.Description
	}
	if md.Status != "" {
		base.Status = md.Status
	}
	if md.Labels != nil && labels != nil {
		*labels = md.Labels
	}
}

func activeRuntimeStatus(status string) string {
	if strings.EqualFold(status, "active") {
		return "ACTIVE"
	}
	return status
}

func (ri *ResourceImport) metadataName() string {
	if ri.Metadata != nil {
		return ri.Metadata.Name
	}
	return ""
}

func parseSpec(raw json.RawMessage, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("body must contain exactly one JSON value")
	}
	return nil
}

// ImportResource imports a single canonical resource into the database.
func (fc *FlowgentConsole) ImportResource(ri *ResourceImport, filePath string) bool {
	ls := fc.getStores()
	kind := strings.ToLower(ri.Kind)

	switch kind {
	case "agent":
		var a entities.AgentInfo
		if err := parseSpec(ri.Data, &a); err != nil {
			fmt.Printf("Error parsing Agent spec in %s: %v\n", filePath, err)
			return false
		}
		a.CreatedAt = time.Now()
		a.UpdatedAt = time.Now()
		if a.ID == "" {
			a.ID = uuid.New().String()
		}
		if a.Name == "" && ri.metadataName() != "" {
			a.Name = ri.metadataName()
		}
		applyWrapperMeta(ri, &a.BaseEntity, &a.Labels, fc.namespace)
		if err := ls.agents.Save(fc.ctx, &a); err != nil {
			fmt.Printf("Error saving Agent %s: %v\n", a.Name, err)
			return false
		}
		fmt.Printf("  OK Agent: %s\n", a.Name)

	case "mcp":
		var m entities.McpInfo
		if err := parseSpec(ri.Data, &m); err != nil {
			fmt.Printf("Error parsing MCP spec in %s: %v\n", filePath, err)
			return false
		}
		m.CreatedAt = time.Now()
		m.UpdatedAt = time.Now()
		if m.ID == "" {
			m.ID = uuid.New().String()
		}
		if m.Status == "active" {
			m.Enabled = true
		}
		if m.Name == "" && ri.metadataName() != "" {
			m.Name = ri.metadataName()
		}
		for key, value := range m.HeaderRefs {
			if secretref.IsSensitiveHeader(key) && !secretref.TemplateIsReference(value) {
				fmt.Printf("Error saving MCP %s: header %q must reference an injected environment secret\n", m.Name, key)
				return false
			}
			if m.Headers == nil {
				m.Headers = make(map[string]string)
			}
			m.Headers[key] = strings.TrimSpace(value)
		}
		for key, value := range m.EnvRefs {
			name, ok := secretref.EnvName(value)
			if !ok {
				fmt.Printf("Error saving MCP %s: environment value %q must be an injected secret reference\n", m.Name, key)
				return false
			}
			if m.Env == nil {
				m.Env = make(map[string]string)
			}
			m.Env[key] = "${" + name + "}"
		}
		m.HeaderRefs = nil
		m.EnvRefs = nil
		applyWrapperMeta(ri, &m.BaseEntity, &m.Labels, fc.namespace)
		// Align Enabled with Status so that MCPs imported with status=active are
		// immediately usable. TM skips MCPs with enabled=false (taskmanager.go:83).
		if m.Status == "active" {
			m.Enabled = true
		}
		if err := ls.mcps.Save(fc.ctx, &m); err != nil {
			fmt.Printf("Error saving MCP %s: %v\n", m.Name, err)
			return false
		}
		fmt.Printf("  OK MCP: %s\n", m.Name)

	case "llmprovider":
		var p entities.LlmProviderInfo
		if err := parseSpec(ri.Data, &p); err != nil {
			fmt.Printf("Error parsing LLMProvider spec in %s: %v\n", filePath, err)
			return false
		}
		p.ID = uuid.New().String()
		p.CreatedAt = time.Now()
		p.UpdatedAt = time.Now()
		applyWrapperMeta(ri, &p.BaseEntity, &p.Labels, fc.namespace)
		if p.Status == "" {
			p.Status = p.BaseEntity.Status
		}
		p.Status = activeRuntimeStatus(p.Status)
		if strings.TrimSpace(p.ApiKeyEnv) != "" {
			normalized, err := secretref.Normalize("${" + strings.TrimSpace(p.ApiKeyEnv) + "}")
			if err != nil {
				fmt.Printf("Error saving LLMProvider %s: api_key_env must name an injected environment variable\n", p.ID)
				return false
			}
			p.ApiKey = normalized
		}
		p.ApiKeyEnv = ""
		if err := ls.llm.Save(fc.ctx, &p); err != nil {
			fmt.Printf("Error saving LLMProvider %s: %v\n", p.ID, err)
			return false
		}
		fmt.Printf("  OK LLMProvider: %s\n", p.ID)

	case "flow":
		var spec entities.FlowInfo
		if err := parseSpec(ri.Data, &spec); err != nil {
			fmt.Printf("Error parsing Flow spec in %s: %v\n", filePath, err)
			return false
		}
		if spec.ID == "" && ri.metadataName() != "" {
			spec.ID = ri.metadataName()
		}
		if spec.Kind == "" {
			spec.Kind = "flow"
		}
		applyWrapperMeta(ri, &spec.BaseEntity, &spec.Labels, fc.namespace)
		if err := spec.ValidateDAG(); err != nil {
			fmt.Printf("Error validating Flow %s: %v\n", spec.ID, err)
			return false
		}
		if err := ls.flows.SaveSpec(fc.ctx, &spec, "import", "imported from "+filePath); err != nil {
			fmt.Printf("Error saving Flow %s: %v\n", spec.ID, err)
			return false
		}
		fmt.Printf("  OK Flow: %s\n", spec.ID)

	case "flowrun":
		var run entities.FlowRunInfo
		if err := parseSpec(ri.Data, &run); err != nil {
			fmt.Printf("Error parsing FlowRun spec in %s: %v\n", filePath, err)
			return false
		}
		if run.ID == "" {
			run.ID = uuid.New().String()
		}
		run.CreatedAt = time.Now()
		run.UpdatedAt = time.Now()
		applyWrapperMeta(ri, &run.BaseEntity, &run.Labels, fc.namespace)
		if err := ls.runs.Create(fc.ctx, &run); err != nil {
			fmt.Printf("Error saving FlowRun %s: %v\n", run.ID, err)
			return false
		}
		fmt.Printf("  OK FlowRun: %s\n", run.ID)

	case "skill":
		var spec entities.FlowInfo
		if err := parseSpec(ri.Data, &spec); err != nil {
			fmt.Printf("Error parsing Skill spec in %s: %v\n", filePath, err)
			return false
		}
		if spec.ID == "" && ri.metadataName() != "" {
			spec.ID = ri.metadataName()
		}
		spec.Kind = "skill"
		applyWrapperMeta(ri, &spec.BaseEntity, &spec.Labels, fc.namespace)
		if err := spec.ValidateDAG(); err != nil {
			fmt.Printf("Error validating Skill %s: %v\n", spec.ID, err)
			return false
		}
		if err := ls.flows.SaveSpec(fc.ctx, &spec, "import", "imported from "+filePath); err != nil {
			fmt.Printf("Error saving Skill %s: %v\n", spec.ID, err)
			return false
		}
		fmt.Printf("  OK Skill: %s\n", spec.ID)

	case "notifychannel":
		var ch entities.NotifyChannelInfo
		if err := parseSpec(ri.Data, &ch); err != nil {
			fmt.Printf("Error parsing NotifyChannel spec in %s: %v\n", filePath, err)
			return false
		}
		ch.ID = uuid.New().String()
		ch.CreatedAt = time.Now()
		ch.UpdatedAt = time.Now()
		applyWrapperMeta(ri, &ch.BaseEntity, &ch.Labels, fc.namespace)
		if err := ls.channels.Save(fc.ctx, &ch); err != nil {
			fmt.Printf("Error saving NotifyChannel %s: %v\n", ch.Name, err)
			return false
		}
		fmt.Printf("  OK NotifyChannel: %s\n", ch.Name)

	default:
		fmt.Printf("Unknown kind %q in %s — expected Agent|MCP|LLMProvider|Flow|FlowRun|NotifyChannel|Skill\n", ri.Kind, filePath)
		return false
	}
	return true
}

// --- Bulk import ---

// ImportAll imports all resources from an ExportData structure into the
// database. Returns counts: [llms, channels, mcps, skills, agents, flows, runs].
func (fc *FlowgentConsole) ImportAll(data *ExportData) ([]int, error) {
	ls := fc.getStores()
	counts := make([]int, 7)

	for i := range data.LLMs {
		llm := &data.LLMs[i]
		llm.Namespace = fc.namespace
		llm.CreatedAt = time.Now()
		llm.UpdatedAt = time.Now()
		if llm.ID == "" {
			llm.ID = uuid.New().String()
		}
		if err := ls.llm.Save(fc.ctx, llm); err != nil {
			return counts, fmt.Errorf("llm %s: %w", llm.ID, err)
		}
		counts[0]++
	}
	for i := range data.Channels {
		ch := &data.Channels[i]
		ch.Namespace = fc.namespace
		ch.CreatedAt = time.Now()
		ch.UpdatedAt = time.Now()
		if ch.ID == "" {
			ch.ID = uuid.New().String()
		}
		if err := ls.channels.Save(fc.ctx, ch); err != nil {
			return counts, fmt.Errorf("channel %s: %w", ch.ID, err)
		}
		counts[1]++
	}
	for i := range data.MCPs {
		m := &data.MCPs[i]
		m.Namespace = fc.namespace
		m.CreatedAt = time.Now()
		m.UpdatedAt = time.Now()
		if m.ID == "" {
			m.ID = uuid.New().String()
		}
		if m.Status == "active" {
			m.Enabled = true
		}
		if err := ls.mcps.Save(fc.ctx, m); err != nil {
			return counts, fmt.Errorf("mcp %s: %w", m.Name, err)
		}
		counts[2]++
	}
	for _, spec := range data.Skills {
		sp := spec
		sp.Kind = "skill"
		if sp.ID == "" {
			sp.ID = uuid.New().String()
		}
		sp.Namespace = fc.namespace
		if err := ls.flows.SaveSpec(fc.ctx, &sp, "import", "imported from file"); err != nil {
			return counts, fmt.Errorf("skill %s: %w", sp.ID, err)
		}
		counts[3]++
	}
	for i := range data.AgentDefs {
		a := &data.AgentDefs[i]
		a.Namespace = fc.namespace
		a.CreatedAt = time.Now()
		a.UpdatedAt = time.Now()
		if a.ID == "" {
			a.ID = uuid.New().String()
		}
		if err := ls.agents.Save(fc.ctx, a); err != nil {
			return counts, fmt.Errorf("agent %s: %w", a.Name, err)
		}
		counts[4]++
	}
	for _, spec := range data.AgentFlows {
		sp := spec
		if sp.ID == "" {
			sp.ID = uuid.New().String()
		}
		sp.Namespace = fc.namespace
		if sp.Kind == "" {
			sp.Kind = "flow"
		}
		if err := ls.flows.SaveSpec(fc.ctx, &sp, "import", "imported from file"); err != nil {
			return counts, fmt.Errorf("flow %s: %w", sp.ID, err)
		}
		counts[5]++
	}
	for i := range data.FlowRuns {
		run := &data.FlowRuns[i]
		run.Namespace = fc.namespace
		run.CreatedAt = time.Now()
		run.UpdatedAt = time.Now()
		if run.ID == "" {
			run.ID = uuid.New().String()
		}
		if err := ls.runs.Create(fc.ctx, run); err != nil {
			return counts, fmt.Errorf("run %s: %w", run.ID, err)
		}
		counts[6]++
	}
	return counts, nil
}

// ImportFile imports one canonical resource envelope or one bulk export file.
func (fc *FlowgentConsole) ImportFile(filePath string, extraArgs []string) bool {
	fm, err := DetectFormat(filePath, extraArgs)
	if err != nil {
		fmt.Printf("Error detecting format for %s: %v\n", filePath, err)
		return false
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Error reading %s: %v\n", filePath, err)
		return false
	}

	ri, isK8s := ParseResourceImport(raw, fm)
	if isK8s {
		return fc.ImportResource(ri, filePath)
	}

	var data ExportData
	switch fm {
	case "json":
		err = json.Unmarshal(raw, &data)
	case "yaml":
		err = yaml.Unmarshal(raw, &data)
	}
	if err != nil {
		fmt.Printf("Error parsing %s: %v\n", fm, err)
		return false
	}
	counts, err := fc.ImportAll(&data)
	if err != nil {
		fmt.Printf("Error importing: %v\n", err)
		return false
	}
	fmt.Printf("Imported from %s (%d llms, %d channels, %d mcps, %d skills, %d agents, %d flows, %d runs)\n",
		filePath, counts[0], counts[1], counts[2], counts[3], counts[4], counts[5], counts[6])
	return true
}

// ImportPaths imports files/directories from the given patterns (supports glob).
// Returns the number of successfully imported files.
func (fc *FlowgentConsole) ImportPaths(patterns []string, extraArgs []string) int {
	seen := make(map[string]bool)
	var files []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			info, statErr := os.Stat(pattern)
			if statErr != nil {
				fmt.Printf("Skipping %q: %v\n", pattern, statErr)
				continue
			}
			if info.IsDir() {
				CollectDirFiles(pattern, &files, &seen)
				continue
			}
			if !seen[pattern] {
				seen[pattern] = true
				files = append(files, pattern)
			}
			continue
		}
		for _, m := range matches {
			info, statErr := os.Stat(m)
			if statErr != nil {
				continue
			}
			if info.IsDir() {
				CollectDirFiles(m, &files, &seen)
			} else if !seen[m] {
				seen[m] = true
				files = append(files, m)
			}
		}
	}

	if len(files) == 0 {
		fmt.Println("No files matched.")
		return 0
	}

	var imported int
	for _, f := range files {
		if ok := fc.ImportFile(f, extraArgs); ok {
			imported++
		}
	}
	return imported
}
