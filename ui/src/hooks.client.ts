// Runs once when the app starts in the browser (GK-021): sign-in secrets are
// removed from the URL and history before any page loads data.
import type { ClientInit } from '@sveltejs/kit/hooks';
import { scrubbedUrl } from './lib/scrub';

export const init: ClientInit = () => {
	const clean = scrubbedUrl(location.href);
	if (clean !== null) history.replaceState(history.state, '', clean);
};
