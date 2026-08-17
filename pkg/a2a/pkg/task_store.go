package a2a

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	protocol "github.com/a2aproject/a2a-go/a2a"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PersistentTaskStore keeps A2A protocol task state in Flowgent's shared
// database. Tasks are isolated by a one-way digest of the caller credential;
// bearer credentials are never persisted.
type PersistentTaskStore struct {
	sqlite   *sql.DB
	postgres *pgxpool.Pool
}

// NewPersistentTaskStore adapts Flowgent's configured database to the A2A SDK
// task-store contract. Migrations are owned by the regular store bootstrap.
func NewPersistentTaskStore(store storepkg.IStore) (*PersistentTaskStore, error) {
	switch db := store.DB().(type) {
	case *sql.DB:
		return &PersistentTaskStore{sqlite: db}, nil
	case *pgxpool.Pool:
		return &PersistentTaskStore{postgres: db}, nil
	default:
		return nil, fmt.Errorf("a2a: unsupported store %T", store.DB())
	}
}

func (s *PersistentTaskStore) Save(
	ctx context.Context,
	task *protocol.Task,
	_ protocol.Event,
	_ *protocol.Task,
	previousVersion protocol.TaskVersion,
) (protocol.TaskVersion, error) {
	if task == nil || task.ID == "" {
		return protocol.TaskVersionMissing, fmt.Errorf("task id is required: %w", protocol.ErrInvalidParams)
	}
	payload, err := json.Marshal(task)
	if err != nil {
		return protocol.TaskVersionMissing, fmt.Errorf("marshal task: %w", err)
	}
	if s.sqlite != nil {
		return s.saveSQLite(ctx, task, payload, previousVersion)
	}
	return s.savePostgres(ctx, task, payload, previousVersion)
}

func (s *PersistentTaskStore) saveSQLite(ctx context.Context, task *protocol.Task, payload []byte, previousVersion protocol.TaskVersion) (protocol.TaskVersion, error) {
	tx, err := s.sqlite.BeginTx(ctx, nil)
	if err != nil {
		return protocol.TaskVersionMissing, err
	}
	defer func() { _ = tx.Rollback() }()

	var current int64
	var owner string
	err = tx.QueryRowContext(ctx, `SELECT version, caller_key FROM a2a_task WHERE id=?`, task.ID).Scan(&current, &owner)
	next := int64(1)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	caller := a2aCallerKey(ctx)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `INSERT INTO a2a_task
            (id, caller_key, context_id, state, task_json, version, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, 1, ?, ?)`, task.ID, caller, task.ContextID, task.Status.State, string(payload), now, now)
	case err != nil:
		return protocol.TaskVersionMissing, err
	default:
		if owner != caller || (previousVersion != protocol.TaskVersionMissing && int64(previousVersion) != current) {
			return protocol.TaskVersionMissing, protocol.ErrConcurrentTaskModification
		}
		next = current + 1
		_, err = tx.ExecContext(ctx, `UPDATE a2a_task
            SET context_id=?, state=?, task_json=?, version=?, updated_at=? WHERE id=? AND caller_key=?`,
			task.ContextID, task.Status.State, string(payload), next, now, task.ID, caller)
	}
	if err != nil {
		return protocol.TaskVersionMissing, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.TaskVersionMissing, err
	}
	return protocol.TaskVersion(next), nil
}

func (s *PersistentTaskStore) savePostgres(ctx context.Context, task *protocol.Task, payload []byte, previousVersion protocol.TaskVersion) (protocol.TaskVersion, error) {
	tx, err := s.postgres.Begin(ctx)
	if err != nil {
		return protocol.TaskVersionMissing, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current int64
	var owner string
	err = tx.QueryRow(ctx, `SELECT version, caller_key FROM a2a_task WHERE id=$1 FOR UPDATE`, task.ID).Scan(&current, &owner)
	next := int64(1)
	caller := a2aCallerKey(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		_, err = tx.Exec(ctx, `INSERT INTO a2a_task
            (id, caller_key, context_id, state, task_json, version) VALUES ($1, $2, $3, $4, $5, 1)`,
			task.ID, caller, task.ContextID, task.Status.State, payload)
	case err != nil:
		return protocol.TaskVersionMissing, err
	default:
		if owner != caller || (previousVersion != protocol.TaskVersionMissing && int64(previousVersion) != current) {
			return protocol.TaskVersionMissing, protocol.ErrConcurrentTaskModification
		}
		next = current + 1
		_, err = tx.Exec(ctx, `UPDATE a2a_task
            SET context_id=$1, state=$2, task_json=$3, version=$4, updated_at=NOW()
            WHERE id=$5 AND caller_key=$6`, task.ContextID, task.Status.State, payload, next, task.ID, caller)
	}
	if err != nil {
		return protocol.TaskVersionMissing, err
	}
	if err := tx.Commit(ctx); err != nil {
		return protocol.TaskVersionMissing, err
	}
	return protocol.TaskVersion(next), nil
}

func (s *PersistentTaskStore) Get(ctx context.Context, taskID protocol.TaskID) (*protocol.Task, protocol.TaskVersion, error) {
	var payload []byte
	var version int64
	var err error
	if s.sqlite != nil {
		var raw string
		err = s.sqlite.QueryRowContext(ctx, `SELECT task_json, version FROM a2a_task WHERE id=? AND caller_key=?`, taskID, a2aCallerKey(ctx)).Scan(&raw, &version)
		payload = []byte(raw)
	} else {
		err = s.postgres.QueryRow(ctx, `SELECT task_json, version FROM a2a_task WHERE id=$1 AND caller_key=$2`, taskID, a2aCallerKey(ctx)).Scan(&payload, &version)
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return nil, protocol.TaskVersionMissing, protocol.ErrTaskNotFound
	}
	if err != nil {
		return nil, protocol.TaskVersionMissing, err
	}
	var task protocol.Task
	if err := json.Unmarshal(payload, &task); err != nil {
		return nil, protocol.TaskVersionMissing, fmt.Errorf("decode stored task: %w", err)
	}
	return &task, protocol.TaskVersion(version), nil
}

func (s *PersistentTaskStore) List(ctx context.Context, req *protocol.ListTasksRequest) (*protocol.ListTasksResponse, error) {
	if req == nil {
		req = &protocol.ListTasksRequest{}
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("page size must be between 1 and 100")
	}
	if req.HistoryLength < 0 {
		return nil, fmt.Errorf("history length must be non-negative")
	}
	offset, err := decodePageOffset(req.PageToken)
	if err != nil {
		return nil, err
	}
	if s.sqlite != nil {
		return s.listSQLite(ctx, req, pageSize, offset)
	}
	return s.listPostgres(ctx, req, pageSize, offset)
}

func (s *PersistentTaskStore) listSQLite(ctx context.Context, req *protocol.ListTasksRequest, pageSize, offset int) (*protocol.ListTasksResponse, error) {
	where := []string{"caller_key=?"}
	args := []any{a2aCallerKey(ctx)}
	if req.ContextID != "" {
		where = append(where, "context_id=?")
		args = append(args, req.ContextID)
	}
	if req.Status != protocol.TaskStateUnspecified {
		where = append(where, "state=?")
		args = append(args, req.Status)
	}
	if req.LastUpdatedAfter != nil {
		where = append(where, "updated_at>?")
		args = append(args, req.LastUpdatedAfter.UTC().Format(time.RFC3339Nano))
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.sqlite.QueryRowContext(ctx, `SELECT COUNT(*) FROM a2a_task WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	queryArgs := append(append([]any{}, args...), pageSize, offset)
	rows, err := s.sqlite.QueryContext(ctx, `SELECT task_json FROM a2a_task WHERE `+clause+` ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return decodeTaskPage(rows, req, pageSize, offset, total)
}

func (s *PersistentTaskStore) listPostgres(ctx context.Context, req *protocol.ListTasksRequest, pageSize, offset int) (*protocol.ListTasksResponse, error) {
	where := []string{"caller_key=$1"}
	args := []any{a2aCallerKey(ctx)}
	add := func(column string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf("%s=$%d", column, len(args)))
	}
	if req.ContextID != "" {
		add("context_id", req.ContextID)
	}
	if req.Status != protocol.TaskStateUnspecified {
		add("state", req.Status)
	}
	if req.LastUpdatedAfter != nil {
		args = append(args, req.LastUpdatedAfter.UTC())
		where = append(where, fmt.Sprintf("updated_at>$%d", len(args)))
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.postgres.QueryRow(ctx, `SELECT COUNT(*) FROM a2a_task WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	queryArgs := append(append([]any{}, args...), pageSize, offset)
	query := fmt.Sprintf(`SELECT task_json FROM a2a_task WHERE %s ORDER BY updated_at DESC, id DESC LIMIT $%d OFFSET $%d`, clause, len(args)+1, len(args)+2)
	rows, err := s.postgres.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]*protocol.Task, 0, pageSize)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		task, err := decodeListedTask(payload, req)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return taskPage(tasks, pageSize, offset, total), nil
}

type rowScanner interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func decodeTaskPage(rows rowScanner, req *protocol.ListTasksRequest, pageSize, offset, total int) (*protocol.ListTasksResponse, error) {
	tasks := make([]*protocol.Task, 0, pageSize)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		task, err := decodeListedTask([]byte(raw), req)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return taskPage(tasks, pageSize, offset, total), nil
}

func decodeListedTask(payload []byte, req *protocol.ListTasksRequest) (*protocol.Task, error) {
	var task protocol.Task
	if err := json.Unmarshal(payload, &task); err != nil {
		return nil, fmt.Errorf("decode stored task: %w", err)
	}
	if req.HistoryLength > 0 && len(task.History) > req.HistoryLength {
		task.History = task.History[len(task.History)-req.HistoryLength:]
	}
	if !req.IncludeArtifacts {
		task.Artifacts = nil
	}
	return &task, nil
}

func taskPage(tasks []*protocol.Task, pageSize, offset, total int) *protocol.ListTasksResponse {
	next := ""
	if offset+len(tasks) < total {
		next = encodePageOffset(offset + len(tasks))
	}
	return &protocol.ListTasksResponse{Tasks: tasks, TotalSize: total, PageSize: pageSize, NextPageToken: next}
}

func a2aCallerKey(ctx context.Context) string {
	token := bearerTokenFromContext(ctx)
	if token == "" {
		return "anonymous"
	}
	digest := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func encodePageOffset(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodePageOffset(token string) (int, error) {
	if token == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, fmt.Errorf("invalid page token")
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid page token")
	}
	return offset, nil
}
