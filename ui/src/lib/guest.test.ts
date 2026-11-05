import { describe, expect, it } from 'vitest';
import {
	canKeepCapability,
	isGuestProof,
	saveOrderProblem,
	takeOrderProblem,
	lastOrder,
	newGuestProof,
	orderCapability,
	rememberLastOrder,
	saveOrderCapability
} from './guest';

function memoryStore(): Storage {
	const m = new Map<string, string>();
	return {
		get length() {
			return m.size;
		},
		clear: () => m.clear(),
		key: (i) => [...m.keys()][i] ?? null,
		getItem: (k) => m.get(k) ?? null,
		setItem: (k, v) => void m.set(k, v),
		removeItem: (k) => void m.delete(k)
	};
}

const order = '0b6c1b8e-6a0e-4c47-9a4e-2f1a7a3e5d10';

/** Decodes unpadded base64url as the API does (RawURLEncoding.Strict). */
function decode(s: string): Uint8Array {
	const b64 = s.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (s.length % 4)) % 4);
	return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
}

describe('newGuestProof', () => {
	it('is 32 random bytes in unpadded base64url (43 characters)', () => {
		const p = newGuestProof();
		expect(p).toMatch(/^[A-Za-z0-9_-]{43}$/);
		expect(decode(p)).toHaveLength(32);
		expect(newGuestProof()).not.toBe(p);
	});

	it('uses the URL-safe alphabet for every byte value', () => {
		const p = newGuestProof((b) => b.fill(0xff));
		expect(p).toBe('_'.repeat(42) + '8');
		expect(decode(p).every((b) => b === 0xff)).toBe(true);
		const z = newGuestProof((b) => b.fill(0xfb));
		expect(z).not.toMatch(/[+/=]/);
	});

	it('round-trips exact bytes', () => {
		const bytes = Uint8Array.from({ length: 32 }, (_, i) => i * 7);
		const p = newGuestProof((b) => {
			b.set(bytes);
			return b;
		});
		expect([...decode(p)]).toEqual([...bytes]);
	});
});

describe('order capabilities', () => {
	it('stores and reads a capability per order in the given store', () => {
		const s = memoryStore();
		const p = newGuestProof();
		expect(saveOrderCapability(order, p, s)).toBe(true);
		expect(orderCapability(order, s)).toBe(p);
		expect(orderCapability('1b6c1b8e-6a0e-4c47-9a4e-2f1a7a3e5d10', s)).toBeUndefined();
	});

	it('refuses malformed IDs and proofs and survives missing or failing storage', () => {
		const s = memoryStore();
		expect(saveOrderCapability('../x', newGuestProof(), s)).toBe(false);
		expect(saveOrderCapability(order, 'short', s)).toBe(false);
		expect(saveOrderCapability(order, newGuestProof(), null)).toBe(false);
		const failing = {
			...s,
			setItem: () => {
				throw new Error('quota');
			},
			getItem: () => {
				throw new Error('denied');
			}
		};
		expect(saveOrderCapability(order, newGuestProof(), failing as Storage)).toBe(false);
		expect(orderCapability(order, failing as Storage)).toBeUndefined();
		s.setItem('gk-order-capability:' + order, 'tampered');
		expect(orderCapability(order, s)).toBeUndefined();
	});

	it('remembers the last order of the tab', () => {
		const s = memoryStore();
		expect(lastOrder(s)).toBeUndefined();
		rememberLastOrder(order, s);
		expect(lastOrder(s)).toBe(order);
		rememberLastOrder('nope', s);
		expect(lastOrder(s)).toBe(order);
		expect(isGuestProof(newGuestProof())).toBe(true);
		expect(isGuestProof(null)).toBe(false);
	});
});

describe('tab storage checks and handed-over problems', () => {
	it('knows whether this tab can keep a capability', () => {
		expect(canKeepCapability(memoryStore())).toBe(true);
		expect(canKeepCapability(null)).toBe(false);
		const full = {
			...memoryStore(),
			setItem: () => {
				throw new Error('quota');
			}
		};
		expect(canKeepCapability(full as Storage)).toBe(false);
	});

	it('hands a problem to the order page once, without anything but its code', () => {
		const s = memoryStore();
		saveOrderProblem(
			order,
			{ status: 409, code: 'LEAD_TIME', retryable: false, request_id: 'r1' },
			s
		);
		expect(takeOrderProblem(order, s)).toEqual({
			status: 409,
			code: 'LEAD_TIME',
			retryable: false,
			request_id: 'r1'
		});
		expect(takeOrderProblem(order, s)).toBeUndefined();
		s.setItem('gk-order-problem:' + order, '{bad');
		expect(takeOrderProblem(order, s)).toBeUndefined();
	});
});
