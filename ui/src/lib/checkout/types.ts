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
	| 'cancelled';

/**
 * A refund as the API will report it (full-return work, A3c2). Not served
 * yet; the page shows only what this block says.
 */
export interface Refund {
	state: string;
	amount_minor?: number;
	currency?: string;
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
}
