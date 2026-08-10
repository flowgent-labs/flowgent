package console

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"gopkg.in/yaml.v3"
)

// --- K8s-style single-resource parsing ---

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

// ParseResourceImport detects and parses a K8s-style {kind, metadata, spec}
// resource. Also accepts flat-style {kind, name, namespace, ..., spec} as fallback.
// replaceEnvVars substitutes ${VAR} patterns only when VAR exists in the environment.
// Unknown patterns (flow template vars, DAG references) are left unchanged.
func ParseResourceImport(raw []byte, fm string) (*ResourceImport, bool) {
	// Resolve env vars in raw YAML before parsing
	raw = []byte(replaceEnvVars(string(raw)))
	if fm == "yaml" {
		var generic map[string]any
		if err := yaml.Unmarshal(raw, &generic); err != nil {
			return nil, false
		}
		kind, _ := generic["kind"].(string)
		if kind == "" {
			return nil, false
		}
		ri := &ResourceImport{
			APIVersion: toString(generic["apiVersion"]),
			Kind:       kind,
		}

		if meta, ok := generic["metadata"]; ok {
			metaJSON, _ := json.Marshal(meta)
			var md ResourceMetadata
			if err := json.Unmarshal(metaJSON, &md); err == nil {
				ri.Metadata = &md
			}
		} else {
			md := &ResourceMetadata{
				Name:        toString(generic["name"]),
				Namespace:   toString(generic["namespace"]),
				Status:      toString(generic["status"]),
				Description: toString(generic["description"]),
			}
			if labels, ok := generic["labels"]; ok {
				md.Labels = toStringMap(labels)
			}
			if md.Name != "" || md.Namespace != "" || md.Status != "" || md.Description != "" || len(md.Labels) > 0 {
				ri.Metadata = md
			}
		}

		if spec, ok := generic["spec"]; ok {
			specJSON, err := json.Marshal(spec)
			if err != nil {
				return nil, false
			}
			ri.Spec = specJSON
		} else if data, ok := generic["data"]; ok {
			specJSON, err := json.Marshal(data)
			if err != nil {
				return nil, false
			}
			ri.Spec = specJSON
		}
		// accept consoleVersion as an alias for apiVersion
		if ri.APIVersion == "" {
			ri.APIVersion = toString(generic["consoleVersion"])
		}
		return ri, true
	}

	var ri ResourceImport
	if err := json.Unmarshal(raw, &ri); err != nil {
		return nil, false
	}
	if ri.Kind == "" {
		return nil, false
	}
	return &ri, true
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
	return json.Unmarshal(raw, v)
}

// ImportResource imports a single K8s-style resource into the database.
func (fc *FlowgentConsole) ImportResource(ri *ResourceImport, filePath string) bool {
	ls := fc.getStores()
	kind := strings.ToLower(ri.Kind)

	switch kind {
	case "agent":
		var a entities.AgentInfo
		if err := parseSpec(ri.Spec, &a); err != nil {
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
		if err := parseSpec(ri.Spec, &m); err != nil {
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
		if err := parseSpec(ri.Spec, &p); err != nil {
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
		if err := ls.llm.Save(fc.ctx, &p); err != nil {
			fmt.Printf("Error saving LLMProvider %s: %v\n", p.ID, err)
			return false
		}
		fmt.Printf("  OK LLMProvider: %s\n", p.ID)

	case "flow":
		var spec entities.FlowInfo
		if err := parseSpec(ri.Spec, &spec); err != nil {
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
		if err := ls.flows.SaveSpec(fc.ctx, &spec, "import", "imported from "+filePath); err != nil {
			fmt.Printf("Error saving Flow %s: %v\n", spec.ID, err)
			return false
		}
		fmt.Printf("  OK Flow: %s\n", spec.ID)

	case "flowrun":
		var run entities.FlowRunInfo
		if err := parseSpec(ri.Spec, &run); err != nil {
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
		if err := parseSpec(ri.Spec, &spec); err != nil {
			fmt.Printf("Error parsing Skill spec in %s: %v\n", filePath, err)
			return false
		}
		if spec.ID == "" && ri.metadataName() != "" {
			spec.ID = ri.metadataName()
		}
		spec.Kind = "skill"
		applyWrapperMeta(ri, &spec.BaseEntity, &spec.Labels, fc.namespace)
		if err := ls.flows.SaveSpec(fc.ctx, &spec, "import", "imported from "+filePath); err != nil {
			fmt.Printf("Error saving Skill %s: %v\n", spec.ID, err)
			return false
		}
		fmt.Printf("  OK Skill: %s\n", spec.ID)

	case "notifychannel":
		var ch entities.NotifyChannelInfo
		if err := parseSpec(ri.Spec, &ch); err != nil {
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
// database. Returns counts: [llms, channels, mcps, skills, agents, flows, runs, wallets].
func (fc *FlowgentConsole) ImportAll(data *ExportData) ([]int, error) {
	ls := fc.getStores()
	counts := make([]int, 8)

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
	if len(data.Wallets) > 0 && fc.secretStore == nil {
		fmt.Println("Warning: secret store not available, skipping wallet import.")
	}
	for _, w := range data.Wallets {
		if fc.secretStore == nil {
			break
		}
		keyBytes, err := hex.DecodeString(w.PrivateKey)
		if err != nil {
			return counts, fmt.Errorf("wallet %s: invalid hex key: %w", w.Name, err)
		}
		if err := fc.secretStore.PutSecret(fc.ctx, "wallet:"+w.Name, keyBytes); err != nil {
			return counts, fmt.Errorf("wallet %s: %w", w.Name, err)
		}
		counts[7]++
	}
	return counts, nil
}

// ImportFile imports a single file (JSON or YAML). It first tries K8s-style
// single-resource format, then falls back to bulk ExportData format.
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
	fmt.Printf("Imported from %s (%d llms, %d channels, %d mcps, %d skills, %d agents, %d flows, %d runs, %d wallets)\n",
		filePath, counts[0], counts[1], counts[2], counts[3], counts[4], counts[5], counts[6], counts[7])
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
