// How the account shows a request to add an older order (approved GK-016
// r02b sketch): what the customer asked and its state, never the chef's note
// or any order detail before approval (K00: a decline reveals nothing).
import type { Claim } from './client';
import type { ChipTone } from './orders';

export interface ClaimView {
	ref: string;
	title: string;
	chip: { tone: ChipTone; text: string };
	line: string;
	/** Still waiting for the chef (may be withdrawn). */
	pending: boolean;
}

const CHANNEL: Record<string, string> = {
	instagram: 'Instagram order',
	offline: 'Order made in person or by phone',
	website_email_link: 'Website order'
};

const MONTHS = [
	'January',
	'February',
	'March',
	'April',
	'May',
	'June',
	'July',
	'August',
	'September',
	'October',
	'November',
	'December'
];

/** "June 2026" from YYYY-MM or YYYY-MM-DD; anything else as given. */
export function approximateLabel(value: string | null): string | undefined {
	if (!value) return undefined;
	const m = /^(\d{4})-(\d{2})(?:-\d{2})?$/.exec(value);
	if (!m || Number(m[2]) < 1 || Number(m[2]) > 12) return value;
	return `${MONTHS[Number(m[2]) - 1]} ${m[1]}`;
}

/** The short reference of a request (the start of its ID). */
export function claimRef(c: { id: string }): string {
	return c.id.slice(0, 8);
}

export function claimView(c: Claim): ClaimView {
	const emailLink = c.path === 'email_link';
	const when = approximateLabel(c.approximate_date);
	const title = emailLink
		? 'Add an order to your account'
		: `${CHANNEL[c.channel] ?? 'Older order'}${when ? ` · ${when}` : ''}`;
	const ref = claimRef(c);
	switch (c.state) {
		case 'pending':
			return {
				ref,
				title,
				chip: { tone: 'wait', text: 'Waiting for the chef' },
				line: emailLink
					? 'You asked from the link in the order email. Until the chef has checked it, keep using that link to see the order.'
					: 'You asked to add an older order. Nothing is added, and no order details are shown, until the chef has checked it.',
				pending: true
			};
		case 'approved':
			return {
				ref,
				title,
				chip: { tone: 'ok', text: 'Added to your account' },
				line: 'The chef checked it and added the order to your account.',
				pending: false
			};
		case 'declined':
			return {
				ref,
				title,
				chip: { tone: 'done', text: 'Couldn’t add' },
				line: 'The chef couldn’t add an order from the details given, so nothing was added. Please contact us if you think this is a mistake.',
				pending: false
			};
		case 'withdrawn':
			return {
				ref,
				title,
				chip: { tone: 'done', text: 'Withdrawn' },
				line: 'You withdrew this request. Nothing was added.',
				pending: false
			};
		default:
			return {
				ref,
				title,
				chip: { tone: 'info', text: 'Status not available' },
				line: 'We can’t show this request’s status right now. Please refresh later.',
				pending: false
			};
	}
}
