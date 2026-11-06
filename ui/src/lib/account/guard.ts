// Shared sign-in handling for the account pages (as /account and /admin
// already do): no session → sign in and come back; an ended session → sign
// out here and sign in again; signed out in another tab → leave, so no
// private data stays on screen.
import { goto } from '$app/navigation';
import { resolve } from '$app/paths';
import { ApiError } from '../api';
import { leaveOnSignOut, signOut } from '../auth';
import { forgetDeletion } from './privacy';

/** Where the account sends someone who must sign in (the sign-in page returns to /account). */
export function toSignIn(ended = false): Promise<void> {
	return goto(resolve(ended ? '/sign-in?next=/account&ended=1' : '/sign-in?next=/account'), {
		replaceState: true
	});
}

/**
 * Handles an API failure that means the session is over (401): signs out
 * here and goes to sign-in. Returns true when it did.
 */
let ending: Promise<void> | undefined;

export async function endedSession(e: unknown): Promise<boolean> {
	if (e instanceof ApiError && e.signInRequired) {
		// Several reads failing at once sign out and redirect only once.
		ending ??= (async () => {
			try {
				await signOut();
				await toSignIn(true);
			} finally {
				ending = undefined;
			}
		})();
		await ending;
		return true;
	}
	return false;
}

/** Whether a failed account read means the account itself can't be used (no point retrying). */
export function accountUnusable(e: unknown): boolean {
	return e instanceof ApiError && (e.code === 'ACCOUNT_DISABLED' || e.code === 'ACCOUNT_DELETED');
}

/** The words for an account read that failed (other than an ended session). */
export function accountProblem(e: unknown, what: string): string {
	if (e instanceof ApiError && e.code === 'ACCOUNT_DISABLED') {
		return 'This account is disabled. Please contact us if you think this is a mistake.';
	}
	if (e instanceof ApiError && e.code === 'ACCOUNT_DELETED') {
		return 'This account has been deleted.';
	}
	return `We couldn’t load ${what} right now. Please try again.`;
}

/** Leaves the page when this browser signs out elsewhere; returns the unsubscribe. */
export function leaveWhenSignedOut(clear: () => void): () => void {
	return leaveOnSignOut(() => {
		clear();
		void toSignIn();
	});
}

/**
 * The account pages' Sign out: forgets a deletion started on this browser
 * (it never outlives the sign-out), then signs out here. Returns whether the
 * sign-in service could be reached.
 */
export async function signOutHere(): Promise<boolean> {
	try {
		forgetDeletion(window.localStorage);
	} catch {
		// storage refused: nothing kept
	}
	return signOut();
}
