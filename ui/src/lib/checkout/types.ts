// API shapes used by checkout and the order page (GK-025). Field names follow
// the Go handlers and the SQL views (business.quote_get, business.order_get);
// money is always integer pence from the server and never computed here.

export type FulfillmentMode = 'collection' | 'customer_courier' | 'gk_delivery';

export interface Allergen {
	code: string;
	label: string;
	prominent: boolean;
}

export interface Recipe {
	recipe_version_id: string;
	price_minor: number;
	currency: string;
	servings: { min: number; max: number };
	coating: string;
	composition: string[];
	allergens: Allergen[];
	allergen_notice: { id: string; text: string };
}

export interface Size {
	code: string;
	label: string;
	diameter: string;
}

export interface Variant {
	id: string;
	flavour: string;
	size: Size;
	stock_mode: string;
	purchasable: boolean;
	recipe: Recipe | null;
}

export interface Cake {
	id: string;
	name: string;
	description: string;
	variants: Variant[];
}

export interface CakePage {
	cakes: Cake[];
	next_cursor?: string;
}

export interface ReadyCake {
	id: string;
	kind: string;
	cake_id: string;
	cake_name: string;
	variant_id: string;
	recipe_version_id: string;
	flavour: string;
	size: Size;
	price_minor: number;
	currency: string;
	available: number;
	expires_at: string;
}

export interface Day {
	date: string;
	status: 'open' | 'full' | 'lead_time' | 'closed' | 'outside_horizon' | 'no_times';
	remaining_places: number | null;
}

export interface Availability {
	timezone: string;
	as_of: string;
	schedule_version: number | null;
	days: Day[];
}

export interface Slot {
	id: string;
	starts_at: string;
	ends_at: string;
	availability: 'available' | 'requestable' | 'unavailable';
	reason_code: string | null;
}

export interface Choices {
	date: string;
	fulfillment_mode: FulfillmentMode;
	day_status: string;
	slots: Slot[];
}

export interface Policy {
	id: string;
	summary: string;
	effective_at: string;
	source_decision_id: string;
}

export interface QuoteLine {
	line_no: number;
	variant_id: string;
	lot_id: string | null;
	recipe_version_id: string;
	cake_id: string;
	cake_name: string;
	flavour: string;
	size_code: string;
	quantity: number;
	unit_price_minor: number;
	discount_minor: number;
	net_minor: number;
}

export interface Quote {
	id: string;
	version: number;
	sent_at: string;
	expires_at: string;
	date: string;
	slot: { id: string; starts_at: string; ends_at: string };
	fulfillment_mode: FulfillmentMode;
	subtotal_minor: number;
	discount_minor: number;
	total_minor: number;
	currency: string;
	discount: { code: string; kind: string; basis_points: number | null } | null;
	lines: QuoteLine[];
}

/** 202 answers of the order commands (K00 async). */
export interface Accepted {
	order_id: string;
	operation_id?: string;
	state: string;
	poll_after_seconds: number;
}

export interface OrderItem {
	line_no: number;
	variant_id: string;
	lot_id: string | null;
	cake_name: string;
	flavour: string;
	size_code: string;
	quantity: number;
	unit_price_minor: number;
	discount_minor: number;
	net_paid_minor: number;
}

export type PaymentState = 'created' | 'pending' | 'unknown' | 'succeeded' | 'failed' | 'cancelled';

export interface Payment {
	id: string;
	state: PaymentState;
	amount_minor: number;
	currency: string;
	requested_at: string;
	checkout_url: string | null;
	expires_at: string | null;
}

export type Lifecycle =
	| 'awaiting_payment'
	| 'confirmation_pending'
	| 'paid_awaiting_agreement'
	| 'confirmed'
	| 'in_production'
	| 'ready'
	| 'completed'
	| 'cancelled'
	// A cancellation is being secured (K11): no cancel_reason or refund yet.
	| 'cancellation_pending';

/**
 * The order's refund (full-return core, GK-025 A3c2, business.order_get):
 * the amount to return and how much has verifiably been returned.
 */
export interface Refund {
	state: 'requested' | 'pending' | 'partly_returned' | 'returned' | 'needs_attention' | string;
	target_minor: number;
	succeeded_minor: number;
	in_progress_minor?: number;
	currency?: string;
	requested_at?: string;
	/** The goal's reason, e.g. customer_cancelled, chef_cancelled, delivery_not_possible. */
	reason?: string;
	policy_rate?: number | null;
	/**
	 * A GK delivery's share the chef sends separately (K05: delivery refunds
	 * are the chef's), not yet reserved. Its own line on the order page.
	 */
	delivery_pending_minor?: number;
}

/**
 * The latest cancellation command (K05, 0021): processing until secured,
 * then cancelled (with the rate and amount) or refused.
 */
export interface Cancellation {
	state: 'processing' | 'cancelled' | 'refused' | string;
	reason: string | null;
	rate: number | null;
	amount_minor: number | null;
	/** A stray delivery payment returned in full beside amount_minor (0035). */
	unmatched_minor?: number;
	requested_by: 'customer' | 'chef' | string;
	requested_at: string;
}

/**
 * POST /orders/{id}/cancellation-preview (K05): the database's amount at
 * its clock. The page shows amount_minor as it is and never computes it.
 */
export interface CancellationPreview {
	preview_id: string;
	order_id: string;
	order_version: number;
	rate: number;
	paid_minor: number;
	amount_minor: number;
	currency: string;
	allocations: { collection_id: string; amount_minor: number }[];
	full_refund_deadline_at: string;
	computed_at: string;
	expires_at: string;
	preview_hash: string;
	/** The chef's separate delivery share of amount_minor (not automatic). */
	delivery_pending_minor: number;
	/** A stray delivery payment returned in full beside amount_minor. */
	unmatched_minor: number;
}

/** The latest pickup move or chef appointment change (0031). */
export interface AppointmentChange {
	kind: 'pickup_move' | 'appointment_change' | string;
	state: 'processing' | 'moved' | 'refused' | string;
	reason: string | null;
	requested_by: 'customer' | 'chef' | string;
	requested_at: string;
	to: { date: string; id: string; starts_at: string; ends_at: string };
}

/** Fulfilment (0032): awaiting_handover, out_for_delivery, completed, exception, no_show. */
export interface Fulfillment {
	state: string | null;
	at: string | null;
	pending: 'handover' | 'no_show' | string | null;
}

/** GET /schedule?purpose=pickup_move&order_id=… (0031): the same-day move choices. */
export interface MoveChoices {
	schedule_version: number | null;
	as_of: string;
	timezone: string;
	date: string;
	fulfillment_mode: FulfillmentMode;
	/** open, too_late, pending, closed or not_eligible. */
	day_status: string;
	purpose: 'pickup_move';
	order_id: string;
	current_slot: { id: string; starts_at: string; ends_at: string };
	move_until: string | null;
	slots: Slot[];
}

export interface Order {
	id: string;
	version: number;
	lifecycle: Lifecycle;
	cancel_reason: string | null;
	cancelled_at: string | null;
	payment_requirement: 'full' | 'none';
	placed_at: string;
	date: string;
	fulfillment_mode: FulfillmentMode;
	slot: { id: string; starts_at: string; ends_at: string };
	full_refund_deadline_at: string;
	customer_full_name: string;
	different_payer_name: string | null;
	contact_email: string;
	subtotal_minor: number;
	discount_minor: number;
	total_minor: number;
	currency: string;
	paid_at: string | null;
	free_confirmed_at: string | null;
	free_confirmation: { state: string; reason: string | null; requested_at: string } | null;
	payment: Payment | null;
	items: OrderItem[] | null;
	refund?: Refund | null;
	cancellation?: Cancellation | null;
	appointment_change?: AppointmentChange | null;
	/** Collection and own courier: the current start minus 6 hours (K06). */
	pickup_move_until?: string | null;
	fulfillment?: Fulfillment | null;
	/** GK delivery only (0035); only the fields this page reads. */
	delivery?: { state: string; paid_minor?: number } | null;
	/** GK delivery only (0036): a protected change of the order is unresolved. */
	delivery_choice?: { open: boolean; pending: boolean } | null;
}
