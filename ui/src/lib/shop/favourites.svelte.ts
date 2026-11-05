// The device favourites shared by every heart button and the favourites page
// (see favourites.ts). They start empty (as the server renders them) and are
// read from localStorage once the page runs in the browser, so hydration
// matches the server's HTML. They are read again when another tab changes
// them, when the page is shown again and before every change, so favourites
// that expired while a page stayed open never come back.
import { browser } from '$app/env';
import { emptyFavourites, FAVOURITES_KEY, readFavourites, storeToggle } from './favourites';
import type { DeviceStorage, FavouritesState } from './favourites';

/** This browser's localStorage, or null when even reaching it is refused. */
function device(): DeviceStorage | null {
	try {
		return window.localStorage;
	} catch {
		return null;
	}
}

class Favourites {
	state = $state<FavouritesState>(emptyFavourites());
	/** True once the stored favourites have been read in this browser. */
	loaded = $state(false);
	/** False when this browser does not let the page keep favourites. */
	kept = $state(true);

	/** Reads the stored favourites (once; later calls do nothing). */
	load(): void {
		if (!browser || this.loaded) return;
		this.refresh();
		this.loaded = true;
		window.addEventListener('storage', (e) => {
			if (e.key === FAVOURITES_KEY || e.key === null) this.refresh();
		});
		window.addEventListener('pageshow', () => this.refresh());
		document.addEventListener('visibilitychange', () => {
			if (document.visibilityState === 'visible') this.refresh();
		});
	}

	has(id: string): boolean {
		return this.state.ids.includes(id);
	}

	/**
	 * Saves or removes one cake. Returns the new saved state, or null when
	 * this browser could not keep the change (nothing changed then).
	 */
	toggle(id: string): boolean | null {
		this.load();
		const storage = device();
		const next = storage ? storeToggle(storage, id, Date.now()) : null;
		if (next === null) {
			this.kept = false;
			return null;
		}
		this.kept = true;
		this.state = next;
		return next.ids.includes(id);
	}

	private refresh(): void {
		const storage = device();
		const state = storage ? readFavourites(storage, Date.now()) : null;
		this.kept = state !== null;
		this.state = state ?? emptyFavourites();
	}
}

export const favourites = new Favourites();
