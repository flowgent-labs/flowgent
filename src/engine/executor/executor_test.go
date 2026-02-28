package executor

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/src/model"
)

func TestConditionExecutor_True(t *testing.T) {
	e := &ConditionExecutor{}
	if e.TaskType() != model.TaskCondition {
		t.Error("wrong task type")
	}
	plan := &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Expression: "${input.result == true}"},
		Input:    map[string]any{"result": true},
	}
	scope := map[string]map[string]any{"input": plan.Input}
	result, err := e.Execute(context.Background(), plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["result"] != true {
		t.Error("expected true")
	}
}

func TestConditionExecutor_False(t *testing.T) {
	e := &ConditionExecutor{}
	plan := &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Expression: "false"},
	}
	result, _ := e.Execute(context.Background(), plan, nil)
	if result.Output["result"] != false {
		t.Error("expected false")
	}
}

func TestTaskExecutorRouter(t *testing.T) {
	router := NewTaskExecutorRouter()
	router.Register(&ConditionExecutor{})
	router.Register(&NoopExecutor{})

	plan := &model.ExecutionPlan{
		TaskType: model.TaskCondition,
		NodeSpec: &model.NodeSpec{Expression: "true"},
	}
	result, err := router.Execute(context.Background(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["result"] != true {
		t.Error("expected true from condition executor")
	}
}

func TestNoopExecutor(t *testing.T) {
	e := &NoopExecutor{}
	if e.TaskType() != model.TaskNoop {
		t.Error("wrong task type")
	}
	result, _ := e.Execute(context.Background(), nil, nil)
	if result.Output != nil {
		t.Error("noop should return nil output")
	}
}

func TestMapExecutor(t *testing.T) {
	e := &MapExecutor{}
	if e.TaskType() != model.TaskMap {
		t.Error("wrong task type")
	}
	result, _ := e.Execute(context.Background(), nil, nil)
	if result.Output["status"] != "dispatched" {
		t.Error("map should return dispatched")
	}
}
