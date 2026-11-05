// The checkout basket from the URL (GK-025). The real basket arrives with
// the discovery pages (GK-016); until then /checkout is unlinked and reads
// ?v=<variantId> (a made-to-order cake) and ?lot=<lotId> (a ready cake),
// each repeated once per cake, plus an optional ?date=YYYY-MM-DD. Only IDs
// are read: prices, names and allergens always come from the API.

export interface BasketLine {
	variantId?: string;
	lotId?: string;
	quantity: number;
}

export interface Basket {
	lines: BasketLine[];
	/** Preferred handover date, if the link carried a valid one. */
	date?: string;
	problem?: 'empty' | 'invalid' | 'too_many';
}

/** The API's limits (checkout.MaxLines, checkout.MaxCakes). */
export const MAX_LINES = 20;
export const MAX_CAKES = 20;

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const DATE = /^\d{4}-\d{2}-\d{2}$/;

export function parseBasket(params: URLSearchParams): Basket {
	const lines: BasketLine[] = [];
	const add = (key: 'variantId' | 'lotId', id: string) => {
		const line = lines.find((l) => l[key] === id);
		if (line) line.quantity++;
		else lines.push({ [key]: id, quantity: 1 });
	};
	const variants = params.getAll('v');
	const lots = params.getAll('lot');
	for (const id of [...variants, ...lots]) {
		if (!UUID.test(id)) return { lines: [], problem: 'invalid' };
	}
	for (const id of variants) add('variantId', id);
	for (const id of lots) add('lotId', id);
	const dateParam = params.get('date') ?? '';
	const date = DATE.test(dateParam) && !Number.isNaN(Date.parse(dateParam)) ? dateParam : undefined;
	if (lines.length === 0) return { lines, date, problem: 'empty' };
	const cakes = lines.reduce((n, l) => n + l.quantity, 0);
	if (lines.length > MAX_LINES || cakes > MAX_CAKES) return { lines: [], problem: 'too_many' };
	return { lines, date };
}

/** Made-to-order cakes need day places; ready cakes do not (K04). */
export function madeToOrderQuantity(lines: BasketLine[]): number {
	return lines.filter((l) => l.variantId).reduce((n, l) => n + l.quantity, 0);
}

/** The quote request's lines (exact API field names, nothing else). */
export function quoteLines(
	lines: BasketLine[]
): ({ variant_id: string; quantity: number } | { lot_id: string; quantity: number })[] {
	return lines.map((l) =>
		l.variantId
			? { variant_id: l.variantId, quantity: l.quantity }
			: { lot_id: l.lotId as string, quantity: l.quantity }
	);
}
