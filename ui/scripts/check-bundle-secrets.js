// Builds the UI with a unique canary value in every backend-only variable and
// fails if any canary appears anywhere in the build output (client or server).
// Prints variable names and file paths only.
import { spawnSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const BACKEND_ONLY = [
	'DATABASE_URL',
	'MIGRATION_DATABASE_URL',
	'PAYMENT_SECRET',
	'PAYMENT_WEBHOOK_SECRET',
	'EMAIL_API_KEY',
	'EMAIL_WEBHOOK_SECRET',
	'STORAGE_WRITE_KEY'
];
const OUTPUT_DIR = 'build';

const canaries = Object.fromEntries(
	BACKEND_ONLY.map((name) => [name, `gk-canary-${randomBytes(12).toString('hex')}`])
);

const build = spawnSync('npx', ['vite', 'build'], {
	env: { ...process.env, ...canaries },
	stdio: 'inherit'
});
if (build.status !== 0) {
	console.error('bundle secret check: build failed');
	process.exit(1);
}

function* files(dir) {
	for (const entry of readdirSync(dir, { withFileTypes: true })) {
		const path = join(dir, entry.name);
		if (entry.isDirectory()) yield* files(path);
		else yield path;
	}
}

const leaks = [];
let scanned = 0;
for (const file of files(OUTPUT_DIR)) {
	scanned++;
	const content = readFileSync(file, 'latin1');
	for (const [name, value] of Object.entries(canaries)) {
		if (content.includes(value)) leaks.push(`${name} in ${file}`);
	}
}

if (scanned === 0) {
	console.error(`bundle secret check: no files found in ${OUTPUT_DIR}/`);
	process.exit(1);
}
if (leaks.length > 0) {
	console.error(
		`bundle secret check: backend-only values found in build output:\n  ${leaks.join('\n  ')}`
	);
	process.exit(1);
}
console.log(`bundle secret check: ${scanned} files scanned, no backend-only values found`);
