-- Hard-delete any activities that were previously soft-deleted, along with
-- their source_activity_refs so re-importing the same file works cleanly.

-- 1. Capture the ref IDs we are about to orphan.
CREATE TEMP TABLE _soft_deleted_refs AS
SELECT a.source_activity_ref_id AS ref_id
FROM activities a
WHERE a.delete_indicator = true
  AND a.source_activity_ref_id IS NOT NULL;

-- 2. Delete the soft-deleted activity rows (cascades to all child tables via FK ON DELETE CASCADE).
DELETE FROM activities WHERE delete_indicator = true;

-- 3. Delete the now-orphaned source_activity_refs.
DELETE FROM source_activity_refs
WHERE id IN (SELECT ref_id FROM _soft_deleted_refs);
