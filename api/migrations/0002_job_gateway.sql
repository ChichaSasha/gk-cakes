-- GK-020: job lease table and SECURITY DEFINER gateway (K00 r3, schema r1.5 "job").
--
-- Runtime roles never write business.job directly. The worker claims, extends,
-- completes and releases leases only through the functions below; generation
-- fences stale finalization after a lease is reclaimed. Retry counting,
-- attention escalation and dispatch journals belong to later owning tickets.

SET LOCAL ROLE gk_owner;

CREATE TABLE business.job (
  id              uuid PRIMARY KEY,
  operation_id    uuid NOT NULL,
  state           text NOT NULL DEFAULT 'queued'
                  CHECK (state IN ('queued', 'running', 'retry', 'attention', 'completed')),
  next_attempt_at timestamptz NOT NULL,
  lease_owner     uuid,
  lease_until     timestamptz,
  generation      bigint NOT NULL DEFAULT 0 CHECK (generation >= 0),
  version         integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT job_lease_matches_state
    CHECK ((state = 'running') = (lease_owner IS NOT NULL AND lease_until IS NOT NULL))
);

CREATE INDEX job_claimable_idx ON business.job (next_attempt_at) WHERE state IN ('queued', 'retry');
CREATE INDEX job_expired_lease_idx ON business.job (lease_until) WHERE state = 'running';

-- Owner-internal: future domain commands enqueue inside their own transaction.
CREATE FUNCTION business.job_enqueue(p_operation_id uuid, p_not_before timestamptz)
RETURNS uuid
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  INSERT INTO business.job (id, operation_id, next_attempt_at)
  VALUES (pg_catalog.gen_random_uuid(), p_operation_id,
          GREATEST(coalesce(p_not_before, pg_catalog.now()), pg_catalog.now()))
  RETURNING id;
$$;

CREATE FUNCTION business.job_claim(p_worker uuid, p_lease interval)
RETURNS TABLE (job_id uuid, job_operation_id uuid, job_generation bigint, job_lease_until timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_worker IS NULL OR p_lease IS NULL OR p_lease <= interval '0' OR p_lease > interval '10 minutes' THEN
    RAISE EXCEPTION 'job_claim requires a worker and a lease between 0 and 10 minutes'
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  RETURN QUERY
  WITH candidate AS (
    SELECT j.id
    FROM business.job j
    WHERE (j.state IN ('queued', 'retry') AND j.next_attempt_at <= pg_catalog.now())
       OR (j.state = 'running' AND j.lease_until < pg_catalog.now())
    ORDER BY j.next_attempt_at, j.id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
  )
  UPDATE business.job j
  SET state = 'running',
      lease_owner = p_worker,
      lease_until = pg_catalog.now() + p_lease,
      generation = j.generation + 1,
      version = j.version + 1,
      updated_at = pg_catalog.now()
  FROM candidate c
  WHERE j.id = c.id
  RETURNING j.id, j.operation_id, j.generation, j.lease_until;
END;
$$;

-- Returns the new lease expiry, or NULL when the caller no longer holds the lease.
CREATE FUNCTION business.job_heartbeat(p_job_id uuid, p_worker uuid, p_generation bigint, p_lease interval)
RETURNS timestamptz
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_until timestamptz;
BEGIN
  IF p_lease IS NULL OR p_lease <= interval '0' OR p_lease > interval '10 minutes' THEN
    RAISE EXCEPTION 'job_heartbeat requires a lease between 0 and 10 minutes'
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  UPDATE business.job
  SET lease_until = pg_catalog.now() + p_lease,
      version = version + 1,
      updated_at = pg_catalog.now()
  WHERE id = p_job_id AND state = 'running' AND lease_owner = p_worker AND generation = p_generation
  RETURNING lease_until INTO v_until;
  RETURN v_until;
END;
$$;

-- Returns false when the caller's lease generation is stale.
CREATE FUNCTION business.job_complete(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  UPDATE business.job
  SET state = 'completed',
      lease_owner = NULL,
      lease_until = NULL,
      version = version + 1,
      updated_at = pg_catalog.now()
  WHERE id = p_job_id AND state = 'running' AND lease_owner = p_worker AND generation = p_generation;
  RETURN FOUND;
END;
$$;

-- Gives the lease back: immediately claimable (queued) when p_retry_after is
-- zero, otherwise scheduled as retry. Returns false when the lease is stale.
CREATE FUNCTION business.job_release(p_job_id uuid, p_worker uuid, p_generation bigint, p_retry_after interval)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_retry_after IS NULL OR p_retry_after < interval '0' OR p_retry_after > interval '1 day' THEN
    RAISE EXCEPTION 'job_release requires a retry delay between 0 and 1 day'
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  UPDATE business.job
  SET state = CASE WHEN p_retry_after > interval '0' THEN 'retry' ELSE 'queued' END,
      next_attempt_at = pg_catalog.now() + p_retry_after,
      lease_owner = NULL,
      lease_until = NULL,
      version = version + 1,
      updated_at = pg_catalog.now()
  WHERE id = p_job_id AND state = 'running' AND lease_owner = p_worker AND generation = p_generation;
  RETURN FOUND;
END;
$$;

REVOKE ALL ON TABLE business.job FROM PUBLIC;
REVOKE ALL ON FUNCTION business.job_enqueue(uuid, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.job_claim(uuid, interval) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.job_heartbeat(uuid, uuid, bigint, interval) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.job_complete(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.job_release(uuid, uuid, bigint, interval) FROM PUBLIC;

-- Worker observation and lease operations only; no direct DML for any runtime.
GRANT SELECT ON TABLE business.job TO gk_worker;
GRANT EXECUTE ON FUNCTION business.job_claim(uuid, interval) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.job_heartbeat(uuid, uuid, bigint, interval) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.job_complete(uuid, uuid, bigint) TO gk_worker;
GRANT EXECUTE ON FUNCTION business.job_release(uuid, uuid, bigint, interval) TO gk_worker;
