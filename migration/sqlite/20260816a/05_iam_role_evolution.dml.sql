-- Idempotent role evolution for databases that already applied the original
-- IAM seed file. UPSERT preserves role IDs and all referencing bindings.
INSERT INTO iam_role
    (id,name,builtin,permissions,description,namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag)
VALUES
 ('builtin-flow-owner','flow-owner',1,'["flow.*","run.*","task.read","approval.*","trace.read","flow_release.*"]','Full administration of one exact Flow and its execution evidence','*','ACTIVE',datetime('now'),'system',datetime('now'),'system',0),
 ('builtin-maintainer','maintainer',1,'["namespace.read","agent.*","skill.*","flow.*","flow_release.*","llm_provider.read","llm_provider.use","mcp.read","mcp.use","notification.read","knowledge.*","run.*","approval.*","trace.read"]','Maintain resources and operations without secret or membership administration','*','ACTIVE',datetime('now'),'system',datetime('now'),'system',0),
 ('builtin-writer','writer',1,'["namespace.read","agent.read","agent.use","agent.write","skill.read","skill.use","skill.write","flow.read","flow.use","flow.write","flow_release.read","flow_release.install","llm_provider.read","llm_provider.use","mcp.read","mcp.use","notification.read","knowledge.read","knowledge.write","run.read","run.trigger","run.cancel","approval.read","trace.read"]','Author resources and trigger runs','*','ACTIVE',datetime('now'),'system',datetime('now'),'system',0),
 ('builtin-operator','operator',1,'["namespace.read","agent.read","agent.use","skill.read","skill.use","flow.read","flow.use","flow_release.read","flow_release.install","llm_provider.read","llm_provider.use","mcp.read","mcp.use","notification.read","run.read","run.trigger","run.cancel","approval.read","approval.resolve","trace.read"]','Operate approved flows without modifying definitions','*','ACTIVE',datetime('now'),'system',datetime('now'),'system',0),
 ('builtin-reader','reader',1,'["namespace.read","agent.read","skill.read","flow.read","flow_release.read","llm_provider.read","mcp.read","notification.read","knowledge.read","run.read","approval.read","trace.read"]','Read non-secret namespace resources and execution evidence','*','ACTIVE',datetime('now'),'system',datetime('now'),'system',0)
ON CONFLICT(id) DO UPDATE SET
    permissions=excluded.permissions,
    description=excluded.description,
    status='ACTIVE',
    del_flag=0,
    updated_at=datetime('now'),
    updated_by='system';
