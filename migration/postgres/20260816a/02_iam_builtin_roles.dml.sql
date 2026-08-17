INSERT INTO iam_namespace (id, name, labels, description, namespace_id, status, created_by, updated_by)
VALUES ('default', 'Default', '{}', 'Default Flowgent organization/team boundary', 'default', 'ACTIVE', 'system', 'system')
ON CONFLICT (id) DO NOTHING;

INSERT INTO iam_role (id, name, builtin, permissions, description, namespace_id, created_by, updated_by)
VALUES
 ('builtin-platform-owner', 'platform-owner', true, '["*"]', 'Deployment break-glass and platform administration', '*', 'system', 'system'),
 ('builtin-namespace-owner', 'owner', true, '["namespace.*","agent.*","skill.*","flow.*","flow_release.*","llm_provider.*","mcp.*","notification.*","knowledge.*","run.*","approval.*","trace.*","iam.*","service_account.*"]', 'Full ownership of one namespace', '*', 'system', 'system'),
 ('builtin-flow-owner', 'flow-owner', true, '["flow.*","run.*","task.read","approval.*","trace.read","flow_release.*"]', 'Full administration of one exact Flow and its execution evidence', '*', 'system', 'system'),
 ('builtin-maintainer', 'maintainer', true, '["namespace.read","agent.*","skill.*","flow.*","flow_release.*","llm_provider.read","llm_provider.use","mcp.read","mcp.use","notification.read","knowledge.*","run.*","approval.*","trace.read"]', 'Maintain resources and operations without secret or membership administration', '*', 'system', 'system'),
 ('builtin-writer', 'writer', true, '["namespace.read","agent.read","agent.use","agent.write","skill.read","skill.use","skill.write","flow.read","flow.use","flow.write","flow_release.read","flow_release.install","llm_provider.read","llm_provider.use","mcp.read","mcp.use","notification.read","knowledge.read","knowledge.write","run.read","run.trigger","run.cancel","approval.read","trace.read"]', 'Author resources and trigger runs', '*', 'system', 'system'),
 ('builtin-operator', 'operator', true, '["namespace.read","agent.read","agent.use","skill.read","skill.use","flow.read","flow.use","flow_release.read","flow_release.install","llm_provider.read","llm_provider.use","mcp.read","mcp.use","notification.read","run.read","run.trigger","run.cancel","approval.read","approval.resolve","trace.read"]', 'Operate approved flows without modifying definitions', '*', 'system', 'system'),
 ('builtin-reader', 'reader', true, '["namespace.read","agent.read","skill.read","flow.read","flow_release.read","llm_provider.read","mcp.read","notification.read","knowledge.read","run.read","approval.read","trace.read"]', 'Read non-secret namespace resources and execution evidence', '*', 'system', 'system'),
 ('builtin-secret-manager', 'secret-manager', true, '["namespace.read","llm_provider.read","llm_provider.secret.manage","mcp.read","mcp.secret.manage","notification.read","notification.secret.manage"]', 'Manage write-only integration secrets', '*', 'system', 'system'),
 ('builtin-auditor', 'auditor', true, '["namespace.read","run.read","approval.read","trace.read","iam.audit.read"]', 'Read execution and immutable security audit evidence', '*', 'system', 'system'),
 ('builtin-system-controller', 'system-controller', true, '["flow.read","run.read","run.internal.create","namespace.runtime.manage"]', 'Controller workload permissions', '*', 'system', 'system'),
 ('builtin-system-jobmanager', 'system-jobmanager', true, '["flow.read","agent.read","agent.use","skill.read","skill.use","llm_provider.read","llm_provider.use","mcp.read","mcp.use","run.read","run.internal.update","task.read","task.internal.write","approval.internal.create","notification.emit"]', 'JobManager workload permissions', '*', 'system', 'system'),
 ('builtin-system-taskmanager', 'system-taskmanager', true, '["agent.read","agent.use","skill.read","skill.use","llm_provider.read","llm_provider.use","mcp.read","mcp.use","run.read","task.read","task.internal.write","approval.internal.create","notification.emit"]', 'TaskManager workload permissions', '*', 'system', 'system'),
 ('builtin-system-notifier', 'system-notifier', true, '["notification.read","notification.internal.deliver","approval.platform.read"]', 'Notifier workload permissions', '*', 'system', 'system'),
 ('builtin-system-a2a', 'system-a2a', true, '["flow.read","flow.use","run.read","run.trigger","run.cancel","approval.read","approval.resolve","trace.read"]', 'A2A gateway workload permissions', '*', 'system', 'system'),
 ('builtin-system-allinone', 'system-allinone', true, '["flow.read","flow.use","run.read","run.trigger","run.cancel","run.internal.create","run.internal.update","agent.read","agent.use","skill.read","skill.use","llm_provider.read","llm_provider.use","mcp.read","mcp.use","task.read","task.internal.write","approval.read","approval.resolve","approval.internal.create","approval.platform.read","notification.read","notification.emit","notification.internal.deliver","trace.read","namespace.runtime.manage"]', 'In-process development runtime permissions', '*', 'system', 'system')
ON CONFLICT (id) DO UPDATE SET permissions=EXCLUDED.permissions, description=EXCLUDED.description, updated_at=NOW(), updated_by='system';

INSERT INTO iam_principal (id, type, issuer, external_id, username, display_name, description, created_by, updated_by)
VALUES
 ('breakglass:root', 'user', 'flowgent:breakglass', 'root', 'breakglass-root', 'Flowgent break-glass administrator', 'Rotatable deployment emergency identity', 'system', 'system'),
 ('service:controller', 'service_account', 'flowgent:internal', 'controller', 'controller', 'Flowgent Controller', 'Internal workload identity', 'system', 'system'),
 ('service:jobmanager', 'service_account', 'flowgent:internal', 'jobmanager', 'jobmanager', 'Flowgent JobManager', 'Internal workload identity', 'system', 'system'),
 ('service:taskmanager', 'service_account', 'flowgent:internal', 'taskmanager', 'taskmanager', 'Flowgent TaskManager', 'Internal workload identity', 'system', 'system'),
 ('service:notifier', 'service_account', 'flowgent:internal', 'notifier', 'notifier', 'Flowgent Notifier', 'Internal workload identity', 'system', 'system'),
 ('service:a2a', 'service_account', 'flowgent:internal', 'a2a', 'a2a', 'Flowgent A2A Gateway', 'Internal workload identity', 'system', 'system'),
 ('service:allinone', 'service_account', 'flowgent:internal', 'allinone', 'allinone', 'Flowgent All-in-One Runtime', 'Ephemeral in-process development identity', 'system', 'system')
ON CONFLICT (id) DO UPDATE SET display_name=EXCLUDED.display_name, updated_at=NOW(), updated_by='system';

INSERT INTO iam_role_binding (id, role_id, subject_type, subject_id, resource_type, resource_id, effect, namespace_id, description, created_by, updated_by)
VALUES
 ('binding-breakglass-root', 'builtin-platform-owner', 'user', 'breakglass:root', 'platform', '*', 'ALLOW', '*', 'Deployment break-glass owner', 'system', 'system'),
 ('binding-system-controller', 'builtin-system-controller', 'service_account', 'service:controller', 'platform', '*', 'ALLOW', '*', 'Controller workload grant', 'system', 'system'),
 ('binding-system-jobmanager', 'builtin-system-jobmanager', 'service_account', 'service:jobmanager', 'platform', '*', 'ALLOW', '*', 'JobManager workload grant', 'system', 'system'),
 ('binding-system-taskmanager', 'builtin-system-taskmanager', 'service_account', 'service:taskmanager', 'platform', '*', 'ALLOW', '*', 'TaskManager workload grant', 'system', 'system'),
 ('binding-system-notifier', 'builtin-system-notifier', 'service_account', 'service:notifier', 'platform', '*', 'ALLOW', '*', 'Notifier workload grant', 'system', 'system'),
 ('binding-system-a2a', 'builtin-system-a2a', 'service_account', 'service:a2a', 'platform', '*', 'ALLOW', '*', 'A2A workload grant', 'system', 'system'),
 ('binding-system-allinone', 'builtin-system-allinone', 'service_account', 'service:allinone', 'platform', '*', 'ALLOW', '*', 'All-in-one runtime grant', 'system', 'system')
ON CONFLICT (id) DO UPDATE SET role_id=EXCLUDED.role_id, updated_at=NOW(), updated_by='system';
