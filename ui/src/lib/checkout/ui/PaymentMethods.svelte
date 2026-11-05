<script lang="ts" module>
	// Section 5 (GK-017 r01, Creator decision 2: six methods plus "show all
	// 17"). The secure payment page shows what is actually available.
	export const PAYMENT_METHODS = [
		'Cards',
		'Apple Pay',
		'Onelink',
		'Klarna',
		'Revolut Pay',
		'Amazon Pay',
		'Cartes Bancaires',
		'Samsung Pay',
		'Satispay',
		'MB WAY',
		'Bancontact',
		'BLIK',
		'EPS',
		'Pix',
		'Kakao Pay',
		'Naver Pay',
		'PAYCO'
	] as const;
</script>

<section class="co-card" aria-labelledby="pay-h">
	<h2 class="co-h" id="pay-h"><span class="co-num" aria-hidden="true">5</span>Payment</h2>
	<p>You’ll pay on our secure payment page, which shows the options available to you.</p>
	<p class="co-label">Ways to pay include:</p>
	<p class="pay-list">{PAYMENT_METHODS.slice(0, 6).join(' · ')}</p>
	<details class="pay-all">
		<summary>Show all {PAYMENT_METHODS.length} methods</summary>
		<p class="pay-list">{PAYMENT_METHODS.join(' · ')}</p>
	</details>
	<p class="small">
		The list of methods is still being confirmed with our payment provider. Prices and refunds are
		in pounds (GBP). Some options charge in another currency; the payment page shows the converted
		amount and any fees before you pay.
	</p>
</section>
