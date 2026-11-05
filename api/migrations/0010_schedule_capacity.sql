-- GK-023 A1: schedule, soft day capacity and finished cake lots (contracts
-- r6.5 K00, K01, K04, K06; schema r1.5 handover_day, capacity_allocation,
-- finished_cake_lot, pickup_slot; owner decisions GK-001 gk019-owner-r7 and
-- GK-003 gk019-owner-r6).
--
-- Schedule. The chef publishes schedule versions: the default number of
-- standard cake places per handover day, the last bookable date (the
-- booking horizon, a concrete chef-approved date, never a month conversion),
-- opening hours per weekday, the exact 60-minute appointment starts (each
-- must end within the hours; no inferred grid) and closed dates. A version is
-- immutable and applies to new bookings only. Nothing is seeded: until the
-- chef publishes a version nothing is bookable. An appointment may not touch
-- local 01:00-02:00, where UK clocks change (K00: no nonexistent or
-- ambiguous local times), so every start maps to one instant and lasts
-- exactly 60 minutes.
--
-- Capacity. Places are counted in cakes per handover date. The limit is soft
-- (K04): admission checks committed standard cakes plus the requested
-- quantity under the day lock; tentative holds are advisory and do not own
-- the last place, so two admitted payments for the last place can both be
-- committed. Committing never refuses, and lowering a limit below what is
-- committed keeps every accepted order. Chef extras are outside the standard
-- count. Public reads show remaining = max(0, limit - committed - tentative)
-- standard places and nothing else.
--
-- Finished cake lots are physical cakes (ready stock or resale): reserved
-- exclusively, never oversold. A resale lot keeps its original handover time
-- and costs the chef-entered base amount x 50%, rounded half up.
--
-- The capacity and lot engine functions are owner-internal: checkout
-- (GK-025) calls them inside its own gateway transaction. capacity_allocation
-- refers to its order by order_ref; the foreign key to orders comes with
-- GK-025.

SET LOCAL ROLE gk_owner;

CREATE TABLE business.schedule_version (
  id             uuid PRIMARY KEY,
  revision       integer NOT NULL UNIQUE CHECK (revision >= 1),
  standard_limit integer NOT NULL CHECK (standard_limit BETWEEN 0 AND 100),
  -- Last date customers may book online (inclusive); NULL: none yet.
  bookable_until date NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  created_by     uuid NOT NULL REFERENCES business.account (id)
);

-- ISO weekday: 1 Monday ... 7 Sunday. Local Europe/London clock times.
CREATE TABLE business.schedule_window (
  schedule_version_id uuid NOT NULL REFERENCES business.schedule_version (id),
  weekday             smallint NOT NULL CHECK (weekday BETWEEN 1 AND 7),
  opens               time NOT NULL,
  closes              time NOT NULL,
  CONSTRAINT schedule_window_order CHECK (opens < closes),
  PRIMARY KEY (schedule_version_id, weekday)
);

-- Exact appointment starts; each appointment lasts 60 minutes and must end
-- within its window (checked by schedule_version_create).
CREATE TABLE business.schedule_start (
  schedule_version_id uuid NOT NULL,
  weekday             smallint NOT NULL,
  starts              time NOT NULL,
  PRIMARY KEY (schedule_version_id, weekday, starts),
  FOREIGN KEY (schedule_version_id, weekday) REFERENCES business.schedule_window (schedule_version_id, weekday)
);

CREATE TABLE business.schedule_closed_date (
  schedule_version_id uuid NOT NULL REFERENCES business.schedule_version (id),
  day                 date NOT NULL,
  PRIMARY KEY (schedule_version_id, day)
);

-- A handover day's lock row and the chef's limit override (NULL: the
-- current schedule's default). Rows are created on first use.
CREATE TABLE business.handover_day (
  day            date PRIMARY KEY,
  standard_limit integer NULL CHECK (standard_limit BETWEEN 0 AND 100),
  version        integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  updated_by     uuid NULL REFERENCES business.account (id)
);

CREATE TABLE business.capacity_allocation (
  id             uuid PRIMARY KEY,
  -- The order this allocation belongs to (FK to orders added by GK-025).
  order_ref      uuid NOT NULL,
  day            date NOT NULL REFERENCES business.handover_day (day),
  cake_quantity  integer NOT NULL CHECK (cake_quantity >= 1),
  classification text NOT NULL CHECK (classification IN ('standard', 'chef_extra')),
  state          text NOT NULL CHECK (state IN ('tentative', 'committed', 'released')),
  -- Tentative only: when reconciliation should look at it (K04: a trigger,
  -- not proof that payment failed).
  expires_at     timestamptz NULL,
  chef_evidence  text NULL CHECK (chef_evidence IS NULL OR (chef_evidence = btrim(chef_evidence)
                                   AND char_length(chef_evidence) BETWEEN 1 AND 500)),
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT capacity_allocation_extra_evidence CHECK (classification <> 'chef_extra' OR chef_evidence IS NOT NULL),
  CONSTRAINT capacity_allocation_expiry CHECK (state = 'tentative' OR expires_at IS NULL),
  UNIQUE (order_ref, day)
);

CREATE INDEX capacity_allocation_day_idx ON business.capacity_allocation (day, state);

CREATE TABLE business.finished_cake_lot (
  id                   uuid PRIMARY KEY,
  variant_id           uuid NOT NULL,
  recipe_version_id    uuid NOT NULL,
  kind                 text NOT NULL CHECK (kind IN ('ready', 'resale')),
  produced_quantity    integer NOT NULL CHECK (produced_quantity BETWEEN 1 AND 100),
  reserved_quantity    integer NOT NULL DEFAULT 0 CHECK (reserved_quantity >= 0),
  sold_quantity        integer NOT NULL DEFAULT 0 CHECK (sold_quantity >= 0),
  price_minor          bigint NOT NULL CHECK (price_minor BETWEEN 1 AND 10000000),
  resale_base_minor    bigint NULL CHECK (resale_base_minor BETWEEN 1 AND 10000000),
  original_handover_at timestamptz NULL,
  expires_at           timestamptz NOT NULL,
  released_at          timestamptz NULL,
  withdrawn_at         timestamptz NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  created_by           uuid NOT NULL REFERENCES business.account (id),
  version              integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  FOREIGN KEY (recipe_version_id, variant_id) REFERENCES business.recipe_version (id, variant_id),
  CONSTRAINT finished_cake_lot_stock CHECK (reserved_quantity + sold_quantity <= produced_quantity),
  CONSTRAINT finished_cake_lot_resale CHECK (
    (kind = 'resale') = (resale_base_minor IS NOT NULL AND original_handover_at IS NOT NULL)),
  -- K01: explicit base price x 50%, half up.
  CONSTRAINT finished_cake_lot_resale_price CHECK (kind <> 'resale' OR price_minor = (resale_base_minor + 1) / 2),
  -- A resale cake is handed over at its original time, before it expires.
  CONSTRAINT finished_cake_lot_resale_handover CHECK (original_handover_at IS NULL OR original_handover_at <= expires_at)
);

CREATE INDEX finished_cake_lot_public_idx ON business.finished_cake_lot (expires_at)
  WHERE released_at IS NOT NULL AND withdrawn_at IS NULL;

CREATE TRIGGER schedule_version_immutable BEFORE UPDATE OR DELETE ON business.schedule_version
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER schedule_window_immutable BEFORE UPDATE OR DELETE ON business.schedule_window
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER schedule_start_immutable BEFORE UPDATE OR DELETE ON business.schedule_start
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER schedule_closed_date_immutable BEFORE UPDATE OR DELETE ON business.schedule_closed_date
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER handover_day_no_delete BEFORE DELETE ON business.handover_day
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER capacity_allocation_no_delete BEFORE DELETE ON business.capacity_allocation
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER finished_cake_lot_no_delete BEFORE DELETE ON business.finished_cake_lot
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER schedule_no_truncate_version BEFORE TRUNCATE ON business.schedule_version
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER schedule_no_truncate_window BEFORE TRUNCATE ON business.schedule_window
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER schedule_no_truncate_start BEFORE TRUNCATE ON business.schedule_start
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER schedule_no_truncate_closed BEFORE TRUNCATE ON business.schedule_closed_date
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER capacity_no_truncate_day BEFORE TRUNCATE ON business.handover_day
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER capacity_no_truncate_allocation BEFORE TRUNCATE ON business.capacity_allocation
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();
CREATE TRIGGER lot_no_truncate BEFORE TRUNCATE ON business.finished_cake_lot
  FOR EACH STATEMENT EXECUTE FUNCTION business.catalog_immutable();

-- The ordinary minimum lead time (BO-GK-001.lead_time): 192 elapsed hours
-- from payment to handover (the appointment start), equality allowed.
CREATE FUNCTION business.schedule_lead_time()
RETURNS interval
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$ SELECT interval '192 hours' $$;

-- Owner-internal: the current schedule version (none: no row).
CREATE FUNCTION business.schedule_current()
RETURNS TABLE (version_id uuid, version_revision integer, version_limit integer, version_bookable_until date)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT v.id, v.revision, v.standard_limit, v.bookable_until
  FROM business.schedule_version v ORDER BY v.revision DESC LIMIT 1;
$$;

-- Owner-internal: per day of a range, the schedule facts and counts.
-- day_state: open | closed (not an opening weekday, or a closed date) |
-- outside_horizon | no_times (no published starts). last_start is the last
-- appointment start of the day (UTC instant) when open.
CREATE FUNCTION business.schedule_day_facts(p_from date, p_to date)
RETURNS TABLE (day date, day_state text, effective_limit integer, limit_override boolean, day_version integer,
               committed_standard bigint, tentative_standard bigint, chef_extra bigint,
               first_start timestamptz, last_start timestamptz, schedule_revision integer)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  WITH cur AS (SELECT * FROM business.schedule_current()),
  days AS (SELECT d::date AS day FROM pg_catalog.generate_series(p_from::timestamp, p_to::timestamp, interval '1 day') d)
  SELECT d.day,
         CASE
           WHEN cur.version_id IS NULL THEN 'outside_horizon'
           WHEN w.weekday IS NULL OR EXISTS (SELECT 1 FROM business.schedule_closed_date c
                                             WHERE c.schedule_version_id = cur.version_id AND c.day = d.day) THEN 'closed'
           WHEN cur.version_bookable_until IS NULL OR d.day > cur.version_bookable_until THEN 'outside_horizon'
           WHEN s.first_local IS NULL THEN 'no_times'
           ELSE 'open'
         END,
         coalesce(h.standard_limit, cur.version_limit, 0),
         h.standard_limit IS NOT NULL,
         coalesce(h.version, 0),
         coalesce(a.committed, 0), coalesce(a.tentative, 0), coalesce(a.extra, 0),
         CASE WHEN s.first_local IS NOT NULL THEN (d.day + s.first_local) AT TIME ZONE 'Europe/London' END,
         CASE WHEN s.last_local IS NOT NULL THEN (d.day + s.last_local) AT TIME ZONE 'Europe/London' END,
         cur.version_revision
  FROM days d
  LEFT JOIN cur ON true
  LEFT JOIN business.schedule_window w
    ON w.schedule_version_id = cur.version_id AND w.weekday = extract(isodow FROM d.day)::smallint
  LEFT JOIN LATERAL (SELECT min(st.starts) AS first_local, max(st.starts) AS last_local
                     FROM business.schedule_start st
                     WHERE st.schedule_version_id = cur.version_id AND st.weekday = w.weekday) s ON true
  LEFT JOIN business.handover_day h ON h.day = d.day
  LEFT JOIN LATERAL (SELECT sum(ca.cake_quantity) FILTER (WHERE ca.classification = 'standard' AND ca.state = 'committed') AS committed,
                            sum(ca.cake_quantity) FILTER (WHERE ca.classification = 'standard' AND ca.state = 'tentative') AS tentative,
                            sum(ca.cake_quantity) FILTER (WHERE ca.classification = 'chef_extra' AND ca.state <> 'released') AS extra
                     FROM business.capacity_allocation ca WHERE ca.day = d.day) a ON true
  ORDER BY d.day;
$$;

CREATE FUNCTION business.schedule_check_range(p_from date, p_to date)
RETURNS void
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_from IS NULL OR p_to IS NULL OR p_to < p_from OR p_to - p_from > 30 THEN
    RAISE EXCEPTION 'range_invalid' USING ERRCODE = 'GK422';
  END IF;
END;
$$;

-- ------------------------------------------------------------ public reads

-- Days of a range (at most 31) for a new purchase of p_quantity standard
-- cakes at p_now. status: open | full | lead_time (every start is less than
-- 192 hours away) | closed | outside_horizon | no_times. remaining is the
-- public count of standard places, NULL unless the day is bookable at all.
CREATE FUNCTION business.schedule_public_days(p_from date, p_to date, p_now timestamptz, p_quantity integer)
RETURNS TABLE (day date, day_status text, remaining integer, schedule_revision integer)
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.schedule_check_range(p_from, p_to);
  IF p_now IS NULL OR p_quantity IS NULL OR p_quantity NOT BETWEEN 1 AND 20 THEN
    RAISE EXCEPTION 'quantity_invalid' USING ERRCODE = 'GK422';
  END IF;
  RETURN QUERY
  SELECT f.day,
         CASE
           WHEN f.day_state <> 'open' THEN f.day_state
           WHEN f.last_start < p_now + business.schedule_lead_time() THEN 'lead_time'
           WHEN greatest(f.effective_limit - f.committed_standard - f.tentative_standard, 0) < p_quantity THEN 'full'
           ELSE 'open'
         END,
         CASE WHEN f.day_state = 'open'
              THEN greatest(f.effective_limit - f.committed_standard - f.tentative_standard, 0)::integer END,
         f.schedule_revision
  FROM business.schedule_day_facts(p_from, p_to) f;
END;
$$;

-- Appointment choices of one day for a new purchase. availability:
-- available | requestable (GK delivery before chef agreement: "subject to
-- chef confirmation") | unavailable with reason_code lead_time | full |
-- closed | outside_horizon. The slot ID is stable for a schedule version,
-- day and start. There is no per-slot cap.
CREATE FUNCTION business.schedule_public_slots(p_day date, p_mode text, p_now timestamptz, p_quantity integer)
RETURNS TABLE (slot_id uuid, starts_at timestamptz, ends_at timestamptz, availability text, reason_code text,
               schedule_revision integer)
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_mode IS NULL OR p_mode NOT IN ('collection', 'customer_courier', 'gk_delivery') THEN
    RAISE EXCEPTION 'mode_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_day IS NULL OR p_now IS NULL OR p_quantity IS NULL OR p_quantity NOT BETWEEN 1 AND 20 THEN
    RAISE EXCEPTION 'quantity_invalid' USING ERRCODE = 'GK422';
  END IF;
  RETURN QUERY
  WITH f AS (SELECT * FROM business.schedule_day_facts(p_day, p_day)),
  cur AS (SELECT * FROM business.schedule_current()),
  slots AS (
    SELECT st.starts,
           (p_day + st.starts) AT TIME ZONE 'Europe/London' AS s_at,
           ((p_day + st.starts) AT TIME ZONE 'Europe/London') + interval '60 minutes' AS e_at
    FROM cur
    JOIN business.schedule_start st
      ON st.schedule_version_id = cur.version_id AND st.weekday = extract(isodow FROM p_day)::smallint)
  SELECT pg_catalog.md5(cur.version_id::text || '|' || p_day::text || '|' || s.starts::text)::uuid,
         s.s_at, s.e_at,
         CASE
           WHEN f.day_state IN ('closed', 'outside_horizon') THEN 'unavailable'
           WHEN s.s_at < p_now + business.schedule_lead_time() THEN 'unavailable'
           WHEN greatest(f.effective_limit - f.committed_standard - f.tentative_standard, 0) < p_quantity THEN 'unavailable'
           WHEN p_mode = 'gk_delivery' THEN 'requestable'
           ELSE 'available'
         END,
         CASE
           WHEN f.day_state IN ('closed', 'outside_horizon') THEN f.day_state
           WHEN s.s_at < p_now + business.schedule_lead_time() THEN 'lead_time'
           WHEN greatest(f.effective_limit - f.committed_standard - f.tentative_standard, 0) < p_quantity THEN 'full'
           WHEN p_mode = 'gk_delivery' THEN 'chef_confirms'
           ELSE NULL
         END,
         cur.version_revision
  FROM slots s CROSS JOIN f CROSS JOIN cur
  ORDER BY s.s_at, s.starts;
END;
$$;

-- Released, unexpired finished cakes with stock left, of active cakes and
-- variants.
CREATE FUNCTION business.lot_public_list(p_now timestamptz)
RETURNS TABLE (lot_id uuid, lot_kind text, cake_id uuid, cake_name text, variant_id uuid, flavour text,
               size_code text, price_minor bigint, available integer, expires_at timestamptz,
               original_handover_at timestamptz, recipe_version_id uuid)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT l.id, l.kind, c.id, c.name, v.id, r.flavour_name, r.size_code, l.price_minor,
         l.produced_quantity - l.reserved_quantity - l.sold_quantity, l.expires_at, l.original_handover_at, r.id
  FROM business.finished_cake_lot l
  JOIN business.variant v ON v.id = l.variant_id
  JOIN business.cake c ON c.id = v.cake_id
  JOIN business.recipe_version r ON r.id = l.recipe_version_id
  WHERE l.released_at IS NOT NULL AND l.withdrawn_at IS NULL AND l.expires_at > p_now
    AND (l.original_handover_at IS NULL OR l.original_handover_at > p_now)
    AND l.produced_quantity - l.reserved_quantity - l.sold_quantity > 0
    AND c.state = 'active' AND v.archived_at IS NULL
  ORDER BY l.expires_at, l.id
  LIMIT 200;
$$;

-- ------------------------------------------------------------ staff reads

-- A published version (p_version NULL: the current one), or NULL.
CREATE FUNCTION business.schedule_staff_version(p_actor uuid, p_version uuid)
RETURNS json
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_result json;
BEGIN
  PERFORM business.catalog_staff_account(p_actor, ARRAY['chef', 'fulfillment', 'technical_support']);
  SELECT pg_catalog.json_build_object(
           'revision', v.revision, 'standard_limit', v.standard_limit, 'bookable_until', v.bookable_until,
           'created_at', v.created_at,
           'weekdays', coalesce((
             SELECT pg_catalog.json_agg(pg_catalog.json_build_object(
                      'weekday', w.weekday, 'opens', pg_catalog.to_char(w.opens, 'HH24:MI'),
                      'closes', pg_catalog.to_char(w.closes, 'HH24:MI'),
                      'starts', coalesce((SELECT pg_catalog.json_agg(pg_catalog.to_char(st.starts, 'HH24:MI') ORDER BY st.starts)
                                          FROM business.schedule_start st
                                          WHERE st.schedule_version_id = w.schedule_version_id AND st.weekday = w.weekday),
                                         '[]'::json)) ORDER BY w.weekday)
             FROM business.schedule_window w WHERE w.schedule_version_id = v.id), '[]'::json),
           'closed_dates', coalesce((SELECT pg_catalog.json_agg(c.day ORDER BY c.day)
                                     FROM business.schedule_closed_date c WHERE c.schedule_version_id = v.id), '[]'::json))
  INTO v_result
  FROM business.schedule_version v
  WHERE p_version IS NULL OR v.id = p_version
  ORDER BY v.revision DESC LIMIT 1;
  RETURN v_result;
END;
$$;

-- The chef's and fulfilment's day view: limit, counts, workload (standard
-- plus chef extras, which stay visible) and whether online booking has
-- closed (every start is less than 192 hours away, or outside the horizon).
CREATE FUNCTION business.schedule_staff_days(p_actor uuid, p_from date, p_to date, p_now timestamptz)
RETURNS TABLE (day date, day_state text, effective_limit integer, limit_override boolean, day_version integer,
               committed_standard bigint, tentative_standard bigint, chef_extra bigint, workload bigint,
               remaining integer, online_closed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.catalog_staff_account(p_actor, ARRAY['chef', 'fulfillment', 'technical_support']);
  PERFORM business.schedule_check_range(p_from, p_to);
  RETURN QUERY
  SELECT f.day, f.day_state, f.effective_limit, f.limit_override, f.day_version,
         f.committed_standard, f.tentative_standard, f.chef_extra,
         f.committed_standard + f.tentative_standard + f.chef_extra,
         greatest(f.effective_limit - f.committed_standard - f.tentative_standard, 0)::integer,
         f.day_state <> 'open' OR f.last_start < p_now + business.schedule_lead_time()
  FROM business.schedule_day_facts(p_from, p_to) f;
END;
$$;

CREATE FUNCTION business.lot_staff_list(p_actor uuid, p_include_inactive boolean, p_lot uuid)
RETURNS TABLE (lot_id uuid, lot_kind text, cake_id uuid, cake_name text, variant_id uuid, flavour text,
               size_code text, price_minor bigint, resale_base_minor bigint, produced integer, reserved integer,
               sold integer, expires_at timestamptz, original_handover_at timestamptz, released_at timestamptz,
               withdrawn_at timestamptz, lot_version integer, recipe_version_id uuid, created_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.catalog_staff_account(p_actor, ARRAY['chef', 'fulfillment', 'technical_support']);
  RETURN QUERY
  SELECT l.id, l.kind, c.id, c.name, v.id, r.flavour_name, r.size_code, l.price_minor, l.resale_base_minor,
         l.produced_quantity, l.reserved_quantity, l.sold_quantity, l.expires_at, l.original_handover_at,
         l.released_at, l.withdrawn_at, l.version, r.id, l.created_at
  FROM business.finished_cake_lot l
  JOIN business.variant v ON v.id = l.variant_id
  JOIN business.cake c ON c.id = v.cake_id
  JOIN business.recipe_version r ON r.id = l.recipe_version_id
  WHERE (p_lot IS NULL OR l.id = p_lot) AND (p_include_inactive OR p_lot IS NOT NULL OR l.withdrawn_at IS NULL)
  ORDER BY l.created_at DESC, l.id DESC
  LIMIT 500;
END;
$$;

-- ---------------------------------------------------------------- commands

-- Publishes the next schedule version. p_windows:
-- [{"weekday":4,"opens":"11:00","closes":"17:00","starts":["11:00","11:30"]}]
-- with unique weekdays (1-7), HH:MM local times, and starts whose 60-minute
-- appointment ends within the window. p_expected_revision is the current
-- revision (0 when none).
CREATE FUNCTION business.schedule_version_create(
  p_actor uuid, p_key text, p_body_sha256 text, p_expected_revision integer, p_standard_limit integer,
  p_bookable_until date, p_windows json, p_closed_dates date[])
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'schedule.version_create', '', p_key, p_body_sha256);
  v_id uuid := pg_catalog.gen_random_uuid();
  v_current integer;
  v_window json;
  v_start json;
  v_weekday smallint;
  v_opens time;
  v_closes time;
  v_starts time;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  -- Serialize publishers: the newest version row would not exist for the
  -- first one, so lock on a transaction advisory key instead.
  PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('business.schedule_version', 0));
  SELECT coalesce(max(v.revision), 0) INTO v_current FROM business.schedule_version v;
  IF p_expected_revision IS NULL OR p_expected_revision < 0 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  IF p_expected_revision <> v_current THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF p_windows IS NULL OR pg_catalog.json_typeof(p_windows) <> 'array' OR pg_catalog.json_array_length(p_windows) > 7 THEN
    RAISE EXCEPTION 'windows_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_closed_dates IS NOT NULL AND (coalesce(pg_catalog.array_ndims(p_closed_dates), 1) <> 1
     OR pg_catalog.array_position(p_closed_dates, NULL) IS NOT NULL OR pg_catalog.cardinality(p_closed_dates) > 400) THEN
    RAISE EXCEPTION 'closed_dates_invalid' USING ERRCODE = 'GK422';
  END IF;
  BEGIN
    INSERT INTO business.schedule_version (id, revision, standard_limit, bookable_until, created_by)
    VALUES (v_id, v_current + 1, p_standard_limit, p_bookable_until, v_account);
  EXCEPTION WHEN check_violation OR not_null_violation THEN
    RAISE EXCEPTION 'standard_limit_invalid' USING ERRCODE = 'GK422';
  END;
  FOR v_window IN SELECT e FROM pg_catalog.json_array_elements(p_windows) e LOOP
    IF pg_catalog.json_typeof(v_window) <> 'object'
       OR pg_catalog.json_typeof(v_window -> 'weekday') IS DISTINCT FROM 'number'
       OR coalesce((v_window ->> 'weekday') !~ '^[1-7]$', true)
       OR pg_catalog.json_typeof(v_window -> 'opens') IS DISTINCT FROM 'string'
       OR pg_catalog.json_typeof(v_window -> 'closes') IS DISTINCT FROM 'string'
       OR coalesce((v_window ->> 'opens') !~ '^([01][0-9]|2[0-3]):[0-5][0-9]$', true)
       OR coalesce((v_window ->> 'closes') !~ '^([01][0-9]|2[0-3]):[0-5][0-9]$', true)
       OR pg_catalog.json_typeof(v_window -> 'starts') IS DISTINCT FROM 'array'
       OR pg_catalog.json_array_length(v_window -> 'starts') > 48 THEN
      RAISE EXCEPTION 'windows_invalid' USING ERRCODE = 'GK422';
    END IF;
    v_weekday := (v_window ->> 'weekday')::smallint;
    v_opens := (v_window ->> 'opens')::time;
    v_closes := (v_window ->> 'closes')::time;
    BEGIN
      INSERT INTO business.schedule_window (schedule_version_id, weekday, opens, closes)
      VALUES (v_id, v_weekday, v_opens, v_closes);
    EXCEPTION
      WHEN unique_violation THEN RAISE EXCEPTION 'windows_invalid' USING ERRCODE = 'GK422';
      WHEN check_violation THEN RAISE EXCEPTION 'windows_invalid' USING ERRCODE = 'GK422';
    END;
    FOR v_start IN SELECT e FROM pg_catalog.json_array_elements(v_window -> 'starts') e LOOP
      IF pg_catalog.json_typeof(v_start) <> 'string'
         OR (v_start #>> '{}') !~ '^([01][0-9]|2[0-3]):[0-5][0-9]$' THEN
        RAISE EXCEPTION 'starts_invalid' USING ERRCODE = 'GK422';
      END IF;
      v_starts := (v_start #>> '{}')::time;
      -- The 60-minute appointment must start and end within the window
      -- (BO-GK-001.pickup_slot end_must_be_within_pickup_hours), compared
      -- as intervals so nothing wraps at midnight, and must not touch local
      -- 01:00-02:00 (UK clock changes).
      IF v_starts < v_opens OR v_starts::interval + interval '60 minutes' > v_closes::interval
         OR (v_starts > time '00:00' AND v_starts < time '02:00') THEN
        RAISE EXCEPTION 'starts_invalid' USING ERRCODE = 'GK422';
      END IF;
      BEGIN
        INSERT INTO business.schedule_start (schedule_version_id, weekday, starts) VALUES (v_id, v_weekday, v_starts);
      EXCEPTION WHEN unique_violation THEN
        RAISE EXCEPTION 'starts_invalid' USING ERRCODE = 'GK422';
      END;
    END LOOP;
  END LOOP;
  BEGIN
    INSERT INTO business.schedule_closed_date (schedule_version_id, day)
    SELECT v_id, d FROM pg_catalog.unnest(coalesce(p_closed_dates, '{}'::date[])) d;
  EXCEPTION WHEN unique_violation THEN
    RAISE EXCEPTION 'closed_dates_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', v_account, 'schedule.version_published', 'schedule_version', v_id, NULL,
                                pg_catalog.json_build_object('revision', v_current + 1));
  PERFORM business.command_finish(v_account, 'schedule.version_create', '', p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- Owner-internal: lock a handover day (creating its row) and return its
-- effective standard limit. Lock order (K00): handover days by date.
CREATE FUNCTION business.capacity_day_lock(p_day date)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_override integer;
BEGIN
  INSERT INTO business.handover_day (day) VALUES (p_day) ON CONFLICT DO NOTHING;
  SELECT h.standard_limit INTO v_override FROM business.handover_day h WHERE h.day = p_day FOR UPDATE;
  RETURN coalesce(v_override, (SELECT version_limit FROM business.schedule_current()), 0);
END;
$$;

-- Sets a day's standard limit (a chef override of the schedule default).
-- Lowering it below what is committed keeps every accepted order (K04).
-- p_expected_version is the day's version, 0 when the day has no row yet.
-- p_limit NULL clears the override (the schedule default applies again).
CREATE FUNCTION business.capacity_day_limit_set(p_actor uuid, p_key text, p_body_sha256 text, p_day date,
                                                p_expected_version integer, p_limit integer)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  -- Days have no UUID: the receipt resource and result are a UUID derived
  -- from the date.
  v_result uuid := pg_catalog.md5('handover_day|' || coalesce(p_day::text, ''))::uuid;
  v_prior uuid := business.command_claim(v_account, 'capacity.day_limit_set', v_result::text, p_key, p_body_sha256);
  v_version integer;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  IF p_day IS NULL THEN
    RAISE EXCEPTION 'day_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_limit NOT BETWEEN 0 AND 100 THEN
    RAISE EXCEPTION 'standard_limit_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_expected_version IS NULL OR p_expected_version < 0 THEN
    RAISE EXCEPTION 'expected_version_required' USING ERRCODE = 'GK422';
  END IF;
  SELECT h.version INTO v_version FROM business.handover_day h WHERE h.day = p_day FOR UPDATE;
  IF coalesce(v_version, 0) <> p_expected_version THEN
    RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
  END IF;
  IF v_version IS NULL THEN
    BEGIN
      INSERT INTO business.handover_day (day, standard_limit, updated_by) VALUES (p_day, p_limit, v_account);
    EXCEPTION WHEN unique_violation THEN
      -- Another command or checkout created the day meanwhile.
      RAISE EXCEPTION 'stale_version' USING ERRCODE = 'GK409';
    END;
  ELSE
    UPDATE business.handover_day h
    SET standard_limit = p_limit, version = h.version + 1, updated_at = pg_catalog.now(), updated_by = v_account
    WHERE h.day = p_day;
  END IF;
  PERFORM business.audit_record('staff', v_account, 'capacity.day_limit_set', 'handover_day', v_result, NULL,
                                pg_catalog.json_build_object('day', p_day, 'standard_limit', p_limit));
  PERFORM business.command_finish(v_account, 'capacity.day_limit_set', v_result::text, p_key, v_result);
  RETURN QUERY SELECT v_result, false;
END;
$$;

-- Owner-internal (checkout, GK-025): admit a tentative standard allocation
-- of p_quantity cakes for an order on a day. Refuses (GK409 day_full) when
-- committed standard cakes plus the quantity exceed the day's limit;
-- tentative holds of other orders do not count against admission.
CREATE FUNCTION business.capacity_admit(p_order uuid, p_day date, p_quantity integer, p_expires_at timestamptz)
RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_limit integer;
  v_committed bigint;
  v_id uuid := pg_catalog.gen_random_uuid();
BEGIN
  IF p_order IS NULL OR p_day IS NULL OR p_quantity IS NULL OR p_quantity < 1 OR p_expires_at IS NULL THEN
    RAISE EXCEPTION 'allocation_invalid' USING ERRCODE = 'GK422';
  END IF;
  v_limit := business.capacity_day_lock(p_day);
  SELECT coalesce(sum(a.cake_quantity), 0) INTO v_committed
  FROM business.capacity_allocation a
  WHERE a.day = p_day AND a.classification = 'standard' AND a.state = 'committed';
  IF v_committed + p_quantity > v_limit THEN
    RAISE EXCEPTION 'day_full' USING ERRCODE = 'GK409';
  END IF;
  BEGIN
    INSERT INTO business.capacity_allocation (id, order_ref, day, cake_quantity, classification, state, expires_at)
    VALUES (v_id, p_order, p_day, p_quantity, 'standard', 'tentative', p_expires_at);
  EXCEPTION WHEN unique_violation THEN
    RAISE EXCEPTION 'duplicate' USING ERRCODE = 'GK409';
  END;
  RETURN v_id;
END;
$$;

-- Owner-internal: commit an order's tentative allocation (a successful
-- payment or free confirmation). Never refuses for capacity: an admitted
-- valid payment keeps its acceptance even over the limit (K04). Returns false
-- when it was already committed.
CREATE FUNCTION business.capacity_commit(p_order uuid, p_day date)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_state text;
BEGIN
  PERFORM business.capacity_day_lock(p_day);
  SELECT a.state INTO v_state FROM business.capacity_allocation a
  WHERE a.order_ref = p_order AND a.day = p_day FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'allocation not found' USING ERRCODE = 'GK404';
  ELSIF v_state = 'committed' THEN
    RETURN false;
  ELSIF v_state = 'released' THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  UPDATE business.capacity_allocation a SET state = 'committed', expires_at = NULL, updated_at = pg_catalog.now()
  WHERE a.order_ref = p_order AND a.day = p_day;
  RETURN true;
END;
$$;

-- Owner-internal: release an order's allocation (terminal unpaid, or a
-- chef cancellation). Returns false when already released.
CREATE FUNCTION business.capacity_release(p_order uuid, p_day date)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM business.capacity_day_lock(p_day);
  UPDATE business.capacity_allocation a SET state = 'released', expires_at = NULL, updated_at = pg_catalog.now()
  WHERE a.order_ref = p_order AND a.day = p_day AND a.state <> 'released';
  IF FOUND THEN
    RETURN true;
  END IF;
  PERFORM 1 FROM business.capacity_allocation a WHERE a.order_ref = p_order AND a.day = p_day;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'allocation not found' USING ERRCODE = 'GK404';
  END IF;
  RETURN false;
END;
$$;

-- Records a finished cake lot of a variant's current recipe version (not
-- yet for sale). Ready lots carry the chef's price; resale lots the base
-- amount (price = base x 50%, half up) and the original handover time.
CREATE FUNCTION business.lot_create(p_actor uuid, p_key text, p_body_sha256 text, p_variant uuid, p_kind text,
                                    p_quantity integer, p_price_minor bigint, p_resale_base_minor bigint,
                                    p_original_handover_at timestamptz, p_expires_at timestamptz)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'lot.create', coalesce(p_variant::text, ''), p_key, p_body_sha256);
  v_id uuid := pg_catalog.gen_random_uuid();
  v_recipe uuid;
  v_archived timestamptz;
  v_stock text;
  v_price bigint;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  SELECT v.current_recipe_version_id, v.archived_at, v.stock_mode INTO v_recipe, v_archived, v_stock
  FROM business.variant v WHERE v.id = p_variant FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'variant not found' USING ERRCODE = 'GK404';
  END IF;
  IF v_archived IS NOT NULL OR v_recipe IS NULL THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  IF p_kind = 'resale' THEN
    IF p_price_minor IS NOT NULL OR p_resale_base_minor IS NULL OR p_original_handover_at IS NULL THEN
      RAISE EXCEPTION 'lot_invalid' USING ERRCODE = 'GK422';
    END IF;
    v_price := (p_resale_base_minor + 1) / 2;
  ELSIF p_kind = 'ready' THEN
    -- Ready stock is a ready-stock variant; its per-cake price is the
    -- chef's (GK-002 ready_cake_price_editing).
    IF v_stock <> 'ready_stock' THEN
      RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
    END IF;
    IF p_price_minor IS NULL OR p_resale_base_minor IS NOT NULL OR p_original_handover_at IS NOT NULL THEN
      RAISE EXCEPTION 'lot_invalid' USING ERRCODE = 'GK422';
    END IF;
    v_price := p_price_minor;
  ELSE
    RAISE EXCEPTION 'lot_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_expires_at IS NULL OR p_expires_at <= pg_catalog.now() THEN
    RAISE EXCEPTION 'expires_invalid' USING ERRCODE = 'GK422';
  END IF;
  IF p_kind = 'resale' AND (p_original_handover_at <= pg_catalog.now() OR p_original_handover_at > p_expires_at) THEN
    RAISE EXCEPTION 'handover_invalid' USING ERRCODE = 'GK422';
  END IF;
  BEGIN
    INSERT INTO business.finished_cake_lot (id, variant_id, recipe_version_id, kind, produced_quantity, price_minor,
                                            resale_base_minor, original_handover_at, expires_at, created_by)
    VALUES (v_id, p_variant, v_recipe, p_kind, p_quantity, v_price, p_resale_base_minor, p_original_handover_at,
            p_expires_at, v_account);
  EXCEPTION WHEN check_violation OR not_null_violation THEN
    RAISE EXCEPTION 'lot_invalid' USING ERRCODE = 'GK422';
  END;
  PERFORM business.audit_record('staff', v_account, 'lot.created', 'finished_cake_lot', v_id, NULL,
                                pg_catalog.json_build_object('kind', p_kind, 'quantity', p_quantity));
  PERFORM business.command_finish(v_account, 'lot.create', p_variant::text, p_key, v_id);
  RETURN QUERY SELECT v_id, false;
END;
$$;

-- release: offer the lot for sale (chef release, K01); withdraw: stop
-- offering it (reservations and sales already made stay).
CREATE FUNCTION business.lot_set_state(p_actor uuid, p_key text, p_body_sha256 text, p_lot uuid,
                                       p_expected_version integer, p_action text)
RETURNS TABLE (result_id uuid, replayed boolean)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_account uuid := business.catalog_staff_account(p_actor, ARRAY['chef']);
  v_prior uuid := business.command_claim(v_account, 'lot.set_state', coalesce(p_lot::text, ''), p_key, p_body_sha256);
  v_version integer;
  v_released timestamptz;
  v_withdrawn timestamptz;
BEGIN
  IF v_prior IS NOT NULL THEN
    RETURN QUERY SELECT v_prior, true;
    RETURN;
  END IF;
  IF p_action IS NULL OR p_action NOT IN ('release', 'withdraw') THEN
    RAISE EXCEPTION 'action_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT l.version, l.released_at, l.withdrawn_at INTO v_version, v_released, v_withdrawn
  FROM business.finished_cake_lot l WHERE l.id = p_lot FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'lot not found' USING ERRCODE = 'GK404';
  END IF;
  PERFORM business.catalog_expect_version(v_version, p_expected_version);
  IF v_withdrawn IS NOT NULL OR (p_action = 'release' AND v_released IS NOT NULL) THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  UPDATE business.finished_cake_lot l
  SET released_at = CASE WHEN p_action = 'release' THEN pg_catalog.now() ELSE l.released_at END,
      withdrawn_at = CASE WHEN p_action = 'withdraw' THEN pg_catalog.now() ELSE l.withdrawn_at END,
      version = l.version + 1
  WHERE l.id = p_lot;
  PERFORM business.audit_record('staff', v_account, 'lot.' || CASE WHEN p_action = 'release' THEN 'released' ELSE 'withdrawn' END,
                                'finished_cake_lot', p_lot, NULL, pg_catalog.json_build_object('version', v_version + 1));
  PERFORM business.command_finish(v_account, 'lot.set_state', p_lot::text, p_key, p_lot);
  RETURN QUERY SELECT p_lot, false;
END;
$$;

-- Owner-internal (checkout): reserve p_quantity cakes of a released,
-- unexpired lot exclusively; GK409 stock_unavailable otherwise. Lot rows are
-- locked by ID (K00 finished-cake-lot rank).
CREATE FUNCTION business.lot_reserve(p_lot uuid, p_quantity integer, p_now timestamptz)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_lot business.finished_cake_lot%ROWTYPE;
BEGIN
  IF p_quantity IS NULL OR p_quantity < 1 OR p_now IS NULL THEN
    RAISE EXCEPTION 'quantity_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT * INTO v_lot FROM business.finished_cake_lot l WHERE l.id = p_lot FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'lot not found' USING ERRCODE = 'GK404';
  END IF;
  IF v_lot.released_at IS NULL OR v_lot.withdrawn_at IS NOT NULL OR v_lot.expires_at <= p_now
     OR (v_lot.original_handover_at IS NOT NULL AND v_lot.original_handover_at <= p_now)
     OR v_lot.produced_quantity - v_lot.reserved_quantity - v_lot.sold_quantity < p_quantity
     OR NOT EXISTS (SELECT 1 FROM business.variant v JOIN business.cake c ON c.id = v.cake_id
                    WHERE v.id = v_lot.variant_id AND v.archived_at IS NULL AND c.state = 'active') THEN
    RAISE EXCEPTION 'stock_unavailable' USING ERRCODE = 'GK409';
  END IF;
  UPDATE business.finished_cake_lot l SET reserved_quantity = l.reserved_quantity + p_quantity WHERE l.id = p_lot;
END;
$$;

-- Owner-internal: return reserved cakes (terminal unpaid) or turn them into
-- sold ones (paid). No restock of sold cakes (K07).
CREATE FUNCTION business.lot_settle(p_lot uuid, p_quantity integer, p_sold boolean)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_reserved integer;
BEGIN
  IF p_quantity IS NULL OR p_quantity < 1 OR p_sold IS NULL THEN
    RAISE EXCEPTION 'quantity_invalid' USING ERRCODE = 'GK422';
  END IF;
  SELECT l.reserved_quantity INTO v_reserved FROM business.finished_cake_lot l WHERE l.id = p_lot FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'lot not found' USING ERRCODE = 'GK404';
  END IF;
  IF v_reserved < p_quantity THEN
    RAISE EXCEPTION 'state_conflict' USING ERRCODE = 'GK409';
  END IF;
  UPDATE business.finished_cake_lot l
  SET reserved_quantity = l.reserved_quantity - p_quantity,
      sold_quantity = l.sold_quantity + CASE WHEN p_sold THEN p_quantity ELSE 0 END
  WHERE l.id = p_lot;
END;
$$;

REVOKE ALL ON TABLE business.schedule_version, business.schedule_window, business.schedule_start,
  business.schedule_closed_date, business.handover_day, business.capacity_allocation, business.finished_cake_lot FROM PUBLIC;

REVOKE ALL ON FUNCTION business.schedule_lead_time() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.schedule_current() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.schedule_day_facts(date, date) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.schedule_check_range(date, date) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.schedule_public_days(date, date, timestamptz, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.schedule_public_slots(date, text, timestamptz, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.lot_public_list(timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.schedule_staff_version(uuid, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.schedule_staff_days(uuid, date, date, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.lot_staff_list(uuid, boolean, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.schedule_version_create(uuid, text, text, integer, integer, date, json, date[]) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.capacity_day_lock(date) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.capacity_day_limit_set(uuid, text, text, date, integer, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.capacity_admit(uuid, date, integer, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.capacity_commit(uuid, date) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.capacity_release(uuid, date) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.lot_create(uuid, text, text, uuid, text, integer, bigint, bigint, timestamptz, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.lot_set_state(uuid, text, text, uuid, integer, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.lot_reserve(uuid, integer, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.lot_settle(uuid, integer, boolean) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.schedule_public_days(date, date, timestamptz, integer) TO gk_api;
GRANT EXECUTE ON FUNCTION business.schedule_public_slots(date, text, timestamptz, integer) TO gk_api;
GRANT EXECUTE ON FUNCTION business.lot_public_list(timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.schedule_staff_version(uuid, uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.schedule_staff_days(uuid, date, date, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.lot_staff_list(uuid, boolean, uuid) TO gk_api;
GRANT EXECUTE ON FUNCTION business.schedule_version_create(uuid, text, text, integer, integer, date, json, date[]) TO gk_api;
GRANT EXECUTE ON FUNCTION business.capacity_day_limit_set(uuid, text, text, date, integer, integer) TO gk_api;
GRANT EXECUTE ON FUNCTION business.lot_create(uuid, text, text, uuid, text, integer, bigint, bigint, timestamptz, timestamptz) TO gk_api;
GRANT EXECUTE ON FUNCTION business.lot_set_state(uuid, text, text, uuid, integer, text) TO gk_api;
