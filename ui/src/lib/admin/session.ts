// The staff API as the signed-in staff member (browser only; private pages
// render a shell on the server and load everything here, K00).
import { PUBLIC_API_URL } from '$app/env/public';
import { currentSession } from '../auth';
import type { StaffApi } from './catalogue';

/** The staff API with this browser's session, or null when nobody is signed in. */
export async function staffApi(): Promise<StaffApi | null> {
	const session = await currentSession();
	if (!session || !PUBLIC_API_URL) return null;
	return { baseUrl: PUBLIC_API_URL, token: session.access_token };
}
