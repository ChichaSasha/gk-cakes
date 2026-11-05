<script lang="ts">
	// Footer of the discovery pages (approved GK-016 r01c sketch), linking only
	// pages that exist, plus the owner's public contact details (GK-002/GK-005).
	import { resolve } from '$app/paths';
</script>

<footer class="shop-footer">
	<div class="wrap">
		<div class="footer-main">
			<div>
				<h2>Shop</h2>
				<a href={resolve('/(shop)/shop')}>All cakes</a>
				<a href={resolve('/(shop)/shop?selection=orderable')}>Order online</a>
				<a href={resolve('/(shop)/shop?selection=ready')}>Ready-made cakes</a>
				<a href={resolve('/(shop)/sizes')}>Sizes &amp; starting prices</a>
			</div>
			<div>
				<h2>Your order</h2>
				<a href={resolve('/(shop)/bag')}>Your bag</a>
				<a href={resolve('/account')}>Your account</a>
			</div>
			<div>
				<h2>Let’s talk cake</h2>
				<a href="mailto:gk.cake.london@gmail.com">gk.cake.london@gmail.com</a>
				<p>St Katharine Docks, London</p>
				<a href="https://www.instagram.com/gk.cake.london/" rel="noopener noreferrer"
					>Instagram · @gk.cake.london ↗</a
				>
			</div>
		</div>
		<div class="footer-wordmark" aria-hidden="true">GK Cake London</div>
		<div class="footer-bottom">
			<span>© {new Date().getFullYear()} GK Cake London</span>
		</div>
	</div>
</footer>
