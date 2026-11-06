// Money on the chef's order page (GK-031 slice 3, in the visual language
// and wording of the Creator-approved GK-018 r03b2 sketch): the chef's
// cancellation (100%, api/README "Cancellation"), agreed partial refunds
// with allocations and the chef's delivery share ("Chef refunds and
// recovery"), returning an unmatched (stray) delivery payment with that
// exception refund (0035), the manager recovery tools on one order (0025:
// match or accept an outside refund, decide an unknown refund past its
// window, clear a contradiction, reserve a failed remainder again) and the
// Q5 verified transfer (0026). Every command is the chef's (K00: the API's
// role names are chef, technical_support and fulfillment; technical
// support reads without amounts, fulfilment sees none of this). Amounts are
// the server's integer pence; typed amounts are parsed as text, never
// through a float. Eligibility mirrors the README at the page's clock; the
// API still decides, and its refusals are shown in plain words.
import { ApiError, apiGet, apiPost } from '../api';
import { formatMoney } from '../checkout/format';
import type { StaffApi } from './catalogue';
import { lineProblem } from './order-decisions';
import {
	refusalOf,
	unresolvedChange,
	when,
	type Accepted,
	type Refusal,
	type StaffCollection,
	type StaffOrderDetail,
	type StaffRefundOperation
} from './order-detail';
import type { StaffView } from './orders';

// --------------------------------------------------------- input rules

/** The API's largest refund (£1,000,000 in pence). */
export const MAX_REFUND_MINOR = 100_000_000;

/**
 * An amount typed in pounds ("45", "£45", "45.50", "1,200") as whole pence,
 * parsed as text (never a float). Pence, when given, are two digits.
 */
export function parseAmount(
	text: string,
	what = 'an amount'
): { minor: number } | { error: string } {
	const t = text.trim().replace(/^£\s*/, '');
	if (!t) return { error: `Enter ${what}.` };
	const m = /^(\d{1,3}(?:,\d{3})+|\d+)(?:\.(\d*))?$/.exec(t);
	if (!m) return { error: 'Enter an amount in pounds, for example 20 or 20.50.' };
	const pence = m[2];
	if (pence !== undefined && pence.length !== 2) {
		return { error: 'Give pence as two digits, for example 20.50.' };
	}
	const pounds = m[1].replace(/,/g, '').replace(/^0+(?=\d)/, '');
	if (pounds.length > 7) return { error: 'That amount is too large.' };
	return { minor: Number(pounds) * 100 + Number(pence ?? '0') };
}

/** A required one-line note or reason (1–500 characters): the API's rule. */
export function noteProblem(text: string, what = 'why'): string {
	return lineProblem(text, 500, what);
}

const STRIPE_REFUND_ID = /^(re|pyr)_[A-Za-z0-9_]{1,250}$/;

/** The payment provider's refund ID for a "succeeded" decision ('' when it can be sent). */
export function refundIdProblem(text: string): string {
	const t = text.trim();
	if (!t) return 'Enter the payment provider’s refund ID (it starts with re_).';
	if (!STRIPE_REFUND_ID.test(t)) {
		return 'That isn’t a refund ID: it starts with re_ (or pyr_) followed by letters, digits or _.';
	}
	return '';
}

/**
 * An evidence reference (0026): where the evidence is kept, never the
 * evidence or bank details themselves. 1–200 visible characters; once
 * spaces, hyphens and dots are removed, no run of six or more digits and
 * nothing IBAN-like (the API's and the database's rule).
 */
export function referenceProblem(text: string, what: string): string {
	const t = text.trim();
	if (!t) return `Enter ${what}.`;
	if (t.length > 200) return 'Keep it to 200 characters.';
	if (!/^[\x20-\x7e]+$/.test(t)) {
		return 'Use plain letters, digits and punctuation only (no accents, emoji or line breaks).';
	}
	const bare = t.replace(/[ .-]/g, '');
	if (/[0-9]{6,}/.test(bare) || /[a-z]{2}[0-9]{2}[a-z0-9]{11,30}/i.test(bare)) {
		return 'Never type bank details here: a reference can’t contain six or more digits in a row or anything like an IBAN. Say where the evidence is kept instead.';
	}
	return '';
}

// ------------------------------------------------------ reading money

const money = (o: StaffOrderDetail, minor: number, currency?: string) =>
	formatMoney(minor, currency ?? o.currency ?? 'GBP');

/** States in which a refund operation may still succeed (K05: counted, never resent). */
export const UNRESOLVED = ['reserved', 'submitting', 'pending', 'requires_action', 'unknown'];

/** A collection's room: paid − refunded − reserved (chef only; 0 without amounts). */
export function room(c: StaffCollection): number {
	if (c.state !== 'succeeded' || c.amount_minor == null) return 0;
	return Math.max(0, c.amount_minor - (c.refunded_minor ?? 0) - (c.refund_reserved_minor ?? 0));
}

/**
 * What a collection paid for: the cake, the delivery quote, or a delivery
 * payment that couldn't be used (its outcome is unmatched: a stray, 0035).
 */
export function collectionKind(
	o: StaffOrderDetail,
	c: StaffCollection
): 'cake' | 'delivery' | 'stray' {
	if (c.kind !== 'delivery') return 'cake';
	for (const q of o.delivery_history ?? []) {
		for (const p of q.payments ?? []) {
			if (p.collection_id === c.id && p.outcome === 'unmatched') return 'stray';
		}
	}
	return 'delivery';
}

const KIND_LABEL = {
	cake: 'cake payment',
	delivery: 'delivery payment',
	stray: 'unmatched delivery payment'
};

/** "the cake payment" / "the delivery payment" / "the unmatched delivery payment". */
export function collectionLabel(o: StaffOrderDetail, id: string): string {
	const c = (o.collections ?? []).find((x) => x.id === id);
	return c ? KIND_LABEL[collectionKind(o, c)] : 'payment';
}

function ops(o: StaffOrderDetail): StaffRefundOperation[] {
	return o.refund_operations ?? [];
}

/** The latest operation of a collection that wasn't withdrawn (README: re-reserve and Q5). */
export function latestOperation(
	o: StaffOrderDetail,
	collectionId: string
): StaffRefundOperation | null {
	const list = ops(o)
		.filter((r) => r.collection_id === collectionId && r.state !== 'cancelled')
		.sort((x, y) => x.created_at.localeCompare(y.created_at) || x.id.localeCompare(y.id));
	return list.length ? list[list.length - 1] : null;
}

/**
 * Why the order needs reconciliation first (409 RECONCILIATION_REQUIRED):
 * an unresolved outside refund, a contradiction on a refund or payment, a
 * refund waiting for a provider action or in reconciliation, or a payment
 * whose result is unknown. Empty when none.
 */
export function reconciliationReasons(o: StaffOrderDetail): string[] {
	const out: string[] = [];
	if ((o.refund_observations ?? []).some((b) => b.quarantined && !b.resolved_at)) {
		out.push('a refund made outside GK Cakes');
	}
	if (ops(o).some((r) => r.mismatch_reason)) out.push('a refund whose result changed');
	if ((o.collections ?? []).some((c) => c.mismatch_reason)) {
		out.push('a payment that doesn’t match the order');
	}
	if (ops(o).some((r) => r.state === 'requires_action')) {
		out.push('a refund waiting for an action at the provider');
	}
	if (ops(o).some((r) => UNRESOLVED.includes(r.state) && r.record_state === 'reconcile')) {
		out.push('a refund being reconciled');
	}
	if ((o.collections ?? []).some((c) => c.state === 'unknown'))
		out.push('a payment result unknown');
	return out;
}

/** Any refund of the order that may still succeed (409 REFUND_IN_PROGRESS). */
export function refundInProgress(o: StaffOrderDetail, collectionId?: string): boolean {
	return ops(o).some(
		(r) => UNRESOLVED.includes(r.state) && (!collectionId || r.collection_id === collectionId)
	);
}

const BUSY_NOTE =
	'Another change to this order is still being processed. Refunds and manager decisions come back when it’s done.';
const RECONCILE_NOTE =
	'A refund can’t be sent while a manager case on this order is open (see Needs a manager above).';
const IN_PROGRESS_NOTE =
	'A refund for this order is on its way. Another can be sent once its result is known: we never send a second refund while one may still succeed.';

/** Why no new refund can be sent now ('' when one can), in the order the API checks. */
export function refundBlock(o: StaffOrderDetail, collectionId?: string): string {
	if (unresolvedChange(o)) return BUSY_NOTE;
	if (reconciliationReasons(o).length) return RECONCILE_NOTE;
	if (refundInProgress(o, collectionId)) return IN_PROGRESS_NOTE;
	return '';
}

// ---------------------------------------------------------- cancelling

const CANCELLABLE = ['confirmed', 'in_production', 'ready'];

export interface CancelChoice {
	can: boolean;
	note: string;
}

/**
 * The chef's cancellation (100%): an accepted, secured order (confirmed,
 * in production or ready) with no cancellation or other protected change
 * in progress (README "Cancellation"; the chef's route has no dispatch
 * rule, unlike the customer's).
 */
export function cancelChoice(o: StaffOrderDetail, view: StaffView): CancelChoice {
	if (view !== 'chef' || !CANCELLABLE.includes(o.lifecycle)) return { can: false, note: '' };
	if (unresolvedChange(o)) {
		return {
			can: false,
			note: 'Cancelling comes back when the change being processed is done.'
		};
	}
	return { can: true, note: '' };
}

export interface CancelPreview {
	/** Everything paid for the order (cake + delivery that paid its quote): the 100% basis. */
	paidMinor: number;
	cakeMinor: number;
	deliveryMinor: number;
	/** Unmatched delivery payments not yet returned: returned in full too (0035). */
	strayMinor: number;
	/** Already refunded or on its way under an earlier goal (it counts, K05 cumulative). */
	alreadyMinor: number;
	/** The delivery share is returned automatically (a recorded GK-side floor), not the chef's step. */
	deliveryAutomatic: boolean;
	currency: string;
}

/**
 * What the chef's cancellation returns, from the server's figures: 100% of
 * the paid aggregate (the cake automatically; a delivery paid separately
 * waits for the chef unless GK couldn't deliver) plus any unmatched delivery
 * payment in full. The API computes the actual goal; this is its preview.
 */
export function cancelPreview(o: StaffOrderDetail): CancelPreview {
	const paid = (o.collections ?? []).filter((c) => c.state === 'succeeded');
	let cake = 0;
	let delivery = 0;
	let stray = 0;
	for (const c of paid) {
		const k = collectionKind(o, c);
		if (k === 'cake') cake += c.amount_minor ?? 0;
		else if (k === 'delivery') delivery += c.amount_minor ?? 0;
		else stray += room(c);
	}
	if (!paid.length) {
		cake = o.paid_at && o.total_minor != null ? o.total_minor : 0;
		delivery = o.delivery?.paid_minor ?? 0;
	}
	const r = o.refund;
	return {
		paidMinor: cake + delivery,
		cakeMinor: cake,
		deliveryMinor: delivery,
		strayMinor: stray,
		alreadyMinor: r ? (r.succeeded_minor ?? 0) + (r.in_progress_minor ?? 0) : 0,
		deliveryAutomatic: Boolean(
			o.delivery_choice?.not_possible || o.delivery_failure?.cause === 'our_side'
		),
		currency: o.currency ?? 'GBP'
	};
}

/** The preview's lines (r03b2: "When you cancel, the customer gets 100% of everything paid"). */
export function cancelPreviewLines(o: StaffOrderDetail, p: CancelPreview): string[] {
	const m = (n: number) => formatMoney(n, p.currency);
	if (!p.paidMinor && !p.strayMinor) {
		return [
			'Nothing was paid for this order, so nothing is refunded. The day place is released and the customer is told by email from “GK Cakes”.'
		];
	}
	const lines = [
		`When you cancel, the customer gets 100% of everything paid: ${m(p.paidMinor)}. The customer is told by email from “GK Cakes”; no confirmation from them is needed.`
	];
	if (p.cakeMinor) {
		lines.push(
			`Cake ${m(p.cakeMinor)}: sent back to their original payment automatically once the cancellation is confirmed.`
		);
	}
	if (p.deliveryMinor) {
		lines.push(
			p.deliveryAutomatic
				? `Delivery ${m(p.deliveryMinor)}: returned in full automatically, because GK couldn’t deliver.`
				: `Delivery ${m(p.deliveryMinor)} was paid separately: after cancelling it appears as your next step, “Delivery refund · your step”.`
		);
	}
	if (p.strayMinor) {
		lines.push(
			`A delivery payment of ${m(p.strayMinor)} that couldn’t be used is also returned in full.`
		);
	}
	if (p.alreadyMinor) {
		lines.push(
			`${m(p.alreadyMinor)} already refunded or on its way counts towards it; nothing is returned twice.`
		);
	}
	return lines;
}

// ------------------------------------------------------------- refunds

export interface RefundSource {
	id: string;
	kind: 'cake' | 'delivery' | 'stray';
	/** "Cake payment" … */
	label: string;
	paidMinor: number;
	roomMinor: number;
}

const SOURCE_LABEL = {
	cake: 'Cake payment',
	delivery: 'Delivery payment',
	stray: 'Unmatched delivery payment'
};

/** Succeeded payments that can still return money (paid − refunded − reserved > 0). */
export function refundSources(o: StaffOrderDetail): RefundSource[] {
	return (o.collections ?? [])
		.filter((c) => room(c) > 0)
		.map((c) => {
			const kind = collectionKind(o, c);
			return {
				id: c.id,
				kind,
				label: SOURCE_LABEL[kind],
				paidMinor: c.amount_minor ?? 0,
				roomMinor: room(c)
			};
		});
}

/** Lifecycles where an agreed partial refund belongs to this page (a paid-late order has its own card). */
const REFUNDABLE = ['confirmed', 'in_production', 'ready', 'completed', 'cancelled'];

export interface RefundChoice {
	/** The "Refund part of the payment…" button. */
	can: boolean;
	note: string;
	sources: RefundSource[];
	/** paid − refunded − reserved over the order (409 REFUND_LIMIT above it). */
	limitMinor: number;
}

/** The chef's agreed partial refund (README "Agreed exception"). */
export function refundChoice(o: StaffOrderDetail, view: StaffView): RefundChoice {
	// A payment whose last refund failed definitively waits for the manager
	// decision (reserve again or a verified transfer), not another refund.
	const sources =
		view === 'chef'
			? refundSources(o).filter((s) => latestOperation(o, s.id)?.state !== 'failed')
			: [];
	const limitMinor = sources.reduce((n, s) => n + s.roomMinor, 0);
	const none = (note = ''): RefundChoice => ({ can: false, note, sources, limitMinor });
	if (view !== 'chef' || !REFUNDABLE.includes(o.lifecycle) || !sources.length) return none();
	const block = refundBlock(o);
	return block ? none(block) : { can: true, note: '', sources, limitMinor };
}

export interface AllocationProblems {
	/** Per source ID: the field's problem ('' when fine). */
	fields: Record<string, string>;
	/** A problem with the whole (nothing entered, over the limit). */
	total: string;
	allocations: { collection_id: string; amount_minor: number }[];
	totalMinor: number;
}

/**
 * The typed amounts per payment as allocations (empty fields are left
 * out; each at least 1p and at most that payment's room; the total within
 * the order's limit). The API checks the same and refuses otherwise.
 */
export function allocationsFrom(
	sources: RefundSource[],
	typed: Record<string, string>,
	currency = 'GBP'
): AllocationProblems {
	const fields: Record<string, string> = {};
	const allocations: { collection_id: string; amount_minor: number }[] = [];
	let totalMinor = 0;
	for (const s of sources) {
		const text = (typed[s.id] ?? '').trim();
		fields[s.id] = '';
		if (!text) continue;
		const p = parseAmount(text);
		if ('error' in p) {
			fields[s.id] = p.error;
			continue;
		}
		if (p.minor === 0) continue;
		if (p.minor > s.roomMinor) {
			fields[s.id] =
				`At most ${formatMoney(s.roomMinor, currency)} can come back from the ${s.label.toLowerCase()}.`;
			continue;
		}
		allocations.push({ collection_id: s.id, amount_minor: p.minor });
		totalMinor += p.minor;
	}
	let total = '';
	const fieldError = Object.values(fields).some(Boolean);
	if (!fieldError && !allocations.length) total = 'Enter how much to refund.';
	else if (!fieldError && totalMinor > MAX_REFUND_MINOR) total = 'That amount is too large.';
	if (fieldError) {
		allocations.length = 0;
		totalMinor = 0;
	}
	return { fields, total, allocations, totalMinor };
}

export interface DeliveryShare {
	/** The "Refund delivery" button(s): one per delivery payment that can pay it. */
	collections: { id: string; roomMinor: number }[];
	pendingMinor: number;
	note: string;
}

/**
 * "Delivery refund · your step" (K05: delivery refunds are the chef's): the
 * share the goal still owes from a delivery payment that paid its quote
 * (delivery_pending_minor), sent with POST /delivery-refund.
 */
export function deliveryShare(o: StaffOrderDetail, view: StaffView): DeliveryShare {
	const pendingMinor =
		o.refund?.delivery_pending_minor ?? o.refund_goal?.delivery_pending_minor ?? 0;
	const empty = { collections: [], pendingMinor, note: '' };
	if (view !== 'chef' || pendingMinor <= 0) return empty;
	const candidates = (o.collections ?? []).filter(
		(c) => c.kind === 'delivery' && collectionKind(o, c) === 'delivery' && room(c) > 0
	);
	if (!candidates.length) return empty;
	if (unresolvedChange(o)) return { ...empty, note: BUSY_NOTE };
	if (reconciliationReasons(o).length) return { ...empty, note: RECONCILE_NOTE };
	const free = candidates.filter((c) => !refundInProgress(o, c.id));
	if (!free.length) return { ...empty, note: IN_PROGRESS_NOTE };
	return {
		collections: free.map((c) => ({ id: c.id, roomMinor: room(c) })),
		pendingMinor,
		note: ''
	};
}

export interface StrayReturn {
	collections: { id: string; amountMinor: number }[];
	note: string;
}

/** The default reason for returning a stray (the chef may edit it; staff only). */
export const STRAY_REASON = 'Returning a delivery payment that couldn’t be used for this order';

/**
 * An unmatched delivery payment (0035) the chef returns with the agreed
 * exception refund on that payment, in full (what is left of it).
 */
export function strayReturn(o: StaffOrderDetail, view: StaffView): StrayReturn {
	if (view !== 'chef') return { collections: [], note: '' };
	const strays = (o.collections ?? []).filter(
		(c) => collectionKind(o, c) === 'stray' && room(c) > 0
	);
	if (!strays.length) return { collections: [], note: '' };
	const block = refundBlock(o);
	if (block) return { collections: [], note: block };
	return { collections: strays.map((c) => ({ id: c.id, amountMinor: room(c) })), note: '' };
}

// ---------------------------------------------------- manager decisions

export type CaseKind =
	| 'quarantine'
	| 'refund_contradiction'
	| 'payment_contradiction'
	| 'refund_unknown'
	| 'refund_requires_action'
	| 'refund_failed'
	| 'manual_payout';

export type Tool =
	'match_or_explain' | 'review' | 'reconcile' | 'decide' | 're_reserve' | 'transfer';

export interface MatchCandidate {
	id: string;
	label: string;
}

export interface ManagerCase {
	kind: CaseKind;
	/** Stable for the page (the row the case is about). */
	key: string;
	title: string;
	lines: string[];
	/** The decisions this view may start now (chef only). */
	tools: Tool[];
	/** Why a decision isn't offered, or what happens next ('' when nothing to add). */
	note: string;
	observationId?: string;
	operationId?: string;
	collectionId: string;
	/** The amount the case is about, chef only. */
	amountMinor?: number;
	/** For a failed refund: what reserving the remainder again would send (at most). */
	remainderMinor?: number;
	candidates?: MatchCandidate[];
}

const OP_STATE: Record<string, string> = {
	reserved: 'reserved, nothing sent yet',
	submitting: 'being sent',
	pending: 'sent, result not confirmed',
	requires_action: 'waiting for an action at the provider',
	unknown: 'result unknown',
	succeeded: 'confirmed',
	failed: 'failed',
	cancelled: 'withdrawn'
};

// Stripe's refund failure reasons in plain words (others are left out).
const FAILURE_WORDS: Record<string, string> = {
	expired_or_canceled_card: 'The original card is closed or expired and can’t receive refunds.',
	lost_or_stolen_card: 'The original card was reported lost or stolen.',
	charge_for_pending_refund_disputed: 'The customer disputed the payment with their bank.',
	insufficient_funds: 'The payment provider’s balance couldn’t cover it.',
	declined: 'The customer’s bank declined it.',
	merchant_request: 'It was cancelled at the payment provider.'
};

const PROVIDER_WORDS: Record<string, string> = {
	succeeded: 'succeeded',
	pending: 'pending',
	failed: 'failed',
	canceled: 'cancelled',
	cancelled: 'cancelled',
	requires_action: 'waiting for an action'
};

/**
 * The manager cases on this order (r03b2 Needs attention, "Money · needs a
 * manager decision"), each with the decisions the API offers for it. The
 * chef sees amounts and tools; technical support the same cases without
 * amounts or tools (K00: no money authority).
 */
export function managerCases(o: StaffOrderDetail, view: StaffView): ManagerCase[] {
	if (view === 'fulfillment') return [];
	const chef = view === 'chef';
	const amt = (minor: number | undefined, cur?: string) =>
		chef && minor != null ? money(o, minor, cur) : '';
	const busy = unresolvedChange(o);
	const reconcile = reconciliationReasons(o).length > 0;
	const out: ManagerCase[] = [];
	const tools = (t: Tool[]) => (chef && !busy ? t : []);
	const busyNote = chef && busy ? BUSY_NOTE : '';
	const label = (id: string) => collectionLabel(o, id);

	for (const b of o.refund_observations ?? []) {
		if (!b.quarantined || b.resolved_at || !b.collection_id) continue;
		const a = amt(b.amount_minor, b.currency);
		const candidates: MatchCandidate[] = chef
			? ops(o)
					.filter(
						(r) =>
							r.collection_id === b.collection_id &&
							r.channel === 'provider' &&
							(b.amount_minor == null || r.amount_minor === b.amount_minor)
					)
					.sort((x, y) =>
						x.id === b.named_operation_id ? -1 : y.id === b.named_operation_id ? 1 : 0
					)
					.map((r) => ({
						id: r.id,
						label: `${amt(r.amount_minor, r.currency)} refund ${r.state === 'reserved' ? 'reserved' : 'sent'} ${when(r.created_at)} (${OP_STATE[r.state] ?? r.state})`
					}))
			: [];
		out.push({
			kind: 'quarantine',
			key: `obs-${b.id}`,
			title: 'Refund made outside GK Cakes',
			lines: [
				`${a ? `${a} was refunded` : 'A refund was made'} on the ${label(b.collection_id)} ${when(b.observed_at)}, not from this workspace${b.source === 'dashboard' ? ' (in the payment provider’s dashboard)' : ''}${b.status ? `; the provider reports it ${PROVIDER_WORDS[b.status] ?? b.status}` : ''}.`,
				'It counts as possibly returned, and no refund can be sent from here for that payment until it’s matched to a refund we sent or accepted as made outside GK Cakes. The order itself is unchanged.'
			],
			tools: tools(['match_or_explain']),
			note: busyNote,
			observationId: b.id,
			collectionId: b.collection_id,
			amountMinor: b.amount_minor,
			candidates
		});
	}

	for (const c of o.collections ?? []) {
		if (!c.mismatch_reason) continue;
		const decided = c.state === 'succeeded' || c.state === 'failed';
		const a = amt(c.amount_minor, c.currency);
		out.push({
			kind: 'payment_contradiction',
			key: `pay-${c.id}`,
			title: 'Payment doesn’t match the order',
			lines: [
				`The ${label(c.id)}${a ? ` of ${a}` : ''} ${c.state === 'succeeded' ? 'is recorded as paid' : c.state === 'failed' ? 'is recorded as failed' : 'has no confirmed result'}, but the payment provider’s evidence contradicts it.`,
				decided
					? 'Nothing was changed automatically. A manager reviews the provider’s records and clears it; the recorded result then stands.'
					: 'It isn’t accepted: the order is never confirmed on payment evidence that doesn’t match, and no second payment can start.'
			],
			tools: decided ? tools(['reconcile']) : [],
			note:
				busyNote ||
				(decided
					? ''
					: 'Reconciling a payment whose result is still unknown isn’t available in the workspace yet.'),
			collectionId: c.id,
			amountMinor: c.amount_minor
		});
	}

	for (const r of ops(o)) {
		const a = amt(r.amount_minor, r.currency);
		const what = `${a ? `The ${a}` : 'The'} ${r.channel === 'manual_alternative' ? 'verified transfer' : 'refund'} on the ${label(r.collection_id)}`;
		if (r.mismatch_reason) {
			out.push({
				kind: 'refund_contradiction',
				key: `rc-${r.id}`,
				title:
					r.state === 'succeeded'
						? 'Refund changed after it was confirmed'
						: 'Refund result contradicts the provider',
				lines: [
					`${what} was recorded as ${OP_STATE[r.state] ?? r.state}${r.settled_at ? ` ${when(r.settled_at)}` : ''}${r.provider_status ? `; the payment provider now reports it ${PROVIDER_WORDS[r.provider_status] ?? r.provider_status}` : '; the payment provider’s records now say otherwise'}.`,
					'Nothing is resent and nothing becomes owed automatically. No new refund for this payment until a manager reviews it.'
				],
				tools: tools(['review']),
				note: busyNote,
				operationId: r.id,
				collectionId: r.collection_id,
				amountMinor: r.amount_minor
			});
			continue;
		}
		if (r.channel === 'provider' && r.state === 'unknown') {
			const ended = r.window_ended === true;
			out.push({
				kind: 'refund_unknown',
				key: `ru-${r.id}`,
				title: ended ? 'Refund result unknown · needs a manager' : 'Refund result not confirmed',
				lines: [
					`${what} was sent ${when(r.created_at)}; its result is unknown. It still counts as on its way: nobody can send another refund for this payment while it may still succeed, and it never becomes owed again just because time passed.`
				],
				tools: ended ? tools(['decide']) : [],
				note:
					busyNote ||
					(ended
						? ''
						: 'It’s within its 23-hour retry window and is checked with the provider automatically. If it’s still unknown when the window ends, a manager decides it here.'),
				operationId: r.id,
				collectionId: r.collection_id,
				amountMinor: r.amount_minor
			});
			continue;
		}
		if (r.channel === 'provider' && r.state === 'requires_action') {
			out.push({
				kind: 'refund_requires_action',
				key: `ra-${r.id}`,
				title: 'Refund needs an action at the provider',
				lines: [
					`${what} sent ${when(r.created_at)} is waiting for an action the payment provider requires.`,
					'It counts as in progress: no other refund for this payment until it’s resolved.'
				],
				tools: [],
				note: chef
					? 'Follow it up in the payment provider’s dashboard: there’s no workspace tool for this yet.'
					: '',
				operationId: r.id,
				collectionId: r.collection_id,
				amountMinor: r.amount_minor
			});
			continue;
		}
		if (
			r.channel === 'manual_alternative' &&
			['reserved', 'pending', 'unknown'].includes(r.state)
		) {
			out.push({
				kind: 'manual_payout',
				key: `mp-${r.id}`,
				title:
					r.state === 'reserved'
						? 'Verified transfer reserved · not sent yet'
						: r.state === 'pending'
							? 'Verified transfer sent · not confirmed'
							: 'Verified transfer result unknown',
				lines: [
					r.state === 'unknown'
						? `${what}: its result is unknown. Nothing else can be paid back on this payment, by any route, until it’s recorded as confirmed or failed.`
						: `${what} is ${r.state === 'reserved' ? 'reserved: make the transfer, then record it here' : 'recorded as sent: record the result once the transfer is confirmed or fails'}. Nothing else can be paid back on this payment meanwhile.`
				],
				tools: [],
				note: busyNote,
				operationId: r.id,
				collectionId: r.collection_id,
				amountMinor: r.amount_minor
			});
		}
	}

	// A definitive failure whose remainder is still owed (attention kind
	// refund_failed_owed): the collection's latest operation, failed, no
	// contradiction, and the goal still needs money.
	const remaining = o.refund_goal?.remaining_minor;
	const owed = chef ? (remaining ?? 0) > 0 : o.refund?.state === 'needs_attention';
	if (owed) {
		const seen = new Set<string>();
		for (const r of ops(o)) {
			if (seen.has(r.collection_id)) continue;
			const last = latestOperation(o, r.collection_id);
			if (!last || last.state !== 'failed' || last.mismatch_reason) continue;
			seen.add(r.collection_id);
			const coll = (o.collections ?? []).find((c) => c.id === r.collection_id);
			const remainder = chef
				? Math.min(last.amount_minor ?? 0, coll ? room(coll) : 0, remaining ?? 0)
				: undefined;
			const provider = last.channel === 'provider';
			const a = amt(last.amount_minor, last.currency);
			const blocked = refundInProgress(o, last.collection_id) || reconcile;
			const t: Tool[] = provider ? ['re_reserve', 'transfer'] : ['transfer'];
			out.push({
				kind: 'refund_failed',
				key: `rf-${last.id}`,
				title: provider
					? 'Refund failed · won’t be resent automatically'
					: 'Verified transfer failed · still owed',
				lines: [
					provider
						? `The payment provider definitively refused the ${a ? `${a} ` : ''}refund on the ${label(last.collection_id)}${last.settled_at ? ` ${when(last.settled_at)}` : ''}.${last.failure_reason && FAILURE_WORDS[last.failure_reason] ? ` ${FAILURE_WORDS[last.failure_reason]}` : ''}`
						: `The verified transfer of ${a || 'the amount'} on the ${label(last.collection_id)} failed${last.settled_at ? ` ${when(last.settled_at)}` : ''}; the amount is owed again.`,
					provider
						? `It stays owed${remainder ? ` (${money(o, remainder)})` : ''}. A manager either reserves the remaining amount again for one new attempt, or arranges a verified transfer with evidence (K05 Q5). Never both, never automatically.`
						: `It stays owed${remainder ? ` (${money(o, remainder)})` : ''}. You can arrange another verified transfer with evidence.`
				],
				tools: blocked ? [] : tools(t),
				note:
					busyNote ||
					(chef && blocked
						? 'Resolve the other open case on this payment or order first (a refund may still be on its way, or something needs reconciling).'
						: ''),
				operationId: last.id,
				collectionId: last.collection_id,
				amountMinor: last.amount_minor,
				remainderMinor: remainder
			});
		}
	}
	return out;
}

/** A tool's button (r03b2 Needs attention wording). */
export const TOOL_LABEL: Record<Tool, string> = {
	match_or_explain: 'Match or explain…',
	review: 'Review…',
	reconcile: 'Reconcile…',
	decide: 'Decide the result…',
	re_reserve: 'Reserve the remainder again…',
	transfer: 'Arrange a verified transfer…'
};

// ------------------------------------------------- manual payouts (Q5)

export interface PayoutPreview {
	preview_id: string;
	order_id: string;
	order_version: number;
	collection_id: string;
	failed_refund_operation_id: string;
	amount_minor: number;
	currency: string;
	unavailable_evidence_ref: string;
	recipient_evidence_ref: string;
	computed_at: string;
	expires_at: string;
	preview_hash: string;
}

export interface Payout {
	refund_operation_id: string;
	collection_id: string;
	/** protecting until its reservation is secured (K11). */
	state:
		| 'protecting'
		| 'reserved'
		| 'pending'
		| 'unknown'
		| 'succeeded'
		| 'failed'
		| 'cancelled'
		| string;
	amount_minor: number;
	currency: string;
	unavailable_evidence_ref: string;
	instructions: {
		amount_minor: number;
		currency: string;
		recipient_evidence_ref: string;
		payment_reference: string;
	} | null;
	submitted_at: string | null;
	transfer_identity: string | null;
	decided_at: string | null;
	outcome_pending: boolean;
}

/** Whether the order has manual payouts worth listing (chef: any manual_alternative operation). */
export function hasPayouts(o: StaffOrderDetail, view: StaffView): boolean {
	return view === 'chef' && ops(o).some((r) => r.channel === 'manual_alternative');
}

/** GET /admin/orders/{id}/manual-payouts (chef). */
export async function payouts(api: StaffApi, o: StaffOrderDetail): Promise<Payout[]> {
	const r = await apiGet<{ items: Payout[] }>(`/api/v1/admin/orders/${o.id}/manual-payouts`, {
		baseUrl: api.baseUrl,
		token: api.token,
		fetch: api.fetch
	});
	return r.items ?? [];
}

/** POST …/manual-payouts/preview (no key: read-like; the header is ignored). */
export async function previewPayout(
	api: StaffApi,
	o: StaffOrderDetail,
	input: { collectionId: string; unavailable: string; recipient: string },
	key: string
): Promise<PayoutPreview> {
	return apiPost<PayoutPreview>(
		`/api/v1/admin/orders/${o.id}/manual-payouts/preview`,
		{
			collection_id: input.collectionId,
			unavailable_evidence_ref: input.unavailable.trim(),
			recipient_evidence_ref: input.recipient.trim()
		},
		{ baseUrl: api.baseUrl, token: api.token, fetch: api.fetch, idempotencyKey: key }
	);
}

export type PayoutOutcome = 'submitted' | 'succeeded' | 'failed' | 'unknown' | 'withdrawn';

/** The outcomes a payout's state allows (README "Outcomes"); none while one is being recorded. */
export function payoutOutcomes(p: Payout): PayoutOutcome[] {
	if (p.outcome_pending || !p.instructions) return [];
	switch (p.state) {
		case 'reserved':
			return ['submitted', 'withdrawn'];
		case 'pending':
			return ['succeeded', 'failed', 'unknown'];
		case 'unknown':
			return ['succeeded', 'failed'];
		default:
			return [];
	}
}

export const PAYOUT_STATE: Record<string, string> = {
	protecting: 'Being reserved · the transfer details appear once it’s secured',
	reserved: 'Reserved · transfer not sent yet',
	pending: 'Transfer sent · not confirmed',
	unknown: 'Transfer result unknown',
	succeeded: 'Transfer confirmed',
	failed: 'Transfer failed · owed again',
	cancelled: 'Withdrawn · nothing was paid'
};

export const OUTCOME_LABEL: Record<PayoutOutcome, string> = {
	submitted: 'I’ve sent the transfer',
	succeeded: 'The transfer arrived…',
	failed: 'The transfer failed…',
	unknown: 'Result unknown',
	withdrawn: 'Withdraw (nothing was paid)…'
};

/** The outcome form's submit button (distinct from the button that opened it). */
export const OUTCOME_SUBMIT: Record<PayoutOutcome, string> = {
	submitted: 'Record that it’s sent',
	succeeded: 'Record that it arrived',
	failed: 'Record that it failed',
	unknown: 'Record that the result is unknown',
	withdrawn: 'Withdraw the reservation'
};

/** Whether an outcome needs an evidence reference (and success also the transfer and amount). */
export function outcomeNeeds(outcome: PayoutOutcome): { evidence: boolean; transfer: boolean } {
	return {
		evidence: outcome === 'succeeded' || outcome === 'failed' || outcome === 'withdrawn',
		transfer: outcome === 'succeeded'
	};
}

// ------------------------------------------------------------ commands

export type MoneyAction =
	| 'cancel'
	| 'refund'
	| 'stray'
	| 'delivery_refund'
	| 'resolve'
	| 'decide'
	| 'clear_refund'
	| 'clear_payment'
	| 're_reserve'
	| 'payout'
	| 'payout_outcome';

export type MoneyInput =
	| { m: 'cancel'; reason: string; amountMinor: number }
	| {
			m: 'refund' | 'stray';
			allocations: { collection_id: string; amount_minor: number }[];
			reason: string;
	  }
	| { m: 'delivery_refund'; collectionId: string; amountMinor: number }
	| {
			m: 'resolve';
			observationId: string;
			resolution: 'match' | 'accept_external';
			operationId?: string;
			note: string;
	  }
	| {
			m: 'decide';
			operationId: string;
			decision: 'failed' | 'succeeded';
			refundId?: string;
			note: string;
	  }
	| {
			m: 'clear_refund';
			operationId: string;
			decision: 'keep' | 'failed' | 'succeeded';
			note: string;
	  }
	| { m: 'clear_payment'; collectionId: string; note: string }
	| { m: 're_reserve'; operationId: string; collectionId: string; note: string }
	| { m: 'payout'; preview: PayoutPreview }
	| {
			m: 'payout_outcome';
			operationId: string;
			outcome: PayoutOutcome;
			transferIdentity?: string;
			evidence?: string;
			amountMinor?: number;
	  };

/**
 * How the API answers: `sync` 200 (a recorded decision), `refund` 202
 * refund_pending (reserved now, dispatched next), `command` 202 for a K11
 * order command (cancellation, a manual payout or its outcome).
 */
export type MoneyMode = 'sync' | 'refund' | 'command';

/** The route and exact body (strict JSON: no nulls, no extras) at the order's version. */
export function moneyRequest(
	o: StaffOrderDetail,
	input: MoneyInput
): { path: string; body: Record<string, unknown>; mode: MoneyMode } {
	const base = `/api/v1/admin/orders/${o.id}`;
	const v = { expected_version: o.version };
	switch (input.m) {
		case 'cancel':
			return {
				path: `${base}/cancel`,
				body: { ...v, reason: input.reason.trim() },
				mode: 'command'
			};
		case 'refund':
		case 'stray': {
			const amount = input.allocations.reduce((n, a) => n + a.amount_minor, 0);
			return {
				path: `${base}/refunds`,
				body: {
					...v,
					amount_minor: amount,
					currency: 'GBP',
					allocations: input.allocations.map((a) => ({
						collection_id: a.collection_id,
						amount_minor: a.amount_minor
					})),
					reason: input.reason.trim()
				},
				mode: 'refund'
			};
		}
		case 'delivery_refund':
			return {
				path: `${base}/delivery-refund`,
				body: { ...v, collection_id: input.collectionId },
				mode: 'refund'
			};
		case 'resolve':
			return {
				path: `${base}/refund-observations/${input.observationId}/resolve`,
				body:
					input.resolution === 'match'
						? {
								...v,
								resolution: 'match',
								refund_operation_id: input.operationId ?? '',
								note: input.note.trim()
							}
						: { ...v, resolution: 'accept_external', note: input.note.trim() },
				mode: 'sync'
			};
		case 'decide':
			return {
				path: `${base}/refund-operations/${input.operationId}/decide`,
				body:
					input.decision === 'succeeded'
						? {
								...v,
								decision: 'succeeded',
								provider_refund_id: (input.refundId ?? '').trim(),
								note: input.note.trim()
							}
						: { ...v, decision: 'failed', note: input.note.trim() },
				mode: 'sync'
			};
		case 'clear_refund':
			return {
				path: `${base}/refund-operations/${input.operationId}/clear-mismatch`,
				body: { ...v, decision: input.decision, note: input.note.trim() },
				mode: 'sync'
			};
		case 'clear_payment':
			return {
				path: `${base}/payment-collections/${input.collectionId}/clear-mismatch`,
				body: { ...v, note: input.note.trim() },
				mode: 'sync'
			};
		case 're_reserve':
			return {
				path: `${base}/refund-operations/${input.operationId}/re-reserve`,
				body: input.note.trim() ? { ...v, note: input.note.trim() } : { ...v },
				mode: 'refund'
			};
		case 'payout':
			return {
				path: `${base}/manual-payouts`,
				body: {
					expected_version: input.preview.order_version,
					preview_id: input.preview.preview_id,
					preview_hash: input.preview.preview_hash
				},
				mode: 'command'
			};
		case 'payout_outcome': {
			const body: Record<string, unknown> = { ...v, outcome: input.outcome };
			const need = outcomeNeeds(input.outcome);
			if (need.transfer) {
				body.transfer_identity = (input.transferIdentity ?? '').trim();
				body.amount_minor = input.amountMinor ?? 0;
				body.currency = 'GBP';
			}
			if (need.evidence) body.evidence_ref = (input.evidence ?? '').trim();
			return {
				path: `${base}/manual-payouts/${input.operationId}/outcome`,
				body,
				mode: 'command'
			};
		}
	}
}

/**
 * The key slot of one request: the action, the version and exactly what is
 * sent. A retry of the same request reuses its key; anything edited is a
 * new request with a new key (the API refuses an old key with another body:
 * KEY_REUSED).
 */
export function moneySlot(o: StaffOrderDetail, input: MoneyInput): string {
	const { path, body } = moneyRequest(o, input);
	const rest = { ...body };
	delete rest.expected_version;
	return `${input.m}:${o.version}:${path}:${JSON.stringify(rest)}`;
}

/** K00 command key: the action, the order, its version and a nonce. */
export function moneyKey(o: StaffOrderDetail, m: MoneyAction, nonce: string): string {
	return `chef-${m.replace(/_/g, '-')}-${o.id}-v${o.version}-${nonce}`;
}

export type MoneyAnswer =
	{ kind: 'done' } | { kind: 'accepted'; mode: 'refund' | 'command'; accepted: Accepted };

/** Sends one money command with its key. */
export async function sendMoney(
	api: StaffApi,
	o: StaffOrderDetail,
	input: MoneyInput,
	key: string
): Promise<MoneyAnswer> {
	const { path, body, mode } = moneyRequest(o, input);
	const r = await apiPost<Partial<Accepted>>(path, body, {
		baseUrl: api.baseUrl,
		token: api.token,
		fetch: api.fetch,
		idempotencyKey: key
	});
	if (mode === 'sync') return { kind: 'done' };
	return {
		kind: 'accepted',
		mode,
		accepted: {
			order_id: r?.order_id ?? o.id,
			operation_id: r?.operation_id ?? '',
			state: r?.state ?? '',
			poll_after_seconds: r?.poll_after_seconds ?? 3
		}
	};
}

/** The collections a refund command reserves on (to watch their new operations). */
export function targetCollections(input: MoneyInput): string[] {
	switch (input.m) {
		case 'refund':
		case 'stray':
			return input.allocations.map((a) => a.collection_id);
		case 'delivery_refund':
			return [input.collectionId];
		case 're_reserve':
			return [input.collectionId];
		default:
			return [];
	}
}

export type MoneyOutcome =
	| 'pending'
	| 'done'
	| 'refused'
	/** Refund outcomes: reserved (nothing sent yet), sent, confirmed or failed. */
	| 'reserved'
	| 'sent'
	| 'succeeded'
	| 'failed';

/**
 * Where an accepted money command stands in a fresh read. A cancellation:
 * done once the order is cancelled, refused when its command was refused.
 * A manual payout or its outcome: its K11 command, secured or refused. A
 * refund: the new operations on its collections (those not in `before`),
 * reserved while nothing is sent yet, then sent, confirmed or failed.
 */
export function moneySettled(
	o: StaffOrderDetail,
	input: MoneyInput,
	operationId: string,
	before: string[] = []
): MoneyOutcome {
	if (input.m === 'cancel') {
		const cmd = (o.commands ?? []).find((c) => c.id === operationId);
		if (cmd && cmd.state !== 'secured') return 'pending';
		if (o.lifecycle === 'cancellation_pending') return 'pending';
		if (o.lifecycle === 'cancelled') return 'done';
		if (cmd?.outcome_status === 'refused' || o.cancellation?.state === 'refused') return 'refused';
		return 'pending';
	}
	if (input.m === 'payout' || input.m === 'payout_outcome') {
		const cmd = (o.commands ?? []).find((c) => c.id === operationId);
		if (!cmd || cmd.state !== 'secured') return 'pending';
		return cmd.outcome_status === 'refused' ? 'refused' : 'done';
	}
	const targets = targetCollections(input);
	if (!targets.length) return 'done';
	const fresh = ops(o).filter((r) => targets.includes(r.collection_id) && !before.includes(r.id));
	if (!fresh.length) return 'pending';
	if (fresh.some((r) => r.state === 'reserved' || r.state === 'submitting')) return 'reserved';
	if (fresh.some((r) => r.state === 'failed')) return 'failed';
	if (fresh.every((r) => r.state === 'succeeded')) return 'succeeded';
	return 'sent';
}

/**
 * Polls the order for an accepted money command (every poll_after_seconds,
 * at most `tries` reads). A refund stops once something was sent (or it
 * settled); a K11 command once it is secured.
 */
export async function pollMoney(
	read: () => Promise<StaffOrderDetail>,
	input: MoneyInput,
	accepted: Pick<Accepted, 'operation_id' | 'poll_after_seconds'>,
	opts: {
		before?: string[];
		tries?: number;
		wait?: (ms: number) => Promise<void>;
		active?: () => boolean;
	} = {}
): Promise<{ order: StaffOrderDetail | null; outcome: MoneyOutcome }> {
	const tries = opts.tries ?? 20;
	const wait = opts.wait ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));
	const every = Math.max(1, Math.min(accepted.poll_after_seconds || 3, 10)) * 1000;
	let last: StaffOrderDetail | null = null;
	let outcome: MoneyOutcome = 'pending';
	for (let i = 0; i < tries; i++) {
		await wait(every);
		if (opts.active && !opts.active()) break;
		try {
			last = await read();
		} catch (e) {
			if (e instanceof ApiError && e.signInRequired) throw e;
			continue;
		}
		outcome = moneySettled(last, input, accepted.operation_id, opts.before);
		if (outcome !== 'pending' && outcome !== 'reserved') return { order: last, outcome };
	}
	return { order: last, outcome };
}

const ONLY_CHEF = 'Only Elina (chef) can do this.';

/** What the page says once a money command is done (or how far it got). */
export function moneyDone(
	o: StaffOrderDetail,
	input: MoneyInput,
	outcome: MoneyOutcome = 'done'
): string {
	const sum = (n: number) => money(o, n);
	const refundAmount =
		input.m === 'refund' || input.m === 'stray'
			? input.allocations.reduce((n, a) => n + a.amount_minor, 0)
			: input.m === 'delivery_refund'
				? input.amountMinor
				: 0;
	const refundWords = (what: string) => {
		switch (outcome) {
			case 'succeeded':
				return `Refunded ✓ · the payment provider confirmed ${what}.`;
			case 'sent':
				return `Sent ✓ · ${what} is with the payment provider. It shows as confirmed once they confirm.`;
			case 'failed':
				return `The payment provider refused ${what}. It stays owed: see Needs a manager.`;
			default:
				return `Reserved ✓ · ${what} goes to the payment provider next. The refund shows “not confirmed” until they confirm.`;
		}
	};
	switch (input.m) {
		case 'cancel':
			return outcome === 'done'
				? `Cancelled ✓ · the customer is told by email and ${input.amountMinor ? `${sum(input.amountMinor)} (everything paid) is being returned` : 'nothing was paid, so nothing is returned'}. See Refund for what has gone back.`
				: 'Recorded ✓';
		case 'refund':
			return refundWords(refundAmount ? `the refund of ${sum(refundAmount)}` : 'the refund');
		case 'stray':
			return refundWords(
				refundAmount ? `the ${sum(refundAmount)} delivery payment` : 'the delivery payment'
			);
		case 'delivery_refund':
			return refundWords(
				refundAmount ? `the delivery refund of ${sum(refundAmount)}` : 'the delivery refund'
			);
		case 're_reserve':
			return refundWords('the remainder');
		case 'resolve':
			return input.resolution === 'match'
				? 'Matched ✓ · the outside refund is counted as that refund’s result. Refunds for this payment are no longer blocked by it.'
				: 'Accepted ✓ · the outside refund is kept and counted as returned (by its own status). Nothing new was sent.';
		case 'decide':
			return input.decision === 'failed'
				? 'Decided ✓ · the refund is recorded as not made; what it would have returned is owed again. Nothing was sent.'
				: 'Recorded ✓ · the refund ID is checked with the payment provider; the refund shows as confirmed once it matches.';
		case 'clear_refund':
			return input.decision === 'keep'
				? 'Reviewed ✓ · the recorded result stands. Refunds for this payment are no longer blocked by it.'
				: input.decision === 'failed'
					? 'Reviewed ✓ · the refund is recorded as failed; what it would have returned is owed again. Nothing was resent.'
					: 'Reviewed ✓ · the refund is recorded as confirmed.';
		case 'clear_payment':
			return 'Reconciled ✓ · the recorded payment result stands and the mismatch is cleared.';
		case 'payout':
			return outcome === 'done'
				? 'Reserved ✓ · the transfer details are below. Make the transfer, then record it here.'
				: 'Recorded ✓';
		case 'payout_outcome':
			return outcome === 'done' ? PAYOUT_DONE[input.outcome] : 'Recorded ✓';
	}
}

const PAYOUT_DONE: Record<PayoutOutcome, string> = {
	submitted: 'Recorded ✓ · the transfer is marked as sent. Record the result once it’s confirmed.',
	succeeded: 'Recorded ✓ · the transfer is confirmed and counted as returned.',
	failed: 'Recorded ✓ · the transfer failed and the amount is owed again.',
	unknown:
		'Recorded ✓ · the result is unknown; nothing else can be paid on this payment until it’s settled.',
	withdrawn: 'Withdrawn ✓ · nothing was paid; the amount is owed again.'
};

export const MONEY_REFUSED =
	'It wasn’t recorded: the order changed before it could be. The latest version is shown; check it and try again if it’s still right.';

/** Refusals in plain words for this slice's commands (api/README codes). */
export function moneyRefusal(e: unknown, m: MoneyAction | 'preview'): Refusal {
	const base = refusalOf(e, 'ready');
	if (!(e instanceof ApiError)) return base;
	const r = (text: string, over: Partial<Refusal> = {}): Refusal => ({
		...base,
		text,
		reload: true,
		...over
	});
	switch (e.code) {
		case 'CONFLICT':
			return r(
				m === 'cancel'
					? 'It can’t be cancelled now: a cancellation or another change to this order is still being processed. The latest version is shown.'
					: m === 're_reserve'
						? 'That refund was already followed by another attempt, or another change to this order is still being processed. The latest version is shown.'
						: m === 'delivery_refund'
							? 'This isn’t a paid delivery payment of the order, or another change to this order is still being processed. The latest version is shown.'
							: 'Another change to this order is still being processed (for example a cancellation or a decision). Wait a moment and check the order again.'
			);
		case 'CANCELLATION_NOT_ELIGIBLE':
			return r(
				'This order can’t be cancelled as it is now (it may already be cancelled, completed, or not confirmed). The latest version is shown.'
			);
		case 'REFUND_LIMIT':
			return r(
				'That’s more than can still come back: the order (or that payment) has less left after what was refunded or is on its way. The latest figures are shown.'
			);
		case 'RECONCILIATION_REQUIRED':
			return r(
				'A refund can’t be sent while a manager case on this order is open (an outside refund, a changed result, a refund waiting at the provider or an unknown payment). Resolve it first under Needs a manager.'
			);
		case 'REFUND_IN_PROGRESS':
			return r(
				'A refund for this payment is still on its way. Another can be sent once its result is known: we never send a second refund while one may still succeed.'
			);
		case 'NO_REFUND_PENDING':
			return r('Nothing is owed from that payment any more. The latest figures are shown.');
		case 'IDENTITY_UNPROVEN':
			return r(
				'That outside refund can’t be matched to this refund: the provider’s records don’t prove it’s the same one (amount, payment or refund ID differ, or it’s already matched). Accept it as made outside GK Cakes instead if that’s what happened.',
				{ reload: false }
			);
		case 'WINDOW_OPEN':
			return r(
				'This refund is still within its retry window, so it can’t be decided yet; it’s checked with the provider automatically. Try again once the window has ended.'
			);
		case 'EVIDENCE_REQUIRED':
			return r(
				m === 'decide'
					? 'It can’t be recorded as not made: that needs a complete check with the payment provider after the retry window that found nothing, and no sign of the refund anywhere. Wait for the next check, or record it as confirmed with its refund ID.'
					: 'The payment provider’s records don’t support that result for this refund. Keep the recorded result, or choose the result the provider shows.',
				{ reload: false }
			);
		case 'DUPLICATE':
			return r(
				m === 'payout_outcome'
					? 'That transfer reference is already recorded for another verified transfer. Check it and enter this transfer’s own reference.'
					: 'That refund ID belongs to another refund or another payment. Check it in the payment provider’s dashboard.',
				{ reload: false }
			);
		case 'MANUAL_PAYOUT_NOT_ELIGIBLE':
			return r(
				'A verified transfer isn’t possible for this payment now: it needs the provider to have definitively refused the refund, nothing else on its way and nothing to reconcile. The latest version is shown.'
			);
		case 'PREVIEW_STALE':
			return r('This preview has expired. Preview the transfer again.');
		case 'FORBIDDEN':
			return r(ONLY_CHEF, { reload: false });
		case 'NOT_FOUND':
			return r(
				'That payment or refund wasn’t found on this order any more. The latest version is shown.'
			);
		case 'INVALID_INPUT':
			return r(INVALID[m], { reload: false });
	}
	return base;
}

const INVALID: Record<MoneyAction | 'preview', string> = {
	cancel: 'Check the reason: one line, up to 500 characters.',
	refund:
		'Check the amounts (whole pence, each at least 1p and adding up to the total) and the reason (one line, up to 500 characters).',
	stray: 'Check the reason: one line, up to 500 characters.',
	delivery_refund: 'This couldn’t be sent as it is. Reload the page and try again.',
	resolve: 'Check your note: one line, up to 500 characters.',
	decide:
		'Check the refund ID (it starts with re_) and your note (one line, up to 500 characters).',
	clear_refund: 'Check your note: one line, up to 500 characters.',
	clear_payment: 'Check your note: one line, up to 500 characters.',
	re_reserve: 'Check your note: one line, up to 500 characters.',
	payout: 'This couldn’t be sent as it is. Preview the transfer again.',
	payout_outcome:
		'Check the references (no bank details: no six or more digits in a row, nothing like an IBAN) and that the amount is exactly the reserved amount.',
	preview:
		'Check the references: up to 200 plain characters each, and never bank details (no six or more digits in a row, nothing like an IBAN).'
};
