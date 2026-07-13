package store

import (
	"database/sql"
	"fmt"
	"testing"
	
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	_ "modernc.org/sqlite"
)

func TestSQLiteRoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil { t.Fatal(err) }
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE orh_agentflow (
		id           TEXT PRIMARY KEY,
		agentflow_id TEXT    NOT NULL,
		version      INTEGER NOT NULL DEFAULT 1,
		definition   TEXT    NOT NULL,
		checksum     TEXT,
		comment      TEXT,
		priority     TEXT    DEFAULT 'medium',
		namespace    TEXT    DEFAULT '',
		mode         TEXT    DEFAULT '',
		labels       TEXT    DEFAULT '{}',
		description  TEXT    NOT NULL DEFAULT '',
		tenant_id    TEXT    NOT NULL DEFAULT 'default',
		status       TEXT    NOT NULL DEFAULT 'ACTIVE',
		created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
		created_by   TEXT    NOT NULL DEFAULT '',
		updated_at   TEXT    NOT NULL DEFAULT (datetime('now')),
		updated_by   TEXT    NOT NULL DEFAULT '',
		del_flag     INTEGER NOT NULL DEFAULT 0,
		UNIQUE(agentflow_id, version)
	)`)
	if err != nil { t.Fatal(err) }

	_, err = db.Exec(`INSERT INTO orh_agentflow (id, agentflow_id, version, definition, comment) 
		VALUES ('test1', 'flow1', 1, '{"id":"flow1","nodes":[]}', 'test')`)
	if err != nil { t.Fatal(err) }

	cols := utils.Columns[entities.FlowVersionInfo]()
	query := fmt.Sprintf("SELECT %s FROM orh_agentflow ORDER BY created_at DESC LIMIT 1", cols)
	t.Logf("Query: %s", query)

	row := db.QueryRow(query)
	var v entities.FlowVersionInfo
	err = utils.ScanStruct(row, &v)
	if err != nil {
		t.Fatalf("ScanStruct failed: %v", err)
	}
	t.Logf("OK: id=%s, flow_id=%s, comment=%s", v.ID, v.FlowID, v.Comment)
	t.Logf("   created_at=%v, updated_at=%v", v.CreatedAt, v.UpdatedAt)
}
