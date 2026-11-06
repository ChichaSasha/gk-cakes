// The sidebar's count of waiting account requests (chef only): the first
// waiting page, read once per page load (there is no count route; "50+"
// when there are more). The requests page sets it from its own read.
import { countLabel, staffClaims, type ClaimPage } from './claims';
import { staffApi } from './session';

class ClaimsWaiting {
	/** "3", "50+", or null when unknown (not read yet, or the read failed). */
	label = $state<string | null>(null);
	private started = false;

	/** Reads the first waiting page once; failures leave the count unknown. */
	async load(): Promise<void> {
		if (this.started) return;
		this.started = true;
		try {
			const api = await staffApi();
			if (!api) return;
			this.set(await staffClaims(api, 'pending'));
		} catch {
			this.label = null;
		}
	}

	set(page: Pick<ClaimPage, 'claims' | 'next_cursor'>): void {
		this.started = true;
		this.label = countLabel(page);
	}
}

export const claimsWaiting = new ClaimsWaiting();
