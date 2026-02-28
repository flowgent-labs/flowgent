package model

type NodeType string

const (
	AgentNode      NodeType = "agent"
	ToolNode       NodeType = "tool"
	SkillNode      NodeType = "skill"
	MapNode        NodeType = "map"
	AgentFlowNode  NodeType = "agentflow"
	ConditionNode  NodeType = "condition"
	TribunalNode   NodeType = "tribunal"
	HumanNode      NodeType = "human"
	SupervisorNode NodeType = "supervisor"
	SandboxNode    NodeType = "sandbox"
	NoopNode       NodeType = "noop"
)
