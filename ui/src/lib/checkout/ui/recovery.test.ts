import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import type { OrderRecoveryApi } from '../client';
import { ActionKeys, type Who } from '../flow';
import type { ChoiceCard } from '../recovery';
import type { Order } from '../types';
import DeliveryChoice from './DeliveryChoice.svelte';
import LateAnswer from './LateAnswer.svelte';

const text = (html: string) =>
	html
		.replace(/<!--[^]*?-->/g, '')
		.replace(/<[^>]+>/g, ' ')
		.replace(/\s+/g, ' ')
		.trim();

const who: Who = { kind: 'guest', secret: 'a'.repeat(43) };
const api = {} as OrderRecoveryApi;
const order = { id: 'o', version: 4 } as Order;

describe('LateAnswer', () => {
	it('shows accept and decline with the record note', () => {
		const { body } = render(LateAnswer, {
			props: {
				order,
				who,
				api,
				keys: new ActionKeys(),
				answer: {
					offerId: 'f1',
					acceptLabel: 'Yes, confirm this date',
					declineLabel: 'No, refund everything · £90',
					note: 'We keep a record of your answer and when you gave it.'
				},
				onsent: () => undefined,
				onproblem: () => undefined
			}
		});
		const t = text(body);
		expect(t).toContain('Yes, confirm this date');
		expect(t).toContain('No, refund everything · £90');
		expect(t).toContain('We keep a record of your answer and when you gave it.');
	});

	it('without an acceptable offer, only the decline', () => {
		const { body } = render(LateAnswer, {
			props: {
				order,
				who,
				api,
				keys: new ActionKeys(),
				answer: { offerId: 'f1', declineLabel: 'No, refund everything · £90', note: 'n' },
				onsent: () => undefined,
				onproblem: () => undefined
			}
		});
		expect(body.match(/<button/g)).toHaveLength(1);
	});
});

describe('DeliveryChoice', () => {
	const card: ChoiceCard = {
		kind: 'failure',
		heading: 'What would you like to do?',
		options: [
			{
				value: 'redelivery',
				label: 'Deliver it again',
				note: 'Nothing more to pay.',
				needsTime: false
			},
			{
				value: 'collection',
				label: 'Collect it today',
				note: 'St Katharine Docks.',
				needsTime: true
			}
		],
		times: [],
		legends: {},
		noTimes: 'none',
		refusal: 'Your last choice couldn’t be made: that time is no longer available.'
	};

	it('native radios in a fieldset, the refusal first, one confirm button, no time list yet', () => {
		const { body } = render(DeliveryChoice, {
			props: {
				order,
				who,
				api,
				keys: new ActionKeys(),
				card,
				onsent: () => undefined,
				onrefresh: () => undefined
			}
		});
		expect(body.match(/type="radio"/g)).toHaveLength(2);
		expect(body).toContain('<fieldset class="oc-choices"');
		expect(body).toContain('<legend class="sr-only">Choose one</legend>');
		const t = text(body);
		expect(t.indexOf('Your last choice')).toBeLessThan(t.indexOf('Deliver it again'));
		expect(t).toContain('Confirm my choice');
		expect(body).not.toContain('oc-times');
	});
});
