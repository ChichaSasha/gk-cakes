<script lang="ts">
	// The customer's recorded agreement (r03b M2): how, when (London time,
	// not in the future) and the evidence, plus the "exactly" tick. Silence
	// never counts; the page lists what is still needed.
	import { CHANNELS, isoToLondonLocal, type AgreementInput } from '../order-decisions';

	let {
		idPrefix,
		value = $bindable(),
		now,
		exactLabel,
		disabled = false,
		describedBy,
		invalidWhen = false,
		invalidEvidence = false
	}: {
		idPrefix: string;
		value: AgreementInput;
		now: Date;
		exactLabel: string;
		disabled?: boolean;
		/** The form's "Still needed" summary, which names what is missing or wrong. */
		describedBy: string;
		invalidWhen?: boolean;
		invalidEvidence?: boolean;
	} = $props();
</script>

<div class="form-grid">
	<label class="field" for="{idPrefix}-channel"
		>How the customer agreed
		<select
			id="{idPrefix}-channel"
			bind:value={value.channel}
			{disabled}
			aria-describedby={describedBy}
		>
			<option value="">Choose…</option>
			{#each CHANNELS as [v, label] (v)}<option value={v}>{label}</option>{/each}
		</select>
	</label>
	<label class="field" for="{idPrefix}-when"
		>When they agreed (London time)
		<input
			id="{idPrefix}-when"
			type="datetime-local"
			max={isoToLondonLocal(now)}
			bind:value={value.when}
			{disabled}
			aria-invalid={invalidWhen ? 'true' : undefined}
			aria-describedby={describedBy}
		/>
	</label>
	<label class="field span2" for="{idPrefix}-evidence"
		>What they said, or where to find it (evidence)
		<input
			id="{idPrefix}-evidence"
			autocomplete="off"
			maxlength="200"
			placeholder="e.g. DM 10:31: “Yes, that works”"
			bind:value={value.evidence}
			{disabled}
			aria-invalid={invalidEvidence ? 'true' : undefined}
			aria-describedby={describedBy}
		/>
	</label>
	<label class="check span2">
		<input type="checkbox" bind:checked={value.exact} {disabled} aria-describedby={describedBy} />
		<span>The customer agreed to exactly {exactLabel}</span>
	</label>
</div>
