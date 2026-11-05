// Checkout renders only a shell on the server; the browser loads the basket,
// times and quotes from the API (contracts K00). Never cached or indexed
// (src/lib/headers.ts isPrivateRoute).
export const ssr = false;
