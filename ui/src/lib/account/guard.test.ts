import { beforeEach, describe, expect, it, vi } from 'vitest';

const signOut = vi.fn(async () => true);
const goto = vi.fn(async () => undefined);
vi.mock('../auth', () => ({ signOut, leaveOnSignOut: () => () => undefined }));
vi.mock('$app/navigation', () => ({ goto }));
vi.mock('$app/paths', () => ({ resolve: (p: string) => p }));

const { ApiError } = await import('../api');
const { accountProblem, accountUnusable, endedSession } = await import('./guard');

describe('account guard', () => {
	beforeEach(() => {
		signOut.mockClear();
		goto.mockClear();
	});

	it('several ended-session failures at once sign out and redirect once', async () => {
		const e = new ApiError(401, { code: 'UNAUTHENTICATED' });
		const results = await Promise.all([endedSession(e), endedSession(e), endedSession(e)]);
		expect(results).toEqual([true, true, true]);
		expect(signOut).toHaveBeenCalledTimes(1);
		expect(goto).toHaveBeenCalledTimes(1);
		expect(goto).toHaveBeenCalledWith('/sign-in?next=/account&ended=1', { replaceState: true });
		// A later ended session (after signing in again) is handled again.
		await endedSession(e);
		expect(signOut).toHaveBeenCalledTimes(2);
	});

	it('other failures are not an ended session', async () => {
		expect(await endedSession(new ApiError(503, { code: 'UNAVAILABLE' }))).toBe(false);
		expect(await endedSession(new Error('x'))).toBe(false);
		expect(signOut).not.toHaveBeenCalled();
	});

	it('words account problems and tells unusable accounts apart', () => {
		expect(accountProblem(new ApiError(403, { code: 'ACCOUNT_DISABLED' }), 'x')).toMatch(
			/disabled/
		);
		expect(accountProblem(new ApiError(403, { code: 'ACCOUNT_DELETED' }), 'x')).toMatch(/deleted/);
		expect(accountProblem(new ApiError(503, {}), 'your orders')).toBe(
			'We couldn’t load your orders right now. Please try again.'
		);
		expect(accountUnusable(new ApiError(403, { code: 'ACCOUNT_DISABLED' }))).toBe(true);
		expect(accountUnusable(new ApiError(503, {}))).toBe(false);
	});
});
