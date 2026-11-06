// The chef's order page (Creator-approved GK-018 r03b2 sketch, order.html;
// approved on the GK-018 card 2026-11-05): GET /api/v1/admin/orders/{id}
// (api/README "Staff order reads" and the blocks added since) for chef,
// technical support and fulfilment, and the day-to-day steps the API allows:
// start production and the fulfilment transitions. Redaction is the API's
// (hidden keys are absent): money only for the chef, no contact details or
// money for technical support, no money, attention or commands for
// fulfilment. Amounts are the server's, only formatted here. Eligibility
// mirrors the README's tables at the page's clock; the API still decides.
import { ApiError, apiGet, apiPost } from '../api';
import { formatMoney, londonTime, sizeLabel, timeRange } from '../checkout/format';
import type { StaffApi } from './catalogue';
import {
	MODES,
	statusOf,
	type Production,
	type Stage,
	type StaffOrderItem,
	type StaffOrderRow,
	type StaffRefund,
	type StaffView
} from './orders';

// ------------------------------------------------------------ the read

export interface Slot {
	id: string;
	starts_at: string;
	ends_at: string;
}

/** Fulfilment (0032): awaiting_handover, out_for_delivery, completed, exception, no_show. */
export interface Fulfillment {
	state: string | null;
	at: string | null;
	pending: 'handover' | 'no_show' | string | null;
}

export interface FulfillmentEvent {
	action: string;
	from: string | null;
	to: string;
	received_at: string;
	command_id: string | null;
	state: 'done' | 'processing' | string;
	/** Chef and fulfilment only. */
	reason?: string | null;
}

export interface AppointmentEvent {
	command_id: string;
	kind: 'pickup_move' | 'appointment_change' | string;
	requested_by: 'customer' | 'chef' | string;
	received_at: string;
	state: 'processing' | 'moved' | 'refused' | string;
	reason: string | null;
	from: Slot & { date: string };
	to: Slot & { date: string };
}

export interface StaffCommand {
	id: string;
	kind: string;
	requested_by: string;
	received_at: string;
	/** The recovery record: anything but secured is unresolved (K11). */
	state: string;
	outcome_status: string | null;
	decision: string | null;
}

export interface StaffPayment {
	id: string;
	state: 'created' | 'pending' | 'unknown' | 'succeeded' | 'failed' | 'cancelled' | string;
	/** Chef only. */
	amount_minor?: number;
	currency?: string;
	requested_at: string;
	expires_at: string | null;
}

export interface RefundPart {
	kind: 'cake' | 'delivery' | 'stray' | string;
	target_minor: number;
	succeeded_minor: number;
	in_progress_minor: number;
	pending_chef_minor: number;
}

export interface DetailRefund extends StaffRefund {
	requested_at?: string;
	reason?: string;
	delivery_pending_minor?: number;
	parts?: RefundPart[];
}

export interface Cancellation {
	state: 'processing' | 'cancelled' | 'refused' | string;
	reason: string | null;
	requested_by: 'customer' | 'chef' | string;
	requested_at: string;
	/** Chef only. */
	amount_minor?: number | null;
	rate?: number | null;
}

export interface StaffDelivery {
	state: string;
	quote?: {
		id: string;
		quoted_at: string;
		amount_minor?: number;
		currency?: string;
		pay_by?: string;
	} | null;
	paid_minor?: number;
	waived?: { at: string };
}

export interface DeliveryChoice {
	open: boolean;
	reason: 'not_possible' | 'pay_by_passed' | string | null;
	pending: boolean;
	chosen?: { choice: string; state: string } | null;
	not_possible?: { at: string } | null;
}

export interface DeliveryFailure {
	cause: 'our_side' | 'nobody_available' | string;
	at: string;
	state: string;
	open: boolean;
	pending: boolean;
	chosen?: { choice: string; state: string } | null;
	redelivery_time_set?: boolean;
}

export interface LatePayment {
	state: 'waiting' | 'offered' | 'processing' | string;
	offer: { date: string; start: string; end: string; accept_until: string } | null;
	paid_minor?: number;
	currency?: string;
}

export interface RefundObservation {
	quarantined: boolean;
	resolved_at: string | null;
	observed_at: string;
	amount_minor?: number;
	currency?: string;
}

/** GET /api/v1/admin/orders/{id} for the caller's view; keys a view lacks are absent. */
export interface StaffOrderDetail {
	id: string;
	version: number;
	lifecycle: string;
	raw_lifecycle: string;
	cancel_reason: string | null;
	cancelled_at: string | null;
	placed_at: string;
	date: string;
	fulfillment_mode: string;
	slot: Slot;
	customer_full_name: string;
	items: StaffOrderItem[] | null;
	production: Production;
	appointment_change?: {
		state: string;
		requested_by: string;
		to: Slot & { date: string };
	} | null;
	appointment_history?: AppointmentEvent[];
	fulfillment?: Fulfillment | null;
	fulfillment_history?: FulfillmentEvent[];
	delivery?: StaffDelivery | null;
	delivery_choice?: DeliveryChoice | null;
	delivery_failure?: DeliveryFailure | null;
	// chef and technical support
	recovery_state?: string;
	payment_requirement?: 'full' | 'none';
	full_refund_deadline_at?: string;
	paid_at?: string | null;
	free_confirmed_at?: string | null;
	free_confirmation?: { state: string } | null;
	cancellation?: Cancellation | null;
	payment?: StaffPayment | null;
	refund?: DetailRefund | null;
	attention?: string[];
	needs_attention?: boolean;
	commands?: StaffCommand[];
	late_payment?: LatePayment | null;
	// chef only
	contact_email?: string;
	subtotal_minor?: number;
	discount_minor?: number;
	total_minor?: number;
	currency?: string;
	refund_goal?: { remaining_minor: number; currency: string } | null;
	refund_observations?: RefundObservation[];
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/** The order ID from the address (lower case), or null when it isn't one. */
export function orderIdFrom(param: string): string | null {
	const id = param.trim().toLowerCase();
	return UUID.test(id) ? id : null;
}

/** One order for the caller's view (404 when unknown or outside fulfilment's scope). */
export function staffOrder(api: StaffApi, id: string): Promise<StaffOrderDetail> {
	const clean = orderIdFrom(id);
	if (!clean) throw new Error('not an order ID');
	return apiGet<StaffOrderDetail>(`/api/v1/admin/orders/${clean}`, {
		baseUrl: api.baseUrl,
		token: api.token,
		fetch: api.fetch
	});
}

// --------------------------------------------------------- formatting

function parts(iso: string, options: Intl.DateTimeFormatOptions): Record<string, string> {
	const out: Record<string, string> = {};
	for (const p of new Intl.DateTimeFormat('en-GB', {
		timeZone: 'Europe/London',
		...options
	}).formatToParts(new Date(iso))) {
		out[p.type] = p.value;
	}
	return out;
}

/** A moment as the sketch's "Thu 22 Oct, 10:20" (London time). */
export function when(iso: string): string {
	const p = parts(iso, { weekday: 'short', day: 'numeric', month: 'short' });
	return `${p.weekday} ${p.day} ${p.month}, ${londonTime(iso)}`;
}

/** A London calendar day as the sketch's "Friday 30 October". */
export function dayLabel(date: string): string {
	const p = parts(`${date}T12:00:00Z`, { weekday: 'long', day: 'numeric', month: 'long' });
	return `${p.weekday} ${p.day} ${p.month}`;
}

/** "Fri 30 Oct". */
export function dayShort(date: string): string {
	const p = parts(`${date}T12:00:00Z`, { weekday: 'short', day: 'numeric', month: 'short' });
	return `${p.weekday} ${p.day} ${p.month}`;
}

const money = (o: StaffOrderDetail, minor: number) => formatMoney(minor, o.currency ?? 'GBP');

/** The page title: each line's cake (with quantity when more than one). */
export function cakeTitle(o: StaffOrderDetail): string {
	const items = o.items ?? [];
	if (!items.length) return 'Order';
	return items
		.map((i) => (i.quantity > 1 ? `${i.quantity} × ${i.cake_name}` : i.cake_name))
		.join(', ');
}

/** A line's details: flavour · size (· quantity). */
export function itemDetail(i: StaffOrderItem): string {
	return [i.flavour, sizeLabel(i.size_code), i.quantity > 1 ? `${i.quantity} cakes` : '']
		.filter(Boolean)
		.join(' · ');
}

export function modeText(mode: string): string {
	return MODES[mode] ?? mode;
}

/** "Order 1a2b3c4d" (the API has no order number yet). */
export function orderLabel(o: StaffOrderDetail): string {
	return `Order ${o.id.slice(0, 8)}`;
}

// ------------------------------------------------------ state reading

/** The fulfilment state, NULL reading awaiting_handover while confirmed, in production or ready. */
export function fulfillmentState(o: StaffOrderDetail): string | null {
	const s = o.fulfillment?.state;
	if (s) return s;
	return ['confirmed', 'in_production', 'ready'].includes(o.lifecycle) ? 'awaiting_handover' : null;
}

/**
 * A protected change still being secured, as far as this view can tell:
 * the chef's and support's command list, or the pending markers every view
 * gets. Steps wait for it (the API refuses them with CONFLICT meanwhile).
 */
export function unresolvedChange(o: StaffOrderDetail): boolean {
	if ((o.commands ?? []).some((c) => c.state !== 'secured')) return true;
	if (o.recovery_state && o.recovery_state !== 'secured') return true;
	if (o.lifecycle === 'confirmation_pending' || o.lifecycle === 'cancellation_pending') return true;
	return Boolean(
		o.fulfillment?.pending ||
		o.appointment_change?.state === 'processing' ||
		o.delivery_choice?.pending ||
		o.delivery_failure?.pending ||
		o.late_payment?.state === 'processing'
	);
}

/** The order as a list row, for the list's status words (payment_state is the detail's payment). */
function asRow(o: StaffOrderDetail): StaffOrderRow {
	return {
		...o,
		items: o.items ?? [],
		payment_state: o.payment?.state ?? null
	} as StaffOrderRow;
}

/** `pending`: a handover or no-show still being secured (K11): never shown as finished. */
export type DetailStage = Stage | 'out' | 'problem' | 'noshow' | 'pending';

/** The status pill: the list's words, plus the fulfilment states the list doesn't show. */
export function detailStatus(
	o: StaffOrderDetail,
	view: StaffView
): { stage: DetailStage; text: string; released: boolean } {
	const pending = o.fulfillment?.pending;
	if (pending === 'handover') {
		return { stage: 'pending', text: 'Handover being recorded', released: false };
	}
	if (pending === 'no_show') {
		return { stage: 'pending', text: 'Not collected · being recorded', released: false };
	}
	const fs = fulfillmentState(o);
	if (o.lifecycle === 'ready' && fs === 'out_for_delivery') {
		return { stage: 'out', text: 'Out for delivery', released: false };
	}
	if (o.lifecycle === 'ready' && fs === 'exception') {
		return { stage: 'problem', text: 'Ready · problem recorded', released: false };
	}
	if (o.lifecycle === 'completed' && fs === 'no_show') {
		return { stage: 'noshow', text: 'Not collected · completed', released: false };
	}
	// Technical support's detail (unlike its list row) says whether money arrived.
	if (
		view === 'technical_support' &&
		o.lifecycle === 'cancelled' &&
		o.cancel_reason === 'payment_not_completed'
	) {
		const paid = o.refund != null || o.payment?.state === 'succeeded';
		return paid
			? { stage: 'cancelled', text: 'Cancelled · full return', released: false }
			: { stage: 'cancelled', text: 'Not paid · released', released: true };
	}
	return statusOf(asRow(o), view);
}

// ------------------------------------------------------------ banners

export interface Banner {
	key: string;
	tone: 'info' | 'warn' | 'bad';
	title: string;
	text: string;
	small: string[];
}

const HELD_PAYMENT = ['created', 'pending', 'unknown'];
const LONG_MS = 15 * 60 * 1000;

/**
 * The sketch's banners for the order's live state (r03b2 order.js), worded
 * for what this view's keys tell. Fulfilment never sees unpaid orders (the
 * API scopes it to orders that were confirmed).
 */
export function banners(o: StaffOrderDetail, view: StaffView, now: Date): Banner[] {
	const out: Banner[] = [];
	const chef = view === 'chef';
	const flags = o.attention ?? [];
	const mismatch = flags.includes('payment_mismatch');
	const day = dayShort(o.date);
	if (o.lifecycle === 'awaiting_payment' && o.payment_requirement === 'none') {
		out.push({
			key: 'free',
			tone: 'info',
			title: 'Free order · waiting for the customer to confirm.',
			text: 'Nothing to pay.',
			small: ['Don’t start this cake until it’s confirmed.']
		});
	} else if (o.lifecycle === 'awaiting_payment') {
		const p = o.payment ?? null;
		const title = mismatch
			? 'Payment doesn’t match the order.'
			: p?.state === 'pending'
				? 'Payment in progress.'
				: p?.state === 'unknown' || flags.includes('payment_unknown')
					? 'Payment result unknown.'
					: 'Waiting for payment.';
		const long = p ? now.getTime() - new Date(p.requested_at).getTime() > LONG_MS : false;
		const started = p ? `The customer started paying ${when(p.requested_at)}. ` : '';
		const checking = mismatch
			? 'It isn’t accepted: the order is never confirmed on payment evidence that doesn’t match.'
			: !p
				? 'No payment has been started yet.'
				: p.state === 'pending' && !long
					? 'The payment page is open.'
					: HELD_PAYMENT.includes(p.state)
						? 'We’re checking with the payment provider.'
						: p.state === 'failed'
							? 'The last try failed; the customer may try again within the 15-minute hold.'
							: 'The last try wasn’t completed; the customer may try again within the 15-minute hold.';
		const held = mismatch || (p !== null && HELD_PAYMENT.includes(p.state));
		const small = held
			? [
					`${long || p?.state === 'unknown' ? 'The customer sees “Still checking payment; do not pay again”. ' : ''}No second payment can start while this one may still succeed. The place on ${day} stays held until we know${mismatch ? ' and a manager has reconciled it' : ''}. Don’t start this cake.`
				]
			: ['Not paid yet. Don’t start this cake.'];
		if (mismatch) small.push('A manager reconciles it; the tools come in a later update.');
		out.push({
			key: 'payment',
			tone: mismatch ? 'bad' : 'info',
			title,
			text: started + checking,
			small
		});
	}
	if (o.lifecycle === 'confirmation_pending') {
		const free = o.payment_requirement === 'none';
		const amount =
			chef && o.payment?.amount_minor != null ? ` of ${money(o, o.payment.amount_minor)}` : '';
		out.push({
			key: 'confirming',
			tone: 'info',
			title: free ? 'Free · confirming the order.' : 'Paid · confirming the order.',
			text: free
				? 'The customer confirmed; we’re securing the confirmation (usually seconds to minutes).'
				: `The payment${amount} succeeded; we’re securing the confirmation (usually seconds to minutes).`,
			small: [
				'Nothing to do yet. The handover isn’t promised and production doesn’t count until it shows Paid. If it can’t be confirmed, it shows here.'
			]
		});
	}
	if (o.lifecycle === 'cancellation_pending') {
		out.push({
			key: 'cancelling',
			tone: 'info',
			title: 'Cancelling · not confirmed yet.',
			text: 'A cancellation is being secured. Don’t start or hand over this cake.',
			small: ['The outcome shows here shortly.']
		});
	}
	// A payment that arrived after the order was released: refused paid
	// confirmation, full return started automatically (late_capture_terminal).
	if (o.lifecycle === 'cancelled' && o.cancel_reason === 'payment_not_completed' && o.refund) {
		out.push({
			key: 'refused',
			tone: 'bad',
			title: 'Payment arrived after the order was released.',
			text: `The order was released${o.cancelled_at ? ` ${when(o.cancelled_at)}` : ''} because the payment hadn’t completed; the money then arrived. The order stays cancelled (it’s never reopened) and the full amount is being returned automatically — never the 50% cancellation charge.`,
			small: ['Tell the customer. If they still want a cake, create a new order.']
		});
	}
	if (mismatch && o.lifecycle !== 'awaiting_payment') {
		out.push({
			key: 'mismatch',
			tone: 'bad',
			title: 'Payment doesn’t match the order.',
			text: 'It isn’t accepted: the order is never confirmed on payment evidence that doesn’t match.',
			small: ['A manager reconciles it; the tools come in a later update.']
		});
	}
	if (
		flags.includes('payment_unknown') &&
		!mismatch &&
		o.lifecycle !== 'awaiting_payment' &&
		o.lifecycle !== 'confirmation_pending'
	) {
		out.push({
			key: 'unknown',
			tone: 'info',
			title: 'Payment result unknown.',
			text: 'We’re checking with the payment provider.',
			small: ['Don’t ask the customer to pay again.']
		});
	}
	if (flags.includes('refund_needs_attention')) {
		out.push({
			key: 'refund',
			tone: 'bad',
			title: 'Refund needs attention.',
			text: 'A manager reviews this order’s refund. Nothing is resent and nothing becomes owed automatically.',
			small: ['The manager tools come in a later update.']
		});
	}
	if (flags.includes('refund_quarantine')) {
		const obs = (o.refund_observations ?? []).find((r) => r.quarantined && !r.resolved_at);
		const what =
			chef && obs?.amount_minor != null
				? `${formatMoney(obs.amount_minor, obs.currency ?? o.currency ?? 'GBP')} was refunded on this payment ${when(obs.observed_at)}`
				: 'A refund was made on this payment';
		out.push({
			key: 'quarantine',
			tone: 'warn',
			title: 'Refund made outside GK Cakes.',
			text: `${what} directly in the payment provider’s dashboard, not from this workspace.`,
			small: [
				'The order itself is unchanged. The money counts as possibly returned, and no refund can be sent from here for this payment until a manager matches or explains it.'
			]
		});
	}
	return out;
}

/** Paid late (K04 Q2): the decision card. Deciding itself is the next slice. */
export function latePaymentCard(
	o: StaffOrderDetail,
	view: StaffView
): { title: string; text: string[]; note: string } | null {
	if (o.lifecycle !== 'paid_awaiting_agreement') return null;
	if (view !== 'chef') {
		return {
			title: 'Paid late · Elina decides',
			text: [
				'Don’t start this cake: Elina decides with the customer whether it goes ahead. Its day place is already counted.'
			],
			note: ''
		};
	}
	const paid = o.late_payment?.paid_minor;
	const lines = [
		`The payment completed${o.paid_at ? ` ${when(o.paid_at)}` : ''}, after the 192-hour point before the handover, so it isn’t accepted automatically.`,
		`${paid != null ? `The ${formatMoney(paid, o.late_payment?.currency ?? o.currency ?? 'GBP')} is received, and the` : 'The'} order already counts in ${dayShort(o.date)} (its place is taken, like a confirmed order). Until you decide: no production, no automatic start and no collection details. Silence never counts as agreement. A full return releases the place.`
	];
	const offer = o.late_payment?.offer;
	if (o.late_payment?.state === 'offered' && offer) {
		lines.push(
			`You offered ${dayShort(offer.date)}, ${timeRange(offer.start, offer.end)}; waiting for the customer’s answer (they can accept until ${when(offer.accept_until)}).`
		);
	}
	return {
		title: 'Paid late · your decision',
		text: lines,
		note: 'Making an offer to the customer or returning everything isn’t on this page yet; it comes in the next update.'
	};
}

// ----------------------------------------------------------- progress

export interface ProgressStep {
	label: string;
	at: string | null;
	note: string;
	done: boolean;
}

function event(o: StaffOrderDetail, action: string): FulfillmentEvent | undefined {
	return [...(o.fulfillment_history ?? [])].reverse().find((e) => e.action === action);
}

/** The Progress card's steps (r03b2), from the API's times; "Scheduled" for a time ahead. */
export function progressSteps(o: StaffOrderDetail, view: StaffView, now: Date): ProgressStep[] {
	const step = (label: string, at: string | null | undefined, note = ''): ProgressStep => ({
		label,
		at: at ?? null,
		note,
		done: Boolean(at) && new Date(at as string) <= now
	});
	const chef = view === 'chef';
	const free = o.payment_requirement === 'none';
	const paidNote =
		chef && o.total_minor != null && !free ? money(o, o.total_minor) : free ? 'free order' : '';
	const p = o.payment ?? null;
	const PAY: Record<string, string> = {
		created: 'payment page being prepared',
		pending: 'payment in progress',
		unknown: 'payment result unknown',
		succeeded: 'paid',
		failed: 'last try failed',
		cancelled: 'last try not completed'
	};
	switch (o.lifecycle) {
		case 'awaiting_payment':
			if (free)
				return [step('Order created', o.placed_at), step('Customer confirms', null, 'waiting')];
			return [
				step('Order created', o.placed_at),
				step('Payment started', p?.requested_at, p ? (PAY[p.state] ?? p.state) : ''),
				step(
					'Payment result',
					null,
					(o.attention ?? []).includes('payment_mismatch')
						? 'doesn’t match · a manager reconciles'
						: 'checking with the payment provider'
				)
			];
		case 'confirmation_pending':
			return [
				step(free ? 'Confirmed by the customer' : 'Paid', p?.requested_at ?? o.placed_at, paidNote),
				step('Confirming the order', null, 'usually seconds to minutes'),
				step('In production (automatic)', null, '48 hours after payment')
			];
		case 'paid_awaiting_agreement':
			return [
				step('Paid (late)', o.paid_at, paidNote),
				step(
					chef ? 'Your decision' : 'Elina’s decision',
					null,
					'pending · the day place is already counted'
				),
				step('In production (automatic)', null, 'held until the decision')
			];
		case 'cancelled':
		case 'cancellation_pending': {
			const steps = [step('Order created', o.placed_at)];
			if (o.paid_at || o.free_confirmed_at) {
				steps.push(
					step(o.paid_at ? 'Paid' : 'Confirmed', o.paid_at ?? o.free_confirmed_at, paidNote)
				);
			}
			const by = o.cancellation?.requested_by;
			steps.push(
				step(
					o.lifecycle === 'cancellation_pending' ? 'Cancelling' : 'Cancelled',
					o.cancelled_at,
					o.lifecycle === 'cancellation_pending'
						? 'not confirmed yet'
						: by === 'chef'
							? 'by Elina'
							: by === 'customer'
								? 'the customer asked'
								: o.cancel_reason === 'payment_not_completed' ||
									  o.cancel_reason === 'free_not_confirmed'
									? 'released automatically'
									: ''
				)
			);
			return steps;
		}
	}
	// Fulfilment gets no payment times: the order was confirmed (it's in its scope).
	const steps =
		view === 'fulfillment'
			? [{ label: 'Confirmed', at: null, note: '', done: true }]
			: [
					step(o.paid_at ? 'Paid' : 'Confirmed', o.paid_at ?? o.free_confirmed_at ?? null, paidNote)
				];
	const prod = o.production;
	if (prod.source === 'manual') {
		steps.push(step('Started making', prod.started_at));
	} else {
		steps.push(
			step(
				'In production (automatic)',
				prod.started_at ?? prod.scheduled_at,
				free ? '48 hours after confirmation' : '48 hours after payment'
			)
		);
	}
	steps.push(step('Ready', event(o, 'ready')?.received_at));
	if (o.fulfillment_mode === 'gk_delivery') {
		steps.push(step('Out for delivery', event(o, 'dispatch')?.received_at));
	}
	const fs = fulfillmentState(o);
	if (fs === 'no_show') {
		steps.push(step('Not collected', o.fulfillment?.at, 'recorded by the chef'));
	} else {
		steps.push(
			step(
				'Handed over',
				o.lifecycle === 'completed'
					? (o.fulfillment?.at ?? event(o, 'handover')?.received_at)
					: null,
				o.fulfillment?.pending === 'handover' ? 'being recorded' : ''
			)
		);
	}
	return steps;
}

// ---------------------------------------------------- steps you can do

export type StepAction =
	'start' | 'ready' | 'dispatch' | 'delivery_exception' | 'handover' | 'no_show';

export interface Steps {
	/** Buttons, primary first. */
	actions: StepAction[];
	/** Why a step that might be expected isn't offered (plain words), or ''. */
	note: string;
}

const AWAITING = ['awaiting_handover', 'exception'];

const NOT_POSSIBLE =
	'GK can’t deliver this order (recorded), so it can’t go out. The customer chooses another way.';

/** Whether a GK delivery may go out, as far as this view can tell (README "Fulfilment transitions"). */
export function dispatchBlock(o: StaffOrderDetail): string {
	const d = o.delivery;
	if (o.delivery_choice?.reason === 'not_possible' || o.delivery_choice?.not_possible) {
		return NOT_POSSIBLE;
	}
	const f = o.delivery_failure;
	if (f && f.cause === 'our_side') {
		if (f.open || !f.chosen) {
			return 'The delivery failed on our side and the customer hasn’t chosen what happens next, so it can’t go out again yet.';
		}
		if (f.chosen.choice === 'redelivery' && f.redelivery_time_set === false) {
			return 'The customer chose a new delivery. Agree the new time with them (change the appointment) before it goes out again.';
		}
	}
	if (!d || (d.state !== 'paid' && d.state !== 'waived')) {
		return 'Out for delivery is available once delivery is paid.';
	}
	return '';
}

/**
 * The steps this view may take now (README eligibility tables; K00:
 * technical support looks only, no-show is the chef's). The API still
 * checks everything: a refusal is shown in plain words.
 */
export function availableSteps(o: StaffOrderDetail, view: StaffView, now: Date): Steps {
	const none = (note = ''): Steps => ({ actions: [], note });
	if (view === 'technical_support') return none();
	if (!['confirmed', 'in_production', 'ready'].includes(o.lifecycle)) return none();
	if (unresolvedChange(o)) {
		return none(
			o.fulfillment?.pending
				? ''
				: 'Another change to this order is still being processed. The steps come back when it’s done.'
		);
	}
	if (o.lifecycle === 'confirmed') {
		return o.production.state === 'started' ? none() : { actions: ['start'], note: '' };
	}
	if (o.lifecycle === 'in_production') return { actions: ['ready'], note: '' };
	const fs = fulfillmentState(o) ?? '';
	const actions: StepAction[] = [];
	let note = '';
	if (o.fulfillment_mode === 'gk_delivery') {
		if (AWAITING.includes(fs)) {
			note = dispatchBlock(o);
			if (!note) actions.push('dispatch');
		}
		if (fs === 'out_for_delivery') actions.push('handover');
		if ([...AWAITING, 'out_for_delivery'].includes(fs)) actions.push('delivery_exception');
		return { actions, note };
	}
	if (AWAITING.includes(fs)) {
		actions.push('handover');
		if (view === 'chef') {
			if (now >= new Date(o.slot.starts_at)) actions.push('no_show');
			else {
				note = `If the customer doesn’t come, you can record it from ${londonTime(o.slot.starts_at)} on ${dayShort(o.date)} (the start of their time).`;
			}
		}
		actions.push('delivery_exception');
	}
	return { actions, note };
}

/** Each step's button (r03b2 wording where it has one). */
export function stepLabel(a: StepAction, o: StaffOrderDetail): string {
	switch (a) {
		case 'start':
			return 'Start making';
		case 'ready':
			return 'Mark ready';
		case 'dispatch':
			return 'Out for delivery';
		case 'handover':
			return o.fulfillment_mode === 'gk_delivery' ? 'Delivered · hand over' : 'Hand over';
		case 'no_show':
			return 'Customer didn’t come…';
		case 'delivery_exception':
			return o.fulfillment_mode === 'gk_delivery' ? 'Delivery problem…' : 'Record a problem…';
	}
}

/** Steps that ask before they are sent: handover (r03b2's two-step confirm), no-show and a problem. */
export function asksFirst(a: StepAction): boolean {
	return a === 'handover' || a === 'no_show' || a === 'delivery_exception';
}

const ROLE: Record<StaffView, string> = {
	chef: 'Chef',
	fulfillment: 'Fulfilment',
	technical_support: 'Technical support'
};

/** The confirm step's words. */
export function confirmText(a: StepAction, o: StaffOrderDetail, view: StaffView): string {
	const who = `Recorded as you (${ROLE[view]}).`;
	switch (a) {
		case 'handover':
			return `Hand over ${orderLabel(o).toLowerCase()} to ${o.customer_full_name} · ${modeText(o.fulfillment_mode)}. ${who} This completes the order.`;
		case 'no_show':
			return `Record that ${o.customer_full_name} didn’t come for ${orderLabel(o).toLowerCase()} (their time started ${londonTime(o.slot.starts_at)}). ${who} This completes the order as not collected. The refund owed becomes 50% of what they paid (anything already refunded counts towards it); it’s requested automatically and can’t be undone.`;
		case 'delivery_exception':
			return o.fulfillment_mode === 'gk_delivery'
				? 'What went wrong with the delivery? It’s kept with the order. The order stays open: nothing is completed or refunded.'
				: 'What went wrong (for example “the customer didn’t arrive”)? It’s kept with the order. The order stays open: nothing is completed or refunded.';
		default:
			return '';
	}
}

/** The confirm button's words. */
export function confirmLabel(a: StepAction): string {
	return a === 'handover'
		? 'Confirm handover'
		: a === 'no_show'
			? 'Record not collected'
			: 'Record the problem';
}

/** What the page says once a step is done (r03b2's preview messages, live). */
export function doneText(a: StepAction, outcome: 'done' | 'noshow' = 'done'): string {
	switch (a) {
		case 'start':
			return 'Started ✓ · recorded at this time. Choosing ingredient batches comes with Ingredients.';
		case 'ready':
			return 'Marked ready for handover ✓';
		case 'dispatch':
			return 'Out for delivery ✓ · handover is recorded when the cake reaches the customer.';
		case 'delivery_exception':
			return 'Problem recorded ✓ · the order stays open.';
		case 'handover':
			return 'Handed over ✓ · the order is complete.';
		case 'no_show':
			return outcome === 'noshow'
				? 'Recorded as not collected ✓ · the order is complete. The refund owed and what has gone back so far are under Refund, as recorded.'
				: 'Recorded ✓';
	}
}

/** A problem's reason: one line, 1–500 characters (the API's rule). */
export function reasonProblem(reason: string): string {
	const r = reason.trim();
	if (!r) return 'Say what went wrong.';
	if ([...r].length > 500) return 'Keep it to 500 characters.';
	const control = (c: string) => {
		const n = c.codePointAt(0) ?? 0;
		return n < 0x20 || (n >= 0x7f && n <= 0x9f) || n === 0x2028 || n === 0x2029;
	};
	if ([...r].some(control)) return 'Keep it to one line.';
	return '';
}

// ------------------------------------------------------------ commands

/**
 * K00 command key for one user action at the version it was taken on: the
 * same action at the same version reuses it (a retry gets the original
 * answer); after a reload the version, and so the key, is new.
 */
export function commandKey(a: StepAction, o: StaffOrderDetail, nonce: string): string {
	return `chef-${a}-${o.id}-v${o.version}-${nonce}`;
}

/**
 * What a stored key belongs to: the action, the version and, for a problem,
 * the exact reason sent. A retry of the same request reuses its key; an
 * edited reason is a new request with a new key (the API would refuse the
 * old key with another body: KEY_REUSED).
 */
export function keySlot(a: StepAction, o: StaffOrderDetail, reason = ''): string {
	return a === 'delivery_exception' ? `${a}:${o.version}:${reason.trim()}` : `${a}:${o.version}`;
}

export interface Accepted {
	order_id: string;
	operation_id: string;
	state: string;
	poll_after_seconds: number;
}

export type StepAnswer =
	| { kind: 'done'; lifecycle: string; fulfillment_state?: string }
	| { kind: 'accepted'; accepted: Accepted };

/** Sends one step at the order's version. */
export async function sendStep(
	api: StaffApi,
	o: StaffOrderDetail,
	a: StepAction,
	key: string,
	reason?: string
): Promise<StepAnswer> {
	const opts = { baseUrl: api.baseUrl, token: api.token, fetch: api.fetch, idempotencyKey: key };
	if (a === 'start') {
		const r = await apiPost<{ order_id: string; lifecycle: string }>(
			`/api/v1/admin/orders/${o.id}/start-production`,
			{ expected_version: o.version },
			opts
		);
		return { kind: 'done', lifecycle: r.lifecycle };
	}
	const body: { action: string; expected_version: number; reason?: string } = {
		action: a,
		expected_version: o.version
	};
	if (a === 'delivery_exception') body.reason = (reason ?? '').trim();
	const r = await apiPost<
		Accepted | { order_id: string; lifecycle: string; fulfillment_state: string }
	>(`/api/v1/admin/orders/${o.id}/transitions`, body, opts);
	if (a === 'handover' || a === 'no_show') return { kind: 'accepted', accepted: r as Accepted };
	const plain = r as { lifecycle: string; fulfillment_state: string };
	return { kind: 'done', lifecycle: plain.lifecycle, fulfillment_state: plain.fulfillment_state };
}

/**
 * Where a handover or no-show stands in a fresh read: still pending, done
 * (completed: handed over or not collected) or refused (pending cleared but
 * not completed; the chef's and support's command list says it outright).
 */
export function settled(
	o: StaffOrderDetail,
	operationId: string
): 'pending' | 'done' | 'noshow' | 'refused' {
	const cmd = (o.commands ?? []).find((c) => c.id === operationId);
	if (cmd && cmd.state !== 'secured') return 'pending';
	if (o.fulfillment?.pending) return 'pending';
	if (o.lifecycle === 'completed') return fulfillmentState(o) === 'no_show' ? 'noshow' : 'done';
	if (cmd?.outcome_status === 'refused' || !cmd) return 'refused';
	return 'pending';
}

/**
 * A handover or no-show already being secured (for example sent from
 * another device): the step and, where the view has the command list, its
 * command, so the page can watch for the outcome.
 */
export function pendingStep(
	o: StaffOrderDetail
): { action: StepAction; operation_id: string } | null {
	const p = o.fulfillment?.pending;
	if (p !== 'handover' && p !== 'no_show') return null;
	const cmd = [...(o.commands ?? [])].reverse().find((c) => c.kind === p && c.state !== 'secured');
	return { action: p, operation_id: cmd?.id ?? '' };
}

/** Polls the order for a K11 step's outcome (every poll_after_seconds, at most `tries` reads). */
export async function pollStep(
	read: () => Promise<StaffOrderDetail>,
	accepted: Accepted,
	opts: { tries?: number; wait?: (ms: number) => Promise<void>; active?: () => boolean } = {}
): Promise<{ order: StaffOrderDetail | null; outcome: 'pending' | 'done' | 'noshow' | 'refused' }> {
	const tries = opts.tries ?? 20;
	const wait = opts.wait ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));
	const every = Math.max(1, Math.min(accepted.poll_after_seconds || 3, 10)) * 1000;
	let last: StaffOrderDetail | null = null;
	for (let i = 0; i < tries; i++) {
		await wait(every);
		if (opts.active && !opts.active()) break;
		try {
			last = await read();
		} catch (e) {
			if (e instanceof ApiError && e.signInRequired) throw e;
			continue;
		}
		const s = settled(last, accepted.operation_id);
		if (s !== 'pending') return { order: last, outcome: s };
	}
	return { order: last, outcome: 'pending' };
}

/** What a refusal or failure means for the page. */
export interface Refusal {
	text: string;
	/** Read the order again (its state is not what the page shows). */
	reload: boolean;
	/** 409 STALE_VERSION: show the "changed while you were looking" banner. */
	stale: boolean;
	/** 401: the session ended. */
	signIn: boolean;
	/** Repeating the same action is safe and keeps its key. */
	retry: boolean;
}

/** Refusal codes in plain words (api/README; internal/httpapi conflict codes). */
export function refusalOf(e: unknown, a: StepAction): Refusal {
	const r = (text: string, over: Partial<Refusal> = {}): Refusal => ({
		text,
		reload: true,
		stale: false,
		signIn: false,
		retry: false,
		...over
	});
	if (!(e instanceof ApiError)) {
		return r(
			'Something went wrong on this page. Reload it and check the order before trying again.',
			{
				reload: false
			}
		);
	}
	if (e.status === 401)
		return r('Your session ended. Please sign in again.', { signIn: true, reload: false });
	if (e.status === 0) {
		return r(
			'The service couldn’t be reached, so it isn’t known whether this was recorded. Try again: repeating it is safe.',
			{ reload: false, retry: true }
		);
	}
	switch (e.code) {
		case 'STALE_VERSION':
			return r(
				'This order changed while you were looking. The latest version is shown; check it before you act.',
				{
					stale: true
				}
			);
		case 'CONFLICT':
			return r(
				a === 'start'
					? 'Production can’t be started now: the order isn’t waiting to start any more, or another change to it is still being processed. The latest version is shown.'
					: 'Another change to this order is still being processed (for example a cancellation or a new time). Wait a moment and check the order again.'
			);
		case 'TRANSITION_NOT_ALLOWED':
			return r('This step isn’t possible for the order as it is now. The latest version is shown.');
		case 'HANDOVER_NOT_DUE':
			return r(
				'The customer’s time hasn’t started yet, so you can’t record that they didn’t come. Try again once it has started.'
			);
		case 'DELIVERY_NOT_PAID':
			return r('Delivery isn’t paid or waived yet, so the cake can’t go out for delivery.');
		case 'DELIVERY_NOT_POSSIBLE':
			return r(NOT_POSSIBLE);
		case 'DELIVERY_FAILURE_OPEN':
			return r(
				'The delivery failed on our side and the customer hasn’t chosen what happens next, so it can’t go out again yet.'
			);
		case 'REDELIVERY_TIME_NOT_SET':
			return r(
				'Agree the new delivery time with the customer first (change the appointment), then send it out again.'
			);
		case 'UNAVAILABLE':
			return r(
				'Order changes aren’t available right now. Try again shortly: repeating it is safe.',
				{ reload: false, retry: true }
			);
		case 'FORBIDDEN':
			return r(
				a === 'no_show'
					? 'Only Elina (chef) can record that a customer didn’t come.'
					: 'Your account can’t do this step.',
				{ reload: false }
			);
		case 'NOT_FOUND':
			return r('This order wasn’t found, or it isn’t one you can see any more.');
		case 'KEY_REUSED':
			// The page renews the key when what is sent changes, so this means an
			// earlier try with other details may have been recorded: check first.
			return r(
				'This was already sent once with different details, so nothing new was recorded. Check the order (the Record shows what was saved), then send it again if it’s still needed.',
				{ reload: false }
			);
		case 'INVALID_INPUT':
			return r(
				a === 'delivery_exception'
					? 'Check what went wrong: one line, up to 500 characters.'
					: 'This step couldn’t be sent as it is. Reload the page and try again.',
				{ reload: false }
			);
	}
	if (e.status >= 500 || e.retryable) {
		return r('Something went wrong on our side. Try again: repeating it is safe.', {
			reload: false,
			retry: true
		});
	}
	return r('This step wasn’t recorded. The latest version of the order is shown.');
}

// --------------------------------------------------------------- money

export interface Fact {
	label: string;
	value: string;
	tone?: 'bad' | 'ok' | 'info' | '';
}

const PAYMENT_WORDS: Record<string, string> = {
	created: 'Payment page being prepared',
	pending: 'Payment in progress',
	unknown: 'Payment result unknown',
	succeeded: 'Paid',
	failed: 'Payment failed · may try again within the 15-minute hold',
	cancelled: 'Payment not completed'
};

/** The Money card (chef only; the API sends money to the chef alone). */
export function moneyFacts(o: StaffOrderDetail): Fact[] {
	if (o.total_minor == null) return [];
	const facts: Fact[] = [];
	const free = o.payment_requirement === 'none';
	if (o.subtotal_minor != null)
		facts.push({ label: 'Cake price', value: money(o, o.subtotal_minor) });
	if (o.discount_minor) facts.push({ label: 'Discount', value: `−${money(o, o.discount_minor)}` });
	if (free) facts.push({ label: 'To pay', value: 'Free' });
	else if (o.lifecycle === 'awaiting_payment') {
		const mismatch = (o.attention ?? []).includes('payment_mismatch');
		facts.push({
			label: 'Cake payment',
			value: mismatch
				? 'Doesn’t match · held'
				: o.payment
					? (PAYMENT_WORDS[o.payment.state] ?? o.payment.state)
					: 'Not started'
		});
	} else if (o.lifecycle === 'confirmation_pending') {
		facts.push({ label: 'Received for the cake', value: money(o, o.total_minor) });
	} else if (o.paid_at || o.payment?.state === 'succeeded') {
		facts.push({ label: 'Paid for the cake', value: money(o, o.total_minor) });
	} else {
		facts.push({ label: 'Cake total', value: money(o, o.total_minor) });
	}
	const outside = (o.refund_observations ?? []).find((r) => r.quarantined && !r.resolved_at);
	if (outside?.amount_minor != null) {
		facts.push({
			label: 'Refunded outside GK Cakes',
			value: `${formatMoney(outside.amount_minor, outside.currency ?? o.currency ?? 'GBP')} · needs a manager`,
			tone: 'bad'
		});
	}
	if (o.delivery?.paid_minor != null) {
		facts.push({ label: 'Paid for delivery', value: money(o, o.delivery.paid_minor) });
	}
	if (o.full_refund_deadline_at) {
		facts.push({
			label: 'Customer’s full-refund deadline',
			value: when(o.full_refund_deadline_at)
		});
	}
	return facts;
}

/** The order-level refund state's words (r03b2 REFUND_STATE). */
export const REFUND_WORDS: Record<string, [string, 'info' | 'warn' | 'ok' | 'bad']> = {
	requested: ['Refund owed · nothing sent yet', 'info'],
	pending: ['Refund in progress · not confirmed', 'info'],
	partly_returned: ['Partly refunded', 'warn'],
	returned: ['Refunded', 'ok'],
	needs_attention: ['Refund needs attention', 'bad']
};

/** The refund ledger, the server's amounts as they are (chef only). */
export function refundFacts(o: StaffOrderDetail): Fact[] {
	const r = o.refund;
	if (!r || r.target_minor == null) return [];
	const cur = r.currency ?? o.currency ?? 'GBP';
	const m = (n: number) => formatMoney(n, cur);
	const facts: Fact[] = [
		{ label: 'Owed in total', value: m(r.target_minor) },
		{ label: 'Refunded (confirmed)', value: m(r.succeeded_minor), tone: 'ok' },
		{
			label: 'Sent, result not confirmed',
			value: m(r.in_progress_minor ?? 0),
			tone: r.in_progress_minor ? 'info' : ''
		}
	];
	if (r.delivery_pending_minor) {
		facts.push({
			label: 'Delivery refund · your step',
			value: m(r.delivery_pending_minor),
			tone: 'bad'
		});
	}
	if (o.refund_goal?.remaining_minor != null) {
		facts.push({
			label: 'Still owed',
			value: formatMoney(o.refund_goal.remaining_minor, o.refund_goal.currency ?? cur),
			tone: o.refund_goal.remaining_minor ? 'bad' : ''
		});
	}
	return facts;
}

const PART_LABELS: Record<string, string> = {
	cake: 'Cake payment',
	delivery: 'Delivery payment',
	stray: 'Unmatched delivery payment'
};

/**
 * The refund ledger "By payment" (r03b2), one row per kind from the
 * refund block's `parts` (0038), the server's amounts only. Chef only (the
 * API sends parts to the chef alone); empty without parts.
 */
export function refundPartRows(o: StaffOrderDetail): { label: string; facts: Fact[] }[] {
	const r = o.refund;
	if (!r?.parts?.length) return [];
	const m = (n: number) => formatMoney(n, r.currency ?? o.currency ?? 'GBP');
	return r.parts.map((p) => {
		const facts: Fact[] = [
			{ label: 'Share owed', value: m(p.target_minor) },
			{
				label: 'Refunded',
				value: p.succeeded_minor ? m(p.succeeded_minor) : '—',
				tone: p.succeeded_minor ? 'ok' : ''
			},
			{
				label: 'Sent, not confirmed',
				value: p.in_progress_minor ? m(p.in_progress_minor) : '—',
				tone: p.in_progress_minor ? 'info' : ''
			}
		];
		if (p.pending_chef_minor) {
			facts.push({ label: 'Your step', value: m(p.pending_chef_minor), tone: 'bad' });
		}
		return { label: PART_LABELS[p.kind] ?? p.kind, facts };
	});
}

/** Who cancelled, in the sketch's words for each view. */
export function cancelledText(o: StaffOrderDetail, view: StaffView): string {
	const reason = o.cancel_reason;
	if (reason === 'payment_not_completed' && !o.refund) {
		return `Nobody completed the payment, so the order was released${o.cancelled_at ? ` ${when(o.cancelled_at)}` : ''}: the day place and any finished cake went back and nothing is owed either way. A released order isn’t reopened. If the customer comes back, create a new order and link.`;
	}
	if (reason === 'free_not_confirmed') {
		return `This free order wasn’t confirmed by the customer within 15 minutes, so it was released${o.cancelled_at ? ` ${when(o.cancelled_at)}` : ''}: the day place went back and nothing is owed either way. A released order isn’t reopened.`;
	}
	const by = o.cancellation?.requested_by;
	const who =
		reason === 'late_payment_returned'
			? 'The late payment is being returned in full; the order ended.'
			: reason === 'payment_not_completed'
				? 'This order was released before its payment completed.'
				: by === 'chef'
					? 'Elina cancelled this order.'
					: by === 'customer'
						? 'The customer cancelled this order.'
						: 'This order was cancelled.';
	return view === 'fulfillment'
		? `${who} Nothing to make or hand over. Refunds are handled by Elina.`
		: `${who} Nothing to make or hand over.`;
}

// ------------------------------------------------------------ history

const ACTION_WORDS: Record<string, string> = {
	ready: 'Marked ready',
	dispatch: 'Out for delivery',
	delivery_exception: 'Problem recorded',
	delivery_failed: 'Delivery failed (our side)',
	handover: 'Handed over',
	no_show: 'Not collected'
};

/** The order's fulfilment and time-change record, oldest first. */
export function historyLines(o: StaffOrderDetail): { at: string; text: string }[] {
	const lines: { at: string; text: string }[] = [];
	for (const e of o.fulfillment_history ?? []) {
		const words = ACTION_WORDS[e.action] ?? e.action;
		lines.push({
			at: e.received_at,
			text: `${words}${e.state === 'processing' ? ' · being recorded' : ''}${e.reason ? ` · “${e.reason}”` : ''}`
		});
	}
	for (const a of o.appointment_history ?? []) {
		const by = a.requested_by === 'chef' ? 'Elina' : 'the customer';
		const to = `${dayShort(a.to.date)}, ${timeRange(a.to.starts_at, a.to.ends_at)}`;
		const state =
			a.state === 'moved' ? '' : a.state === 'refused' ? ' · not changed' : ' · being confirmed';
		lines.push({ at: a.received_at, text: `Time changed to ${to} by ${by}${state}` });
	}
	if (o.production.state === 'started' && o.production.started_at) {
		lines.push({
			at: o.production.started_at,
			text:
				o.production.source === 'manual'
					? 'Started making (recorded by staff)'
					: 'In production (automatic start)'
		});
	}
	return lines.sort((x, y) => x.at.localeCompare(y.at));
}

/** The GK delivery's state in words (money for the chef only). */
export function deliveryText(o: StaffOrderDetail): string {
	const d = o.delivery;
	if (!d) return '';
	const q = d.quote;
	const amount = q?.amount_minor != null ? `${money(o, q.amount_minor)} · ` : '';
	switch (d.state) {
		case 'awaiting_quote':
			return 'Not quoted yet';
		case 'quoted':
			return `${amount}quoted, not paid yet${q?.pay_by ? ` (pay by ${when(q.pay_by)})` : ''}`;
		case 'payment_checking':
			return `${amount}payment being checked`;
		case 'paid':
			return d.paid_minor != null
				? `${money(o, d.paid_minor)} · paid separately`
				: 'Paid separately';
		case 'waived':
			return 'Free (code) · waived';
		case 'pay_by_passed':
			return `${amount}not paid in time`;
		case 'cancelled_by_choice':
			return 'Not delivering · the customer chose another way';
		default:
			return d.state;
	}
}
