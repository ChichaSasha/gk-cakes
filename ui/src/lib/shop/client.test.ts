import { describe, expect, it, vi } from 'vitest';
import { ApiError } from '../api';
import { isId, settle, shopApi } from './client';
import { cake, ID } from './test-fixtures';

const json = (body: unknown, status = 200) =>
	new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });

describe('shopApi', () => {
	it('reads every catalogue page anonymously (no token, no cookies)', async () => {
		const f = vi
			.fn<typeof fetch>()
			.mockResolvedValueOnce(json({ cakes: [cake()], next_cursor: 'c2' }))
			.mockResolvedValueOnce(json({ cakes: [cake({ id: ID.made })] }));
		const cakes = await shopApi('https://api.test', f).cakes();
		expect(cakes.map((c) => c.id)).toEqual([ID.cake, ID.made]);
		expect(String(f.mock.calls[0][0])).toBe('https://api.test/api/v1/cakes?limit=50');
		expect(String(f.mock.calls[1][0])).toBe('https://api.test/api/v1/cakes?limit=50&cursor=c2');
		const init = f.mock.calls[0][1] as RequestInit;
		expect(init.credentials).toBe('omit');
		expect((init.headers as Record<string, string>).Authorization).toBeUndefined();
		expect(init.signal).toBeInstanceOf(AbortSignal);
	});

	it('reads one page for the newest cakes, and bounds a page load with one shared deadline', async () => {
		const f = vi.fn<typeof fetch>().mockResolvedValue(json({ cakes: [cake()], next_cursor: 'c2' }));
		const deadline = AbortSignal.timeout(1000);
		const cakes = await shopApi('https://api.test', f, deadline).cakes(4);
		expect(cakes).toHaveLength(1);
		expect(f).toHaveBeenCalledTimes(1);
		expect(String(f.mock.calls[0][0])).toBe('https://api.test/api/v1/cakes?limit=4');
		expect((f.mock.calls[0][1] as RequestInit).signal).toBe(deadline);
	});

	it('asks the schedule for a new purchase with the places needed', async () => {
		const f = vi.fn<typeof fetch>().mockResolvedValue(json({ slots: [] }));
		await shopApi('https://api.test', f).schedule('2026-11-13', 'customer_courier', 2);
		expect(String(f.mock.calls[0][0])).toBe(
			'https://api.test/api/v1/schedule?date=2026-11-13&fulfillment_mode=customer_courier&purpose=new_purchase&quantity=2'
		);
	});

	it('answers a malformed cake ID as not found without calling the API', async () => {
		const f = vi.fn<typeof fetch>();
		await expect(shopApi('https://api.test', f).cake('../admin')).rejects.toMatchObject({
			status: 404
		});
		expect(f).not.toHaveBeenCalled();
		expect(isId(ID.cake)).toBe(true);
	});

	it('reports a missing API setting as unavailable', async () => {
		await expect(shopApi(undefined, vi.fn()).readyCakes()).rejects.toMatchObject({
			status: 503,
			retryable: false
		});
	});
});

describe('settle', () => {
	it('turns failures into states the page can recover from', async () => {
		expect(await settle(Promise.resolve(1))).toEqual({ ok: true, data: 1 });
		expect(await settle(Promise.reject(new ApiError(404, { code: 'NOT_FOUND' })))).toMatchObject({
			ok: false,
			notFound: true
		});
		expect(
			await settle(Promise.reject(new ApiError(0, { code: 'NETWORK', retryable: true })))
		).toMatchObject({ ok: false, notFound: false, retryable: true });
		expect(await settle(Promise.reject(new Error('boom')))).toMatchObject({
			ok: false,
			retryable: true
		});
	});
});
