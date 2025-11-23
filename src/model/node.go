package model

type NodeType string

const (
	AgentNode      NodeType = "agent"
	ToolNode       NodeType = "tool"
	MapNode        NodeType = "map"
	AgentFlowNode  NodeType = "agentflow"
	ConditionNode  NodeType = "condition"
	TribunalNode   NodeType = "tribunal"
	HumanNode      NodeType = "human"
	SupervisorNode NodeType = "supervisor"
	NoopNode       NodeType = "noop"
)
