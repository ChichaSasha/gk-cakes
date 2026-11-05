-- GK-024/025/026: transactional order emails through the K08 engine
-- (contracts r6.5 K08 "Transaction inserts notice intent+job+audit
-- atomically, unique transition/recipient/template revision", "Financial
-- notices distinguish requested/pending from successful return", "No
-- marketing"; K11 "append outcome/version acknowledgment independently
-- before exposing a confirmed response or confirmation email"; K09 minimal
-- personal data).
--
-- 1. Order notices, rendered and enqueued (business.notification_enqueue:
--    intent, job and audit) inside the transaction of their transition:
--      confirmed / paid_awaiting_agreement / cancelled: in
--        business.order_command_secured, when this call moves the order's
--        recovery state to secured (never before: the outcome envelope is
--        acknowledged first, K11); one per order command;
--      released: the hold review's release of an abandoned order (AFTER
--        UPDATE trigger), only when a payment was actually dispatched to
--        Stripe (a collection with dispatch_started_at; never-dispatched
--        ones do not count) or the customer asked to confirm a free order;
--        an abandoned checkout that never got that far is not emailed;
--      refund_returned: when the order's refund block
--        (business.order_refund_summary) is 'returned' with a positive
--        target and the order is not cancellation_pending; one per
--        refund-goal version. Deferred constraint triggers recheck at commit
--        after any change that can reach it (refund operations and
--        observations, the goal, a secured cancellation, a secured order
--        command such as a published manual payout). Partial returns are
--        not emailed.
--    business.order_notice records each (order, kind, dedupe key) once, so
--    retries, replays, duplicate events and K11 resumes give one notice
--    each. Its intent ID and idempotency key derive from environment,
--    order, kind and key: a transition replayed after a restore reuses
--    them, so the provider deduplicates rather than sending twice. The
--    recipient is the order's contact address, frozen into the payload;
--    recipient_id is the order. Rendering (British English, plain text and
--    simple HTML, London time, whole pounds without decimals) happens here,
--    in the payload's exact canonical JSON (notifications.ParseDispatch).
--    No personal data goes into the audit trail or order_notice. A notice
--    never rolls back its transition: a recipient the dispatcher would
--    refuse is 'not_queued' (detail 'recipient'), one outside the recipient
--    allowlist (outside production) 'not_queued' (detail 'allowlist'), a
--    rendering/enqueue error 'not_queued' (the SQLSTATE), and a database
--    whose API has not recorded the notice settings yet 'not_configured'.
--    Each of these is audited ('order.notice_not_queued' on the order, in
--    the staff audit summary) and, unless deliberate, flagged for staff
--    (4). A cancellation notice says the refund was requested, or already
--    returned when it succeeded before the cancellation was secured.
--    Manual payout instructions are not emailed.
-- 2. business.notice_settings: the environment, the UI origin of order
--    page links and the recipient allowlist, recorded by the API
--    (business.notice_settings_record, gk_api) at start and on every
--    readiness probe (a write only when something changed).
-- 3. Staging recipient allowlist (EMAIL_RECIPIENT_ALLOWLIST, API and
--    worker; required in staging, refused in production). First layer:
--    order_notice_enqueue never queues a notice to another recipient
--    (above), so no worker build can send it. Second layer:
--    business.notification_dispatch_suppress (worker, lease-fenced, freeze
--    first): the dispatcher found the stored recipient outside this
--    environment's allowlist, so the intent is suspended with a fixed reason
--    (the existing suspended_reason; no new state) and audited as
--    'notification.suppressed'. Answers 'suppressed' (never dispatched:
--    the job completes), 'suppressed_after_dispatch' (an earlier attempt
--    may have been sent: manager attention), 'settled' (nothing to stop),
--    'already' (suspended before) or 'frozen' (restore freeze: nothing
--    changes). The provider is never called for a suppressed intent.
-- 4. business.order_attention_flags gains 'notice_not_sent' (staff list
--    and detail).
--
-- Expand-only (K09): new tables, functions and triggers;
-- order_command_secured and order_attention_flags are replaced with their
-- signatures and grants.

SET LOCAL ROLE gk_owner;

-- 2. Notice settings (one row): the environment, the UI origin of order
-- page links and the staging recipient allowlist (lower-case exact
-- addresses or "@domain" entries; required in staging, empty in
-- production, optional elsewhere).
CREATE TABLE business.notice_settings (
  singleton           boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  environment         text NOT NULL CHECK (environment ~ '^[a-z0-9][a-z0-9-]{0,31}$'),
  ui_origin           text NOT NULL CHECK (ui_origin ~ '^https?://[a-z0-9.-]+(:[0-9]{1,5})?$' AND octet_length(ui_origin) <= 200),
  recipient_allowlist text[] NOT NULL DEFAULT '{}' CHECK (pg_catalog.cardinality(recipient_allowlist) <= 50),
  recorded_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT notice_settings_allowlist CHECK (
    CASE environment WHEN 'production' THEN pg_catalog.cardinality(recipient_allowlist) = 0
                     WHEN 'staging' THEN pg_catalog.cardinality(recipient_allowlist) > 0
                     ELSE true END)
);

-- API: record this environment, its UI origin (UI_ORIGIN) and its
-- EMAIL_RECIPIENT_ALLOWLIST, all validated by the API's configuration with
-- the same rules. Writes only when something changed (readiness calls it on
-- every probe).
CREATE FUNCTION business.notice_settings_record(p_env text, p_ui_origin text, p_allowlist text[])
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_env IS NULL OR p_env !~ '^[a-z0-9][a-z0-9-]{0,31}$'
     OR p_ui_origin IS NULL OR p_ui_origin !~ '^https?://[a-z0-9.-]+(:[0-9]{1,5})?$' OR octet_length(p_ui_origin) > 200
     OR (p_env IN ('staging', 'production') AND p_ui_origin !~ '^https://') THEN
    RAISE EXCEPTION 'notice settings need a lower-case environment and an exact origin' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  IF p_allowlist IS NULL OR pg_catalog.cardinality(p_allowlist) > 50
     OR EXISTS (SELECT 1 FROM pg_catalog.unnest(p_allowlist) e
                WHERE e IS NULL OR e !~ '^([a-z0-9._%+-]+@|@)[a-z0-9-]+(\.[a-z0-9-]+)+$')
     OR (p_env = 'production' AND pg_catalog.cardinality(p_allowlist) > 0)
     OR (p_env = 'staging' AND pg_catalog.cardinality(p_allowlist) = 0) THEN
    RAISE EXCEPTION 'the recipient allowlist is required in staging, refused in production and lists lower-case addresses or @domain entries'
      USING ERRCODE = 'invalid_parameter_value';
  END IF;
  INSERT INTO business.notice_settings AS s (environment, ui_origin, recipient_allowlist) VALUES (p_env, p_ui_origin, p_allowlist)
  ON CONFLICT (singleton) DO UPDATE SET environment = EXCLUDED.environment, ui_origin = EXCLUDED.ui_origin,
                                        recipient_allowlist = EXCLUDED.recipient_allowlist, recorded_at = pg_catalog.now()
  WHERE (s.environment, s.ui_origin, s.recipient_allowlist)
        IS DISTINCT FROM (EXCLUDED.environment, EXCLUDED.ui_origin, EXCLUDED.recipient_allowlist);
END;
$$;

-- Owner-internal: whether the settings let a notice go to p_addr (a bare
-- address): always in production; elsewhere when the allowlist is empty
-- (local, test) or lists the address or its exact domain. Staging always
-- has an allowlist (table check), so it fails closed.
CREATE FUNCTION business.notice_recipient_allowed(p_s business.notice_settings, p_addr text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT p_s.environment = 'production' OR pg_catalog.cardinality(p_s.recipient_allowlist) = 0
         OR EXISTS (SELECT 1 FROM pg_catalog.unnest(p_s.recipient_allowlist) e
                    WHERE e = pg_catalog.lower(p_addr)
                       OR (pg_catalog.left(e, 1) = '@' AND pg_catalog.right(pg_catalog.lower(p_addr), pg_catalog.length(e)) = e
                           AND pg_catalog.strpos(pg_catalog.lower(p_addr), '@') = pg_catalog.length(p_addr) - pg_catalog.length(e) + 1));
$$;

-- 1. One row per order notice (no personal data).
CREATE TABLE business.order_notice (
  id                uuid PRIMARY KEY,
  order_id          uuid NOT NULL REFERENCES business.orders (id),
  kind              text NOT NULL CHECK (kind IN ('confirmed', 'paid_awaiting_agreement', 'released', 'cancelled',
                                                  'refund_returned')),
  -- The transition's own key: the order command, the refund-goal version,
  -- or 'released'.
  dedupe            text NOT NULL CHECK (dedupe ~ '^[a-z0-9-]{1,64}$'),
  transition_id     uuid NOT NULL,
  template_revision text NOT NULL CHECK (template_revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
  -- queued: the intent exists; not_configured: no notice settings yet;
  -- not_queued: the stored address is not one the dispatcher accepts, or
  -- rendering/enqueue failed (detail: 'recipient' or the SQLSTATE).
  outcome           text NOT NULL CHECK (outcome IN ('queued', 'not_configured', 'not_queued')),
  intent_id         uuid NULL UNIQUE REFERENCES business.notification_intent (id),
  detail            text NULL CHECK (detail ~ '^[a-z0-9]{1,16}$'),
  created_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT order_notice_once UNIQUE (order_id, kind, dedupe),
  CONSTRAINT order_notice_intent CHECK ((outcome = 'queued') = (intent_id IS NOT NULL)
                                       AND (outcome = 'not_queued') = (detail IS NOT NULL))
);

CREATE TRIGGER order_notice_immutable BEFORE UPDATE OR DELETE ON business.order_notice
  FOR EACH ROW EXECUTE FUNCTION business.catalog_immutable();

-- Owner-internal: p as a JSON string exactly as Go's encoding/json writes
-- it with HTML escaping off (notifications.ParseDispatch requires the
-- canonical bytes): \" \\ \b \f \n \r \t, other controls as \u00xx (lower
-- case), U+2028/U+2029 escaped, everything else literal.
CREATE FUNCTION business.notice_json(p text)
RETURNS text
LANGUAGE plpgsql
IMMUTABLE
STRICT
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v text := p;
BEGIN
  v := pg_catalog.replace(v, '\', '\\');
  v := pg_catalog.replace(v, '"', '\"');
  v := pg_catalog.replace(v, pg_catalog.chr(8), '\b');
  v := pg_catalog.replace(v, pg_catalog.chr(9), '\t');
  v := pg_catalog.replace(v, pg_catalog.chr(10), '\n');
  v := pg_catalog.replace(v, pg_catalog.chr(12), '\f');
  v := pg_catalog.replace(v, pg_catalog.chr(13), '\r');
  FOR i IN 1..31 LOOP
    IF i NOT IN (8, 9, 10, 12, 13) THEN
      v := pg_catalog.replace(v, pg_catalog.chr(i), '\u00' || pg_catalog.lpad(pg_catalog.to_hex(i), 2, '0'));
    END IF;
  END LOOP;
  v := pg_catalog.replace(v, pg_catalog.chr(8232), '\u2028');
  v := pg_catalog.replace(v, pg_catalog.chr(8233), '\u2029');
  RETURN '"' || v || '"';
END;
$$;

-- Owner-internal: text safe inside HTML elements and attribute values.
CREATE FUNCTION business.notice_html(p text)
RETURNS text
LANGUAGE sql
IMMUTABLE
STRICT
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.replace(pg_catalog.replace(pg_catalog.replace(pg_catalog.replace(pg_catalog.replace(
           p, '&', '&amp;'), '<', '&lt;'), '>', '&gt;'), '"', '&quot;'), '''', '&#39;');
$$;

-- Owner-internal: pence as £90 (whole pounds) or £1,040.50 (owner data
-- contract: whole pounds without decimals, otherwise two places).
CREATE FUNCTION business.notice_money(p_minor bigint)
RETURNS text
LANGUAGE sql
IMMUTABLE
STRICT
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT '£' || pg_catalog.to_char(p_minor / 100, 'FM999,999,999,990')
         || CASE WHEN p_minor % 100 = 0 THEN '' ELSE '.' || pg_catalog.lpad((p_minor % 100)::text, 2, '0') END;
$$;

-- Owner-internal: London calendar day and clock time (GMT or BST).
CREATE FUNCTION business.notice_day(p_at timestamptz)
RETURNS text
LANGUAGE sql
STABLE
STRICT
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.to_char(p_at AT TIME ZONE 'Europe/London', 'FMDay FMDD FMMonth YYYY');
$$;

CREATE FUNCTION business.notice_time(p_at timestamptz)
RETURNS text
LANGUAGE sql
STABLE
STRICT
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.to_char(p_at AT TIME ZONE 'Europe/London', 'HH24:MI');
$$;

-- Owner-internal: a GK-002 size as customers see it (as the UI shows it).
CREATE FUNCTION business.notice_size(p_code text)
RETURNS text
LANGUAGE sql
IMMUTABLE
STRICT
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT CASE p_code
    WHEN 'mini' THEN 'Mini · 5 inches' WHEN 's' THEN 'S · 6 inches' WHEN 'm' THEN 'M · 7 inches'
    WHEN 'l' THEN 'L · 8 inches' WHEN 'xl' THEN 'XL · 10 inches' WHEN 'xxl' THEN 'XXL · 12 inches'
    WHEN 'tiers_4_7' THEN '2 tiers · 4 + 7 inches' WHEN 'tiers_6_8' THEN '2 tiers · 6 + 8 inches'
    WHEN 'tiers_8_10' THEN '2 tiers · 8 + 10 inches' WHEN 'tiers_4_6_8' THEN '3 tiers · 4 + 6 + 8 inches'
    WHEN 'tiers_6_9_11' THEN '3 tiers · 6 + 9 + 11 inches' ELSE p_code END;
$$;

-- Owner-internal: a stable UUID derived from p (SHA-256 in a version 4
-- layout).
CREATE FUNCTION business.notice_uuid(p text)
RETURNS uuid
LANGUAGE sql
IMMUTABLE
STRICT
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT (pg_catalog.substr(h, 1, 8) || '-' || pg_catalog.substr(h, 9, 4) || '-4' || pg_catalog.substr(h, 14, 3) || '-'
          || pg_catalog.substr('89ab', (pg_catalog.get_byte(pg_catalog.decode(pg_catalog.substr(h, 17, 2), 'hex'), 0) % 4) + 1, 1)
          || pg_catalog.substr(h, 18, 3) || '-' || pg_catalog.substr(h, 21, 12))::uuid
  FROM (SELECT pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(p, 'UTF8')), 'hex') AS h) x;
$$;

-- Owner-internal: an address the dispatcher accepts as one bare recipient
-- (a conservative ASCII dot-atom subset of what notifications.NewNotice
-- accepts).
CREATE FUNCTION business.notice_address_ok(p text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce(octet_length(p) <= 254
         AND p ~ '^[A-Za-z0-9!#$%&''*+/=?^_`{|}~-]+(\.[A-Za-z0-9!#$%&''*+/=?^_`{|}~-]+)*@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$', false);
$$;

-- Owner-internal: subject, plain text and HTML of one order notice. p_facts:
-- for 'cancelled' the secured outcome's evidence (reason, rate, paid_minor,
-- refund_target_minor); for 'refund_returned' target_minor and
-- goal_version; for 'confirmed' and 'paid_awaiting_agreement' the command's
-- received_at; NULL for 'released'. Every input is persisted, so the same
-- transition always renders the same bytes. The copy is the agent's draft for Creator
-- review: factual, no promise the contracts do not make, never "refunded"
-- before a return succeeded.
CREATE FUNCTION business.order_notice_render(p_kind text, p_o business.orders, p_facts json, p_origin text,
                                             OUT subject text, OUT body_text text, OUT body_html text)
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_text text[] := ARRAY[]::text[];
  v_html text[] := ARRAY[]::text[];
  v_p text;
  v_paid bigint := business.order_paid_minor(p_o.id);
  v_free boolean := p_o.payment_requirement = 'none';
  v_link text := p_origin || '/orders/' || p_o.id::text;
  v_when text := business.notice_day(p_o.original_handover_at) || ', ' || business.notice_time(p_o.original_handover_at)
                 || ' to ' || business.notice_time(p_o.handover_ends_at) || ' (London time)';
  v_mode text := CASE p_o.fulfillment_mode WHEN 'collection' THEN 'collection' WHEN 'customer_courier' THEN 'courier collection'
                                           ELSE 'handover' END;
  v_items text;
  v_totals text;
  v_rate integer;
  v_target bigint;
  v_returned boolean;
  v_style constant text := 'margin:0 0 16px';
  v_link_style constant text := 'color:#5b1a2e';
BEGIN
  SELECT pg_catalog.string_agg(
           '- ' || CASE WHEN i.quantity > 1 THEN i.quantity::text || ' × ' ELSE '' END || c.name || ' (' || r.flavour_name
           || ', ' || business.notice_size(r.size_code) || CASE WHEN i.physical_lot_id IS NOT NULL THEN ', ready-made' ELSE '' END
           || '): ' || business.notice_money(i.unit_price_minor) || CASE WHEN i.quantity > 1 THEN ' each' ELSE '' END,
           E'\n' ORDER BY i.line_no)
  INTO v_items
  FROM business.order_item i
  JOIN business.variant va ON va.id = i.variant_id
  JOIN business.cake c ON c.id = va.cake_id
  JOIN business.recipe_version r ON r.id = i.recipe_version_id
  WHERE i.order_id = p_o.id;
  v_totals := CASE WHEN p_o.discount_minor > 0
                   THEN 'Cakes: ' || business.notice_money(p_o.subtotal_minor) || E'\nDiscount: −'
                        || business.notice_money(p_o.discount_minor) || E'\n' ELSE '' END;

  IF p_kind = 'confirmed' THEN
    subject := 'Your GK Cakes order is confirmed';
    v_text := v_text || CASE WHEN v_free THEN 'Thank you. Your order is confirmed. It is free, so there is nothing to pay.'
                             ELSE 'Thank you. Your order is confirmed and we have received your payment of '
                                  || business.notice_money(v_paid) || '.' END;
    v_text := v_text || ('Order reference: ' || p_o.id::text || E'\n' || pg_catalog.upper(pg_catalog.left(v_mode, 1))
                         || pg_catalog.substr(v_mode, 2) || ': ' || v_when);
    v_text := v_text || (E'Your cakes:\n' || coalesce(v_items, '') || E'\n' || v_totals
                         || CASE WHEN v_free THEN 'Total: £0' ELSE 'Total paid: ' || business.notice_money(v_paid) END);
    v_text := v_text || CASE p_o.fulfillment_mode
      WHEN 'collection' THEN 'Collection is from St Katharine Docks, London. The chef will send you the exact address and instructions about two days before.'
      WHEN 'customer_courier' THEN 'Please arrange and pay for your own courier to collect at the time above. The chef will send the exact collection address and instructions about two days before. Once the cake is handed to your courier, it is their responsibility.'
      ELSE 'Delivery by GK Cakes is agreed and paid separately. The chef will contact you about it.' END;
    IF NOT v_free THEN
      -- Against the command's persisted receipt (as the cancellation rate
      -- is), so a replayed transition renders the same bytes.
      v_text := v_text || CASE WHEN p_o.full_refund_deadline_at > (p_facts ->> 'received_at')::timestamptz
        THEN 'If you need to cancel: cancel before ' || business.notice_time(p_o.full_refund_deadline_at) || ' on '
             || business.notice_day(p_o.full_refund_deadline_at)
             || ' (London time) for a full refund of what you paid. From then on, the refund is 50%.'
        ELSE 'If you need to cancel: the refund is now 50% of what you paid.' END;
    END IF;
  ELSIF p_kind = 'paid_awaiting_agreement' THEN
    subject := 'Payment received for your GK Cakes order';
    v_text := v_text || ('We have received your payment of ' || business.notice_money(v_paid)
                         || '. It arrived after the preparation time needed for your chosen date, so your order is not confirmed yet.');
    v_text := v_text || 'The chef will contact you about whether the date can still work. If you do not agree a handover, the full amount you paid will be returned.'::text;
    v_text := v_text || ('Order reference: ' || p_o.id::text || E'\nRequested ' || v_mode || ': ' || v_when);
    v_text := v_text || (E'Your cakes:\n' || coalesce(v_items, '') || E'\n' || v_totals || 'Paid: ' || business.notice_money(v_paid));
  ELSIF p_kind = 'released' THEN
    subject := 'Your GK Cakes order has been released';
    v_text := v_text || CASE WHEN p_o.cancel_reason = 'free_not_confirmed'
      THEN 'Your free order has been released because it was not confirmed in time. The date and cakes are no longer held for you.'
      ELSE 'Your order has been released because its payment was not completed. The date and cakes are no longer held for you.' END;
    IF NOT v_free THEN
      v_text := v_text || 'We have no record of a successful payment for this order. If a payment does arrive later, the full amount will be returned.'::text;
    END IF;
    v_text := v_text || ('Order reference: ' || p_o.id::text || E'\nRequested ' || v_mode || ': ' || v_when);
  ELSIF p_kind = 'cancelled' THEN
    subject := 'Your GK Cakes order is cancelled';
    v_rate := (p_facts ->> 'rate')::integer;
    v_target := coalesce((p_facts ->> 'refund_target_minor')::bigint, 0);
    v_text := v_text || CASE p_facts ->> 'reason'
      WHEN 'chef_cancelled' THEN 'We are sorry: the chef has had to cancel your order.'
      WHEN 'late_payment_returned' THEN 'Your order has been cancelled because a handover could not be agreed after your payment arrived.'
      ELSE 'Your order has been cancelled as you asked.' END;
    v_returned := coalesce((p_facts ->> 'refund_returned')::boolean, false);
    v_text := v_text || CASE
      WHEN coalesce((p_facts ->> 'paid_minor')::bigint, 0) = 0 THEN 'There was no payment for this order, so there is nothing to refund.'
      WHEN v_target = 0 THEN 'No refund is due for this order.'
      ELSE CASE WHEN v_returned THEN 'Refund: ' ELSE 'Refund requested: ' END || business.notice_money(v_target)
           || CASE WHEN v_rate = 100 THEN ', the full amount you paid.'
                   ELSE ', which is ' || v_rate::text || '% of the ' || business.notice_money((p_facts ->> 'paid_minor')::bigint)
                        || ' you paid, because the full-refund deadline had been reached.' END END;
    IF v_target > 0 THEN
      v_text := v_text || CASE WHEN v_returned THEN 'It has already been returned to you.'
                               ELSE 'We have asked for this refund and will email you once it has been returned.' END;
    END IF;
    v_text := v_text || ('Order reference: ' || p_o.id::text || E'\nIt was planned for: ' || v_when);
  ELSIF p_kind = 'refund_returned' THEN
    subject := 'Your GK Cakes refund has been returned';
    v_text := v_text || ('We have returned ' || business.notice_money((p_facts ->> 'target_minor')::bigint)
                         || CASE WHEN (p_facts ->> 'goal_version')::integer > 1 THEN ' in total' ELSE '' END
                         || ' to you for this order. Depending on your bank, it can take a few working days to show on your statement.');
    v_text := v_text || ('Order reference: ' || p_o.id::text);
  ELSE
    RAISE EXCEPTION 'unknown order notice %', p_kind USING ERRCODE = 'invalid_parameter_value';
  END IF;

  -- Paragraphs: plain text separated by blank lines; HTML paragraphs with
  -- line breaks; then the link paragraph and the footer.
  FOREACH v_p IN ARRAY v_text LOOP
    v_html := v_html || ('<p style="' || v_style || '">' || pg_catalog.replace(business.notice_html(v_p), E'\n', '<br>') || '</p>');
  END LOOP;
  -- A released order has no order page paragraph (and no invitation to
  -- order again: no marketing).
  IF p_kind <> 'released' THEN
    v_text := v_text || ('Your order page: ' || v_link
                         || E'\nIf you ordered without signing in, open this link on the same device and browser you ordered with.');
    v_html := v_html || ('<p style="' || v_style || '">Your order page: <a href="' || business.notice_html(v_link)
                         || '" style="' || v_link_style || '">' || business.notice_html(v_link)
                         || '</a><br>If you ordered without signing in, open this link on the same device and browser you ordered with.</p>');
  END IF;
  v_p := E'GK Cakes, London\nYou can reply to this email if you have a question. You are receiving it because of your order with GK Cakes.';
  body_text := E'Hello,\n\n' || pg_catalog.array_to_string(v_text, E'\n\n') || E'\n\n' || v_p || E'\n';
  body_html := '<!DOCTYPE html><html lang="en-GB"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width">'
    || '<title>' || business.notice_html(subject) || '</title></head>'
    || '<body style="margin:0;padding:24px;background:#ffffff;color:#141416;font-family:Helvetica,Arial,sans-serif;font-size:16px;line-height:1.5">'
    || '<div style="max-width:560px;margin:0 auto">'
    || '<p style="margin:0 0 24px;font-family:Georgia,serif;font-size:22px;color:#5b1a2e">GK Cakes</p>'
    || '<p style="' || v_style || '">Hello,</p>' || pg_catalog.array_to_string(v_html, '')
    || '<p style="margin:32px 0 0;font-size:13px;color:#55585e">' || pg_catalog.replace(business.notice_html(v_p), E'\n', '<br>')
    || '</p></div></body></html>';
END;
$$;

-- Owner-internal: render and enqueue the order notice (p_kind, p_dedupe)
-- once, inside the caller's transaction. Returns the outcome, or 'exists'
-- when it was handled before. The notice itself never raises.
CREATE FUNCTION business.order_notice_enqueue(p_order uuid, p_kind text, p_dedupe text, p_transition uuid, p_facts json)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_s business.notice_settings%ROWTYPE;
  v_o business.orders%ROWTYPE;
  v_rev text := CASE p_kind WHEN 'confirmed' THEN 'order-confirmed.v1' WHEN 'paid_awaiting_agreement' THEN 'order-paid-late.v1'
                            WHEN 'released' THEN 'order-released.v1' WHEN 'cancelled' THEN 'order-cancelled.v1'
                            WHEN 'refund_returned' THEN 'refund-returned.v1' END;
  v_name text := p_order::text || ':' || p_kind || ':' || p_dedupe;
  v_id uuid;
  v_key uuid;
  v_subject text;
  v_text text;
  v_html text;
  v_payload text;
  v_outcome text;
  v_detail text;
BEGIN
  PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('gk.order_notice:' || v_name, 0));
  IF EXISTS (SELECT 1 FROM business.order_notice n WHERE n.order_id = p_order AND n.kind = p_kind AND n.dedupe = p_dedupe) THEN
    RETURN 'exists';
  END IF;
  SELECT * INTO v_s FROM business.notice_settings s WHERE s.singleton;
  SELECT * INTO v_o FROM business.orders o WHERE o.id = p_order;
  IF v_s.singleton IS NULL THEN
    v_outcome := 'not_configured';
  ELSIF NOT business.notice_address_ok(v_o.contact_email) THEN
    v_outcome := 'not_queued';
    v_detail := 'recipient';
  ELSIF NOT business.notice_recipient_allowed(v_s, v_o.contact_email) THEN
    -- Outside production, a recipient off the allowlist is never queued, so
    -- no worker build (old, failed or rolled back) can send to it.
    v_outcome := 'not_queued';
    v_detail := 'allowlist';
  ELSE
    v_id := business.notice_uuid(v_s.environment || ':order_notice:intent:' || v_name);
    v_key := business.notice_uuid(v_s.environment || ':order_notice:key:' || v_name);
    BEGIN
      SELECT r.subject, r.body_text, r.body_html INTO v_subject, v_text, v_html
      FROM business.order_notice_render(p_kind, v_o, p_facts, v_s.ui_origin) r;
      -- notifications payload and Request, field for field (canonical form).
      v_payload := '{"provider":"resend","environment":' || business.notice_json(v_s.environment)
        || ',"idempotency_key":' || business.notice_json(v_key::text)
        || ',"request":{"from":' || business.notice_json('GK Cakes <orders@gkcake.com>')
        || ',"to":[' || business.notice_json(v_o.contact_email) || ']'
        || ',"reply_to":[' || business.notice_json('gk.cake.london@gmail.com') || ']'
        || ',"subject":' || business.notice_json(v_subject)
        || ',"text":' || business.notice_json(v_text)
        || ',"html":' || business.notice_json(v_html)
        || ',"tags":[{"name":"intent","value":' || business.notice_json(v_id::text) || '},{"name":"env","value":'
        || business.notice_json(v_s.environment) || '}]}}';
      PERFORM business.notification_enqueue(v_id, p_transition, p_order, v_rev, v_key, v_payload::json, 'system', NULL);
      v_outcome := 'queued';
    EXCEPTION WHEN OTHERS THEN
      v_outcome := 'not_queued';
      v_detail := pg_catalog.lower(SQLSTATE);
    END;
  END IF;
  INSERT INTO business.order_notice (id, order_id, kind, dedupe, transition_id, template_revision, outcome, intent_id, detail)
  VALUES (pg_catalog.gen_random_uuid(), p_order, p_kind, p_dedupe, p_transition, v_rev, v_outcome,
          CASE WHEN v_outcome = 'queued' THEN v_id END, v_detail)
  -- A concurrent transaction that recorded it first (the advisory lock
  -- makes this unlikely) must not fail this transition at commit.
  ON CONFLICT ON CONSTRAINT order_notice_once DO NOTHING;
  IF v_outcome <> 'queued' THEN
    -- Visible to staff (the order's audit summary; attention flag
    -- notice_not_sent), without personal data.
    PERFORM business.audit_record('system', NULL, 'order.notice_not_queued', 'order', p_order, p_transition,
                                  pg_catalog.json_build_object('kind', p_kind, 'outcome', v_outcome, 'detail', v_detail));
  END IF;
  RETURN v_outcome;
END;
$$;

-- As in 0022, then the confirmed / payment-received / cancelled notice in
-- the same transaction, only when this call secured the order.
CREATE OR REPLACE FUNCTION business.order_command_secured(p_job_id uuid, p_worker uuid, p_generation bigint, p_operation_id uuid)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.order_command := business.order_command_leased_operation(p_job_id, p_worker, p_generation, p_operation_id);
  v_state text;
  v_status text;
  v_evidence json;
BEGIN
  PERFORM 1 FROM business.orders o WHERE o.id = v.order_id FOR UPDATE;
  v_state := business.recovery_mark_secured(v.id);
  SELECT r.outcome ->> 'status', r.outcome -> 'evidence' INTO v_status, v_evidence
  FROM business.recovery_operation r WHERE r.id = v.id;
  UPDATE business.orders o SET recovery_state = 'secured', updated_at = pg_catalog.now()
  WHERE o.id = v.order_id
    AND ((o.recovery_state = 'confirmation_pending' AND v.command <> 'cancel'
          AND v_status IN ('confirmed', 'paid_awaiting_agreement'))
         OR (o.recovery_state = 'cancellation_pending' AND v.command IN ('cancel', 'late_payment')
             AND v_status = 'cancelled'));
  IF FOUND THEN
    -- A cancellation's refund may already have succeeded before the order
    -- was secured: the email then says so instead of "requested".
    PERFORM business.order_notice_enqueue(v.order_id, v_status, v.id::text, v.id,
      CASE WHEN v_status = 'cancelled'
           THEN (v_evidence::jsonb || pg_catalog.jsonb_build_object('refund_returned',
                   coalesce(business.order_refund_summary(v.order_id) ->> 'state' = 'returned', false)))::json
           ELSE pg_catalog.json_build_object('received_at', v.received_at) END);
  END IF;
  RETURN v_state;
END;
$$;

-- Released: the hold review cancelled an abandoned order (0015).
CREATE FUNCTION business.order_notice_released()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM business.payment_collection c WHERE c.order_id = NEW.id AND c.dispatch_started_at IS NOT NULL)
     OR EXISTS (SELECT 1 FROM business.order_command c WHERE c.order_id = NEW.id AND c.command = 'free_confirm') THEN
    PERFORM business.order_notice_enqueue(NEW.id, 'released', 'released', business.notice_uuid('order_released:' || NEW.id::text), NULL);
  END IF;
  RETURN NULL;
END;
$$;

CREATE TRIGGER orders_notice_released AFTER UPDATE OF lifecycle ON business.orders
  FOR EACH ROW WHEN (OLD.lifecycle = 'awaiting_payment' AND NEW.lifecycle = 'cancelled'
                     AND NEW.cancel_reason IN ('payment_not_completed', 'free_not_confirmed'))
  EXECUTE FUNCTION business.order_notice_released();

-- Refund returned: checked at commit for the order.
CREATE FUNCTION business.order_notice_refund_check(p_order uuid)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v json;
BEGIN
  IF p_order IS NULL OR NOT EXISTS (SELECT 1 FROM business.orders o
                                    WHERE o.id = p_order AND o.recovery_state <> 'cancellation_pending') THEN
    RETURN;
  END IF;
  v := business.order_refund_summary(p_order);
  IF v ->> 'state' = 'returned' AND (v ->> 'target_minor')::bigint > 0 THEN
    PERFORM business.order_notice_enqueue(p_order, 'refund_returned', v ->> 'goal_version',
              business.notice_uuid('refund_returned:' || p_order::text || ':' || (v ->> 'goal_version')),
              pg_catalog.json_build_object('target_minor', (v ->> 'target_minor')::bigint,
                                           'goal_version', (v ->> 'goal_version')::integer));
  END IF;
END;
$$;

CREATE FUNCTION business.order_notice_refund_trigger()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF TG_TABLE_NAME = 'orders' THEN
    PERFORM business.order_notice_refund_check(NEW.id);
  ELSIF TG_TABLE_NAME = 'recovery_operation' THEN
    PERFORM business.order_notice_refund_check(NEW.aggregate_id);
  ELSIF TG_TABLE_NAME = 'refund_observation' THEN
    PERFORM business.order_notice_refund_check(c.order_id)
    FROM business.payment_collection c WHERE c.id = NEW.collection_id;
  ELSE
    PERFORM business.order_notice_refund_check(NEW.order_id);
  END IF;
  RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER refund_operation_notice AFTER INSERT OR UPDATE ON business.refund_operation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION business.order_notice_refund_trigger();
CREATE CONSTRAINT TRIGGER refund_observation_notice AFTER INSERT OR UPDATE ON business.refund_observation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION business.order_notice_refund_trigger();
CREATE CONSTRAINT TRIGGER refund_goal_notice AFTER INSERT OR UPDATE ON business.refund_goal
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION business.order_notice_refund_trigger();
CREATE CONSTRAINT TRIGGER orders_refund_notice AFTER UPDATE OF recovery_state ON business.orders
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
  WHEN (OLD.recovery_state = 'cancellation_pending' AND NEW.recovery_state = 'secured')
  EXECUTE FUNCTION business.order_notice_refund_trigger();
-- A secured order command (for example a published manual payout outcome).
CREATE CONSTRAINT TRIGGER recovery_operation_refund_notice AFTER UPDATE OF state ON business.recovery_operation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
  WHEN (NEW.kind = 'command' AND NEW.state = 'secured' AND OLD.state IS DISTINCT FROM 'secured')
  EXECUTE FUNCTION business.order_notice_refund_trigger();

-- 3. The staging allowlist suppression (see the header).
CREATE FUNCTION business.notification_dispatch_suppress(p_job_id uuid, p_worker uuid, p_generation bigint)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v business.notification_intent := business.notification_leased_intent(p_job_id, p_worker, p_generation);
  v_frozen boolean;
  v_result text;
BEGIN
  IF v.id IS NULL THEN
    RAISE EXCEPTION 'job % is not a notification job', p_job_id USING ERRCODE = 'invalid_parameter_value';
  END IF;
  -- Freeze first (0020): the leased rows are locked, then the freeze row.
  SELECT f.frozen INTO v_frozen FROM business.recovery_freeze f WHERE f.singleton FOR SHARE;
  IF v_frozen THEN
    RETURN 'frozen';
  ELSIF v.suspended_reason IS NOT NULL THEN
    RETURN 'already';
  ELSIF v.state NOT IN ('queued', 'sending', 'unknown') THEN
    RETURN 'settled';
  END IF;
  v_result := CASE WHEN v.state = 'queued'
                        AND NOT EXISTS (SELECT 1 FROM business.notification_attempt a WHERE a.intent_id = v.id)
                   THEN 'suppressed' ELSE 'suppressed_after_dispatch' END;
  UPDATE business.notification_intent
  SET suspended_reason = 'recipient not on this environment''s email allowlist (EMAIL_RECIPIENT_ALLOWLIST): not sent',
      version = version + 1, updated_at = pg_catalog.now()
  WHERE id = v.id;
  PERFORM business.audit_record('system', NULL, 'notification.suppressed', 'notification_intent', v.id, v.transition_id,
                                pg_catalog.json_build_object('intent_state', v.state, 'result', v_result));
  RETURN v_result;
END;
$$;

-- 4. As in 0029, plus 'notice_not_sent': an order notice was not queued
-- because the stored address cannot be sent to or rendering failed, or
-- because the settings were missing at the time although they exist now (a
-- deploy window). The staging allowlist and a database whose API never
-- recorded settings (local tooling, tests) are deliberate, not flagged;
-- every outcome other than queued is audited ('order.notice_not_queued').
CREATE OR REPLACE FUNCTION business.order_attention_flags(p_order business.orders)
RETURNS text[]
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT pg_catalog.array_remove(ARRAY[
           CASE WHEN business.order_refund_summary(p_order.id) ->> 'state' = 'needs_attention'
                THEN 'refund_needs_attention' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.payment_collection pc
                             WHERE pc.order_id = p_order.id AND pc.state = 'unknown')
                THEN 'payment_unknown' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.payment_collection pc
                             WHERE pc.order_id = p_order.id AND pc.mismatch_reason IS NOT NULL)
                THEN 'payment_mismatch' END,
           CASE WHEN p_order.lifecycle = 'paid_awaiting_agreement' THEN 'paid_awaiting_agreement' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.refund_observation b
                             JOIN business.payment_collection pc ON pc.id = b.collection_id
                             WHERE pc.order_id = p_order.id AND b.quarantined AND b.resolved_at IS NULL)
                THEN 'refund_quarantine' END,
           CASE WHEN EXISTS (SELECT 1 FROM business.order_notice n
                             WHERE n.order_id = p_order.id
                               AND ((n.outcome = 'not_queued' AND n.detail <> 'allowlist')
                                    OR (n.outcome = 'not_configured'
                                        AND EXISTS (SELECT 1 FROM business.notice_settings s WHERE s.singleton))))
                THEN 'notice_not_sent' END
         ]::text[], NULL);
$$;

REVOKE ALL ON TABLE business.notice_settings FROM PUBLIC;
REVOKE ALL ON TABLE business.order_notice FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notice_settings_record(text, text, text[]) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notice_recipient_allowed(business.notice_settings, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notice_json(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notice_html(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notice_money(bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notice_day(timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notice_time(timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notice_size(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notice_uuid(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notice_address_ok(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_notice_render(text, business.orders, json, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_notice_enqueue(uuid, text, text, uuid, json) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_notice_released() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_notice_refund_check(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION business.order_notice_refund_trigger() FROM PUBLIC;
REVOKE ALL ON FUNCTION business.notification_dispatch_suppress(uuid, uuid, bigint) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION business.notice_settings_record(text, text, text[]) TO gk_api;
GRANT EXECUTE ON FUNCTION business.notification_dispatch_suppress(uuid, uuid, bigint) TO gk_worker;
