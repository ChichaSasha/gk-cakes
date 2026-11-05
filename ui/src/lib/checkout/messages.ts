// API refusals in the customer's words (GK-025, GK-017 r01). Each problem
// says what happened and what the page does next; codes follow the API's
// K00 error body (api/internal/httpapi catalogError).
import { ApiError } from '../api';

/** What the checkout does after a refusal. */
export type Next =
	| 'retry' // the same action may be tried again (same Idempotency-Key)
	| 'requote' // get a fresh total and ask the customer to review it
	| 'reload_policy' // show the current terms and ask for consent again
	| 'reload_slots' // the time went: choose another
	| 'reload_dates' // the date is now too soon: choose another
	| 'remove_code' // drop the discount code and requote
	| 'view_order' // a payment or change is already under way: go to the order
	| 'fix_input' // a field needs correcting
	| 'sign_in' // the signed-in session ended
	| 'start_again' // the basket can no longer be ordered as chosen
	| 'none';

export interface Problem {
	message: string;
	next: Next;
	/** The form field the problem belongs to, when there is one. */
	field?: 'code' | 'name' | 'email' | 'payer' | 'slot';
	/** A support reference (the API request ID) when the cause is unknown. */
	reference?: string;
}

const RETRY = 'We couldn’t reach our service just now. Nothing was charged. Please try again.';

const DISCOUNT: [string, string][] = [
	[
		'needs you to sign in',
		'This code is for signed-in customers. Sign in first, or continue without it.'
	],
	['applies to GK delivery only', 'This code is for GK delivery, which isn’t available online yet.']
];

/** The customer message and next step for a failed API call. */
export function problemFor(err: unknown): Problem {
	if (!(err instanceof ApiError)) {
		return { message: 'Something went wrong on this page. Please try again.', next: 'retry' };
	}
	switch (err.code) {
		case 'PRICE_CHANGED':
			return {
				message:
					'A price changed since your total was shown. We’ve updated it: please review the new total and tick the box again before paying.',
				next: 'requote'
			};
		case 'QUOTE_EXPIRED':
			return {
				message:
					'Your total was more than 24 hours old, so we’ve refreshed it. Please review it and tick the box again before paying.',
				next: 'requote'
			};
		case 'POLICY_CHANGED':
			return {
				message:
					'Our cancellation and refund terms have just been updated. Please read them and tick the box again.',
				next: 'reload_policy'
			};
		case 'SLOT_UNAVAILABLE':
			return {
				message: 'That time is no longer available. Please choose another time.',
				next: 'reload_slots',
				field: 'slot'
			};
		case 'LEAD_TIME':
			return {
				message:
					'This time is now too soon: we need at least 8 days (192 hours) from payment to make your cake. Please choose a later date.',
				next: 'reload_dates'
			};
		case 'ITEM_UNAVAILABLE':
			return {
				message:
					'A cake in your order is no longer available as chosen. Please go back and choose again.',
				next: 'start_again'
			};
		case 'CODE_USED_UP':
			return {
				message:
					'That discount code has just been used up, so we’ve removed it. Please review your new total.',
				next: 'remove_code',
				field: 'code'
			};
		case 'PAYMENT_IN_PROGRESS':
			return {
				message:
					'A payment for this order is already in progress. Please don’t pay again: check your order for the result.',
				next: 'view_order'
			};
		case 'PAYMENT_REQUIRED':
		case 'STALE_VERSION':
		case 'CONFLICT':
			return {
				message: 'This order has changed since this page loaded. Please check its current status.',
				next: 'view_order'
			};
		case 'INVALID_INPUT':
			return invalid(err);
	}
	if (err.status === 401) {
		return {
			message: 'Your session has ended. Please sign in again to continue.',
			next: 'sign_in'
		};
	}
	if (err.status === 0 || err.retryable || err.status >= 500) {
		return { message: RETRY, next: 'retry' };
	}
	return {
		message: 'Something went wrong. Please try again, or contact us with the reference below.',
		next: 'retry',
		reference: err.requestId || undefined
	};
}

function invalid(err: ApiError): Problem {
	const m = err.message;
	if (m.startsWith('discount_code')) {
		const known = DISCOUNT.find(([k]) => m.includes(k));
		return {
			message:
				known?.[1] ?? 'We don’t recognise that code. Check the spelling, or continue without one.',
			next: 'fix_input',
			field: 'code'
		};
	}
	if (m.startsWith('contact.email')) {
		return {
			message: 'Enter an email address like name@example.com.',
			next: 'fix_input',
			field: 'email'
		};
	}
	if (m.startsWith('contact.full_name')) {
		return { message: 'Enter your full name.', next: 'fix_input', field: 'name' };
	}
	if (m.startsWith('contact.payer_name')) {
		return { message: 'Enter the payer’s full name.', next: 'fix_input', field: 'payer' };
	}
	return {
		message: 'Something in this order couldn’t be accepted. Please check it and try again.',
		next: 'retry',
		reference: err.requestId || undefined
	};
}
