-- GK-022 carried review items (catalog text and snapshot columns).
--
-- Catalog free text. The API refuses every control character (Unicode Cc,
-- including the C1 range U+0080..U+009F) and the line and paragraph
-- separators U+2028/U+2029 in catalog text; the database checks used
-- [[:cntrl:]] (locale-dependent) or an ASCII-only class. These constraints
-- refuse the same set in the database whatever the locale: single-line
-- columns refuse U+0001..U+001F, U+007F..U+009F, U+2028 and U+2029;
-- multi-line columns (the allergen notice, a cake description) allow only
-- the line feed of those. The gateway functions already turn a
-- check_violation into GK422, so a bypassing caller gets the same refusal.
--
-- Snapshot columns. A recipe version copies flavour_name, size_code and
-- stock_mode from its variant and flavour at creation; they now carry the
-- source columns' rules (trimmed 1..80 characters, the GK-002 size codes,
-- the stock modes). Its composition is a one-dimensional array without
-- NULLs, as catalog_recipe_version_create requires. A quote's copied slot
-- ends after it starts and names a schedule revision (>= 1); an order's
-- copied handover window ends after it starts.
--
-- Expand-only (K09): constraints only; no column, function or grant
-- changes. Every row written by the gateway functions already complies, so
-- each constraint is added NOT VALID and then validated (a failing row stops
-- the migration instead of being changed).

SET LOCAL ROLE gk_owner;

-- ------------------------------------------------------------ catalog text

ALTER TABLE business.flavour
  ADD CONSTRAINT flavour_name_text CHECK (name !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]') NOT VALID;
ALTER TABLE business.flavour VALIDATE CONSTRAINT flavour_name_text;

ALTER TABLE business.allergen_notice
  ADD CONSTRAINT allergen_notice_body_text CHECK (body !~ '[\x01-\x09\x0b-\x1f\x7f-\x9f\u2028\u2029]') NOT VALID;
ALTER TABLE business.allergen_notice VALIDATE CONSTRAINT allergen_notice_body_text;

ALTER TABLE business.cake
  ADD CONSTRAINT cake_name_text CHECK (name !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]') NOT VALID,
  ADD CONSTRAINT cake_description_text CHECK (description !~ '[\x01-\x09\x0b-\x1f\x7f-\x9f\u2028\u2029]') NOT VALID;
ALTER TABLE business.cake VALIDATE CONSTRAINT cake_name_text;
ALTER TABLE business.cake VALIDATE CONSTRAINT cake_description_text;

-- Composition items are single-line; they are joined with a space (never a
-- refused character) for the check.
ALTER TABLE business.recipe_version
  ADD CONSTRAINT recipe_version_coating_text CHECK (coating !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]') NOT VALID,
  ADD CONSTRAINT recipe_version_composition_text
    CHECK (pg_catalog.array_to_string(composition, ' ') !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]') NOT VALID;
ALTER TABLE business.recipe_version VALIDATE CONSTRAINT recipe_version_coating_text;
ALTER TABLE business.recipe_version VALIDATE CONSTRAINT recipe_version_composition_text;

ALTER TABLE business.recipe_allergen
  ADD CONSTRAINT recipe_allergen_label_text CHECK (label !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]') NOT VALID;
ALTER TABLE business.recipe_allergen VALIDATE CONSTRAINT recipe_allergen_label_text;

ALTER TABLE business.recipe_line
  ADD CONSTRAINT recipe_line_component_text CHECK (component !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]') NOT VALID;
ALTER TABLE business.recipe_line VALIDATE CONSTRAINT recipe_line_component_text;

-- -------------------------------------------------------- snapshot columns

ALTER TABLE business.recipe_version
  ADD CONSTRAINT recipe_version_flavour_name
    CHECK (flavour_name = pg_catalog.btrim(flavour_name) AND pg_catalog.char_length(flavour_name) BETWEEN 1 AND 80
           AND flavour_name !~ '[\x01-\x1f\x7f-\x9f\u2028\u2029]') NOT VALID,
  ADD CONSTRAINT recipe_version_size_code CHECK (size_code IN (
    'mini', 's', 'm', 'l', 'xl', 'xxl',
    'tiers_4_7', 'tiers_6_8', 'tiers_8_10', 'tiers_4_6_8', 'tiers_6_9_11')) NOT VALID,
  ADD CONSTRAINT recipe_version_stock_mode CHECK (stock_mode IN ('made_to_order', 'ready_stock')) NOT VALID,
  -- CASE: array_position refuses a multi-dimensional array.
  ADD CONSTRAINT recipe_version_composition_shape
    CHECK (CASE WHEN pg_catalog.array_ndims(composition) = 1
                THEN pg_catalog.array_position(composition, NULL) IS NULL
                ELSE false END) NOT VALID;
ALTER TABLE business.recipe_version VALIDATE CONSTRAINT recipe_version_flavour_name;
ALTER TABLE business.recipe_version VALIDATE CONSTRAINT recipe_version_size_code;
ALTER TABLE business.recipe_version VALIDATE CONSTRAINT recipe_version_stock_mode;
ALTER TABLE business.recipe_version VALIDATE CONSTRAINT recipe_version_composition_shape;

ALTER TABLE business.checkout_quote
  ADD CONSTRAINT checkout_quote_slot CHECK (slot_ends_at > slot_starts_at AND schedule_revision >= 1) NOT VALID;
ALTER TABLE business.checkout_quote VALIDATE CONSTRAINT checkout_quote_slot;

ALTER TABLE business.orders
  ADD CONSTRAINT orders_handover_window CHECK (handover_ends_at > original_handover_at) NOT VALID;
ALTER TABLE business.orders VALIDATE CONSTRAINT orders_handover_window;
