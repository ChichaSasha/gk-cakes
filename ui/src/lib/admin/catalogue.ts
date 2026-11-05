// The chef's catalogue in the staff API (GK-018 groundwork; api/README
// "Catalog", api/internal/httpapi/catalog.go): typed reads and commands with
// the exact JSON the API accepts (strict JSON: no unknown fields, nulls only
// where documented). Reads are open to the chef and technical support; every
// command needs the chef capability, an Idempotency-Key (one per user action,
// reused on retry) and, for an existing object, its expected_version.
// No screens are built on this yet: the GK-018 design is not approved.
import { apiGet, apiPatch, apiPost, type ApiOptions } from '../api';

export type CakeState = 'draft' | 'active' | 'archived';
export type StockMode = 'made_to_order' | 'ready_stock';
export type Unit = 'g' | 'ml' | 'count';

export interface Size {
	code: string;
	label: string;
	diameter: string;
}

export interface Allergen {
	code: string;
	label: string;
	prominent: boolean;
}

export interface Notice {
	id: string;
	text: string;
	created_at?: string;
}

export interface Recipe {
	recipe_version_id: string;
	revision: number;
	price_minor: number;
	currency: string;
	servings: { min: number; max: number };
	coating: string;
	composition: string[];
	allergens: Allergen[];
	allergen_notice: Notice;
}

export interface RecipeLine {
	component: string;
	quantity: string;
	unit: Unit;
}

/** One immutable recipe version (what an order pins). */
export interface RecipeVersion extends Recipe {
	variant_id: string;
	cake_id: string;
	flavour: string;
	size: Size;
	lines: RecipeLine[];
	source_sha256: string;
	created_at: string;
}

export interface StaffVariant {
	id: string;
	flavour: string;
	size: Size;
	stock_mode: StockMode;
	purchasable: boolean;
	recipe: Recipe | null;
	version: number;
	archived_at?: string;
}

/** A cake's colour as staff see it (archived colours included). */
export interface StaffCakeColour {
	id: string;
	code: string;
	label: string;
	position: number;
	archived_at?: string;
}

export interface StaffCake {
	id: string;
	name: string;
	description: string;
	colour: StaffCakeColour | null;
	state: CakeState;
	published_at?: string;
	created_at: string;
	updated_at: string;
	version: number;
	variants: StaffVariant[];
}

export interface Colour {
	id: string;
	code: string;
	label: string;
	position: number;
	archived_at: string | null;
	version: number;
}

export interface Flavour {
	id: string;
	name: string;
	created_at?: string;
}

export interface CatalogueReference {
	sizes: Size[];
	allergen_codes: string[];
	units: Unit[];
}

/** Where and as whom the staff API is called (the signed-in staff member's token). */
export interface StaffApi {
	baseUrl: string;
	token: string;
	fetch?: typeof fetch;
}

/** Most cakes a full catalogue read follows (50 per page, 20 pages). */
export const MAX_CAKE_PAGES = 20;
const PAGE = 50;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function id(value: string): string {
	if (!UUID.test(value)) throw new Error(`not an ID: ${value}`);
	return value;
}

function opts(api: StaffApi, query?: ApiOptions['query']): ApiOptions {
	return { baseUrl: api.baseUrl, token: api.token, fetch: api.fetch, query };
}

function cmd(api: StaffApi, key: string) {
	return { ...opts(api), idempotencyKey: key };
}

/** Only the fields given (a PATCH changes what it names; absent keeps). */
function present<T extends object>(o: T): Partial<T> {
	return Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined)) as Partial<T>;
}

// ------------------------------------------------------------------ reads

/** Every cake in every state, newest first (follows next_cursor). */
export async function staffCakes(api: StaffApi): Promise<StaffCake[]> {
	const all: StaffCake[] = [];
	let cursor: string | undefined;
	for (let i = 0; i < MAX_CAKE_PAGES; i++) {
		const page = await apiGet<{ cakes: StaffCake[]; next_cursor?: string }>(
			'/api/v1/admin/cakes',
			opts(api, { limit: PAGE, cursor })
		);
		all.push(...page.cakes);
		if (!page.next_cursor) return all;
		cursor = page.next_cursor;
	}
	return all;
}

export function staffCake(api: StaffApi, cakeId: string): Promise<StaffCake> {
	return apiGet<StaffCake>(`/api/v1/admin/cakes/${id(cakeId)}`, opts(api));
}

export function recipeVersion(api: StaffApi, versionId: string): Promise<RecipeVersion> {
	return apiGet<RecipeVersion>(`/api/v1/admin/recipes/versions/${id(versionId)}`, opts(api));
}

export async function flavours(api: StaffApi): Promise<Flavour[]> {
	return (await apiGet<{ flavours: Flavour[] }>('/api/v1/admin/flavours', opts(api))).flavours;
}

export function currentNotice(api: StaffApi): Promise<Notice> {
	return apiGet<Notice>('/api/v1/admin/allergen-notices/current', opts(api));
}

/** Every colour, archived included, by position then label. */
export async function colours(api: StaffApi): Promise<Colour[]> {
	return (await apiGet<{ colours: Colour[] }>('/api/v1/admin/colours', opts(api))).colours;
}

/** The fixed sizes, allergen codes (UK FIC Annex II) and recipe-line units. */
export function catalogueReference(api: StaffApi): Promise<CatalogueReference> {
	return apiGet<CatalogueReference>('/api/v1/admin/sizes', opts(api));
}

// --------------------------------------------------------------- commands

export function createFlavour(api: StaffApi, key: string, name: string): Promise<Flavour> {
	return apiPost<Flavour>('/api/v1/admin/flavours', { name }, cmd(api, key));
}

/** A new allergen-notice version (the shared-kitchen notice). */
export function createNotice(api: StaffApi, key: string, body: string): Promise<Notice> {
	return apiPost<Notice>('/api/v1/admin/allergen-notices', { body }, cmd(api, key));
}

export function createColour(
	api: StaffApi,
	key: string,
	input: { code: string; label: string; position: number }
): Promise<Colour> {
	return apiPost<Colour>('/api/v1/admin/colours', input, cmd(api, key));
}

/** Changes a colour's label or position, or archives (true) / restores (false) it; the code never changes. */
export function updateColour(
	api: StaffApi,
	key: string,
	colourId: string,
	patch: { expected_version: number; label?: string; position?: number; archived?: boolean }
): Promise<Colour> {
	return apiPatch<Colour>(`/api/v1/admin/colours/${id(colourId)}`, present(patch), cmd(api, key));
}

/** A new draft cake; colour_id null or absent means no colour. */
export function createCake(
	api: StaffApi,
	key: string,
	input: { name: string; description: string; colour_id?: string | null }
): Promise<StaffCake> {
	if (input.colour_id) id(input.colour_id);
	return apiPost<StaffCake>('/api/v1/admin/cakes', present(input), cmd(api, key));
}

/** Changes what it names; colour_id null clears the colour, absent keeps it. */
export function updateCake(
	api: StaffApi,
	key: string,
	cakeId: string,
	patch: {
		expected_version: number;
		name?: string;
		description?: string;
		colour_id?: string | null;
	}
): Promise<StaffCake> {
	if (patch.colour_id) id(patch.colour_id);
	return apiPatch<StaffCake>(`/api/v1/admin/cakes/${id(cakeId)}`, present(patch), cmd(api, key));
}

/** Publishes (active) or archives a cake. */
export function setCakeState(
	api: StaffApi,
	key: string,
	cakeId: string,
	input: { expected_version: number; state: 'active' | 'archived' }
): Promise<StaffCake> {
	return apiPost<StaffCake>(`/api/v1/admin/cakes/${id(cakeId)}/state`, input, cmd(api, key));
}

export function createVariant(
	api: StaffApi,
	key: string,
	cakeId: string,
	input: { flavour_id: string; size_code: string; stock_mode: StockMode }
): Promise<{ variant_id: string; cake: StaffCake }> {
	id(input.flavour_id);
	return apiPost(`/api/v1/admin/cakes/${id(cakeId)}/variants`, input, cmd(api, key));
}

export function archiveVariant(
	api: StaffApi,
	key: string,
	variantId: string,
	input: { expected_version: number }
): Promise<{ variant_id: string; archived: true }> {
	return apiPost(`/api/v1/admin/variants/${id(variantId)}/archive`, input, cmd(api, key));
}

export interface RecipeVersionInput {
	variant_id: string;
	/** The variant's version. */
	expected_version: number;
	/** GBP pence (K00). */
	price_minor: number;
	servings_min: number;
	servings_max: number;
	coating: string;
	composition: string[];
	allergen_notice_id: string;
	/** The full declaration: every allergen the recipe contains, as declared (K01). */
	allergens: Allergen[];
	lines: RecipeLine[];
}

/** A new immutable recipe version for a variant (never an edit of an old one). */
export function createRecipeVersion(
	api: StaffApi,
	key: string,
	input: RecipeVersionInput
): Promise<RecipeVersion> {
	id(input.variant_id);
	id(input.allergen_notice_id);
	if (!Number.isSafeInteger(input.price_minor) || input.price_minor < 0) {
		throw new Error('price_minor must be whole pence');
	}
	return apiPost<RecipeVersion>('/api/v1/admin/recipes/versions', input, cmd(api, key));
}

// ---------------------------------------------------------- view helpers

/** Cakes by state for an overview: drafts, then live, then archived (each newest first). */
export function byState(cakes: StaffCake[]): Record<CakeState, StaffCake[]> {
	const out: Record<CakeState, StaffCake[]> = { draft: [], active: [], archived: [] };
	for (const c of cakes) out[c.state]?.push(c);
	return out;
}

/** The variants still offered (not archived). */
export function liveVariants(cake: StaffCake): StaffVariant[] {
	return cake.variants.filter((v) => !v.archived_at);
}
