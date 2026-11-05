// Guest secrets (GK-025, K02). A guest proves ownership of a quote, and then
// of the order made from it, with 32 random bytes in unpadded base64url (43
// characters, checkout.GuestProofDigest); the API stores only its SHA-256
// digest. Each new quote needs a fresh proof. The proof of the quote that
// became an order is that order's capability (read, pay, cancel). It is kept
// in this tab's sessionStorage only: never in a URL, log or analytics, and
// gone when the tab closes. Signed-in customers use their access token.

const ORDER_PREFIX = 'gk-order-capability:';
const LAST_ORDER = 'gk-last-order';
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const PROOF = /^[A-Za-z0-9_-]{43}$/;

/** 32 random bytes as unpadded base64url (43 characters). */
export function newGuestProof(
	random: (bytes: Uint8Array<ArrayBuffer>) => Uint8Array = (b) => crypto.getRandomValues(b)
): string {
	const bytes = random(new Uint8Array(32));
	let binary = '';
	for (const b of bytes) binary += String.fromCharCode(b);
	return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

/** Whether s has the shape of a guest proof. */
export function isGuestProof(s: unknown): s is string {
	return typeof s === 'string' && PROOF.test(s);
}

type Store = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

function tabStore(): Store | null {
	try {
		return globalThis.sessionStorage ?? null;
	} catch {
		return null; // storage disabled: the order page then asks to sign in or start again
	}
}

/** Keeps a guest order's capability in this tab. Returns false if it could not be stored. */
export function saveOrderCapability(orderId: string, proof: string, store = tabStore()): boolean {
	if (!UUID.test(orderId) || !isGuestProof(proof) || !store) return false;
	try {
		store.setItem(ORDER_PREFIX + orderId, proof);
		return true;
	} catch {
		return false;
	}
}

/** This tab's capability for a guest order, if any. */
export function orderCapability(orderId: string, store = tabStore()): string | undefined {
	if (!UUID.test(orderId) || !store) return undefined;
	try {
		const v = store.getItem(ORDER_PREFIX + orderId);
		return isGuestProof(v) ? v : undefined;
	} catch {
		return undefined;
	}
}

/** Remembers the last order started in this tab, so checkout can point back to it. */
export function rememberLastOrder(orderId: string, store = tabStore()): void {
	if (!UUID.test(orderId) || !store) return;
	try {
		store.setItem(LAST_ORDER, orderId);
	} catch {
		// best effort only
	}
}

/** The last order started in this tab, if any. */
export function lastOrder(store = tabStore()): string | undefined {
	try {
		const v = store?.getItem(LAST_ORDER);
		return v && UUID.test(v) ? v : undefined;
	} catch {
		return undefined;
	}
}
