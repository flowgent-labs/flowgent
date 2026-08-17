CREATE OR REPLACE FUNCTION ensure_namespace_default_resource_pool()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO orh_resource_pool
        (id,name,replicas,slots_per_pod,sandbox_replicas,sandbox_slots_per_pod,
         description,namespace_id,status,created_by,updated_by)
    VALUES
        ('builtin-default-resource-pool-' || NEW.id,'default',1,4,1,4,
         'Default namespace worker capacity',NEW.id,'ACTIVE',NEW.created_by,NEW.created_by)
    ON CONFLICT (id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS iam_namespace_default_resource_pool ON iam_namespace;
CREATE TRIGGER iam_namespace_default_resource_pool
AFTER INSERT ON iam_namespace
FOR EACH ROW EXECUTE FUNCTION ensure_namespace_default_resource_pool();
