// The bag shared by the header, cake page and bag page, persisted in this
// tab's sessionStorage (see bag.ts). Other tabs keep their own bag. It
// starts empty (as the server renders it) and is read from storage once the
// page runs in the browser, so hydration matches the server's HTML.
import { browser } from '$app/env';
import { BAG_KEY, emptyBag, parseBag, type BagState } from './bag';

class Bag {
	state = $state<BagState>(emptyBag());
	/** True once the stored bag has been read in this browser. */
	loaded = $state(false);
	/** False when this browser could not keep the bag between pages. */
	kept = $state(true);

	/** Reads the stored bag (once; later calls do nothing). */
	load(): void {
		if (!browser || this.loaded) return;
		try {
			this.state = parseBag(sessionStorage.getItem(BAG_KEY));
		} catch {
			this.kept = false;
		}
		this.loaded = true;
	}

	set(next: BagState): void {
		this.load();
		this.state = next;
		try {
			sessionStorage.setItem(BAG_KEY, JSON.stringify(next));
			this.kept = true;
		} catch {
			this.kept = false;
		}
	}
}

export const bag = new Bag();
