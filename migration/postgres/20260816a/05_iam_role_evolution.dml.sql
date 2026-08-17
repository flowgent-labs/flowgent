-- Idempotent role evolution for databases that already applied the original
-- IAM seed file. UPSERT preserves role IDs and all referencing bindings.
INSERT INTO iam_role
    (id,name,builtin,permissions,description,namespace_id,status,created_by,updated_by)
VALUES
 ('builtin-flow-owner','flow-owner',true,'["flow.*","run.*","task.read","approval.*","trace.read","flow_release.*"]','Full administration of one exact Flow and its execution evidence','*','ACTIVE','system','system'),
 ('builtin-maintainer','maintainer',true,'["namespace.read","agent.*","skill.*","flow.*","flow_release.*","llm_provider.read","llm_provider.use","mcp.read","mcp.use","notification.read","knowledge.*","run.*","approval.*","trace.read"]','Maintain resources and operations without secret or membership administration','*','ACTIVE','system','system'),
 ('builtin-writer','writer',true,'["namespace.read","agent.read","agent.use","agent.write","skill.read","skill.use","skill.write","flow.read","flow.use","flow.write","flow_release.read","flow_release.install","llm_provider.read","llm_provider.use","mcp.read","mcp.use","notification.read","knowledge.read","knowledge.write","run.read","run.trigger","run.cancel","approval.read","trace.read"]','Author resources and trigger runs','*','ACTIVE','system','system'),
 ('builtin-operator','operator',true,'["namespace.read","agent.read","agent.use","skill.read","skill.use","flow.read","flow.use","flow_release.read","flow_release.install","llm_provider.read","llm_provider.use","mcp.read","mcp.use","notification.read","run.read","run.trigger","run.cancel","approval.read","approval.resolve","trace.read"]','Operate approved flows without modifying definitions','*','ACTIVE','system','system'),
 ('builtin-reader','reader',true,'["namespace.read","agent.read","skill.read","flow.read","flow_release.read","llm_provider.read","mcp.read","notification.read","knowledge.read","run.read","approval.read","trace.read"]','Read non-secret namespace resources and execution evidence','*','ACTIVE','system','system')
ON CONFLICT(id) DO UPDATE SET
    permissions=EXCLUDED.permissions,
    description=EXCLUDED.description,
    status='ACTIVE',
    del_flag=false,
    updated_at=NOW(),
    updated_by='system';
