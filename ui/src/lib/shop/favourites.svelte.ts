// The device favourites shared by every heart button and the favourites page
// (see favourites.ts). They start empty (as the server renders them) and are
// read from localStorage once the page runs in the browser, so hydration
// matches the server's HTML; other tabs' changes arrive through the storage
// event.
import { browser } from '$app/env';
import { emptyFavourites, FAVOURITES_KEY, parseFavourites, toggled } from './favourites';
import type { FavouritesState } from './favourites';

class Favourites {
	state = $state<FavouritesState>(emptyFavourites());
	/** True once the stored favourites have been read in this browser. */
	loaded = $state(false);

	/** Reads the stored favourites (once; later calls do nothing). */
	load(): void {
		if (!browser || this.loaded) return;
		this.state = this.read();
		this.loaded = true;
		window.addEventListener('storage', (e) => {
			if (e.key === FAVOURITES_KEY || e.key === null) this.state = this.read();
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
		const next = toggled(this.state, id, Date.now());
		try {
			if (next.ids.length === 0) localStorage.removeItem(FAVOURITES_KEY);
			else localStorage.setItem(FAVOURITES_KEY, JSON.stringify(next));
		} catch {
			return null;
		}
		this.state = next;
		return next.ids.includes(id);
	}

	private read(): FavouritesState {
		try {
			const raw = localStorage.getItem(FAVOURITES_KEY);
			const state = parseFavourites(raw, Date.now());
			// An expired or damaged entry is removed; reading never extends the year.
			if (raw !== null && state.ids.length === 0) localStorage.removeItem(FAVOURITES_KEY);
			return state;
		} catch {
			return emptyFavourites();
		}
	}
}

export const favourites = new Favourites();
