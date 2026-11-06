// The sidebar's count of waiting account requests (chef only): the first
// waiting page, read once (there is no count route; "50+" when there are
// more). The requests page sets it from its own read, so it isn't read twice.
import { countLabel, staffClaims, type ClaimPage } from './claims';
import { staffApi } from './session';

class ClaimsWaiting {
	/** "3", "50+", or null when unknown (not read yet, or the read failed). */
	label = $state<string | null>(null);
	private loading = false;
	private known = false;
	private fromPage = false;

	/** The current page reads the first waiting page itself and will set() the count. */
	expect(): void {
		this.fromPage = true;
	}

	/** The page's own read failed: the count may be read again elsewhere. */
	release(): void {
		this.fromPage = false;
	}

	/** Reads the first waiting page unless a page already gave the count; failures may be retried. */
	async load(): Promise<void> {
		if (this.known || this.loading || this.fromPage) return;
		this.loading = true;
		try {
			const api = await staffApi();
			const page = api ? await staffClaims(api, 'pending') : null;
			if (page && !this.known) this.set(page);
		} catch {
			// The count stays unknown (no badge); the next page load tries again.
		} finally {
			this.loading = false;
		}
	}

	set(page: Pick<ClaimPage, 'claims' | 'next_cursor'>): void {
		this.known = true;
		this.label = countLabel(page);
	}
}

export const claimsWaiting = new ClaimsWaiting();
