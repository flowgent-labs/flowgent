package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
	"github.com/flowgent-labs/flowgent/store/src/flowrun"
	"github.com/flowgent-labs/flowgent/store/src/taskplan"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FlowRunHandler struct {
	runStore  flowrun.IFlowRunStore
	taskStore taskplan.ITaskPlanStore
	logger    *utils.Logger
}

func NewFlowRunHandler(s store.IStore, logger *utils.Logger) *FlowRunHandler {
	var runStore flowrun.IFlowRunStore
	var taskStore taskplan.ITaskPlanStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		runStore = flowrun.NewFlowRunPostgresStore(db)
		taskStore = taskplan.NewTaskPlanPostgresStore(db)
	case *sql.DB:
		runStore = flowrun.NewFlowRunSQLiteStore(db)
		taskStore = taskplan.NewTaskPlanSQLiteStore(db)
	}
	return &FlowRunHandler{runStore: runStore, taskStore: taskStore, logger: logger}
}

func (h *FlowRunHandler) List(w http.ResponseWriter, r *http.Request) {
	runs, err := h.runStore.Select(r.Context(), 0, 50)
	if err != nil { http.Error(w, err.Error(), 500); return }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

func (h *FlowRunHandler) Get(w http.ResponseWriter, r *http.Request) {
	run, err := h.runStore.Get(r.Context(), r.PathValue("id"))
	if err != nil || run == nil { http.Error(w, "not found", 404); return }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

func (h *FlowRunHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.runStore.Delete(r.Context(), r.PathValue("id")); err != nil { http.Error(w, "internal", 500); return }
	w.WriteHeader(200)
}

func (h *FlowRunHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	if err := h.runStore.Cancel(r.Context(), r.PathValue("id")); err != nil { http.Error(w, "internal", 500); return }
	w.WriteHeader(200)
}

func (h *FlowRunHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.taskStore.ListByFlowRun(r.Context(), r.PathValue("id"))
	if err != nil { http.Error(w, err.Error(), 500); return }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func (h *FlowRunHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	task, err := h.taskStore.Get(r.Context(), r.PathValue("task_id"))
	if err != nil || task == nil { http.Error(w, "not found", 404); return }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func (h *FlowRunHandler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	var task model.TaskRun
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil { http.Error(w, "invalid body", 400); return }
	task.ID = r.PathValue("task_id")
	if err := h.taskStore.UpdateTaskRun(r.Context(), &task); err != nil { h.logger.Error("update task", "error", err); http.Error(w, "internal", 500); return }
	w.WriteHeader(200)
}
