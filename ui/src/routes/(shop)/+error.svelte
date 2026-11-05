<script lang="ts">
	// Errors on the discovery pages. A missing cake (archived, replaced or
	// never published) gets the approved "no longer in our collection" page
	// (GK-016 r01c sketch); anything else offers a way back.
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
</script>

<svelte:head>
	<title>{page.status === 404 ? 'Not found' : 'Something went wrong'} · GK Cake London</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="wrap">
	{#if page.status === 404 && page.route.id === '/(shop)/cakes/[id]'}
		<div class="page-title">
			<h1>This cake is no longer in our collection</h1>
			<p>It may have been retired or replaced by the chef.</p>
		</div>
		<div class="narrow">
			<div class="notice">
				<div>
					<p>
						This design isn’t in the collection any more, so it can’t be ordered here. Orders you’ve
						already placed are not affected.
					</p>
				</div>
			</div>
			<p>Loved it? Tell the chef what you liked and she will help you create something similar.</p>
			<div class="form-actions">
				<a class="button primary" href={resolve('/(shop)/shop')}
					>Browse current cakes <span class="arrow" aria-hidden="true">→</span></a
				>
				<a class="button" href="mailto:gk.cake.london@gmail.com">Contact the chef</a>
			</div>
		</div>
	{:else}
		<div class="page-title">
			<h1>{page.status === 404 ? 'Page not found' : 'Something went wrong'}</h1>
			<p>Please go back to the shop and try again.</p>
		</div>
		<div class="narrow">
			<a class="button primary" href={resolve('/(shop)/shop')}
				>Shop cakes <span class="arrow" aria-hidden="true">→</span></a
			>
		</div>
	{/if}
</div>
