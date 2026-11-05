import { defineEnvVars } from '@sveltejs/kit/env';
import { origin, publishableKey } from './lib/settings';

// Public runtime settings (GK-021). Deliberately exposed to the browser: the
// API origin, the identity provider's project URL and its publishable key.
// Backend secrets never appear here. All three are optional so the server
// still starts (and /healthz answers) without them; sign-in then shows that
// it is not configured. Set in each environment's UI service.

export const variables = defineEnvVars({
	PUBLIC_API_URL: {
		public: true,
		description: 'Exact origin of the GK Cakes API, e.g. https://staging-api.gkcake.com',
		schema: origin('PUBLIC_API_URL')
	},
	PUBLIC_AUTH_URL: {
		public: true,
		description: "Identity provider's project URL, e.g. https://<project>.supabase.co",
		schema: origin('PUBLIC_AUTH_URL')
	},
	PUBLIC_AUTH_PUBLISHABLE_KEY: {
		public: true,
		description: "Identity provider's publishable key (sb_publishable_...), never a secret key",
		schema: publishableKey('PUBLIC_AUTH_PUBLISHABLE_KEY')
	}
});
