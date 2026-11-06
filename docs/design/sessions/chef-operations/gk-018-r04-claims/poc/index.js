// GK-018 r04-claims — states index (proposal, not approved).
'use strict';
(function(){
const esc=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const GROUPS=[['Requests to add orders (the queue)','S01','S09'],['Decide a request from an order’s email link','S10','S16'],['Decide a request for an older order','S17','S22'],['Refusals and pauses','S23','S30'],['Decided requests (read only)','S31','S33'],['Add an order to a customer’s account (chef-initiated)','S34','S41']];
const n=id=>+id.slice(1);
document.getElementById('app').innerHTML=`<div class="review-bar" role="region" aria-label="Design preview"><span><strong>Proposal GK-018 r04-claims · not approved</strong> · example data, all names and IDs are fake · prototype: nothing is saved</span></div>
<main class="index-main" id="main"><header class="page-head"><div><p class="eyebrow">GK Cake London · chef workspace · design proposal</p><h1>Account requests: states index</h1><p class="muted lead">The chef side of “add an order to an account” (GK-016 r02b paths a and c), in the visual language of the approved GK-018 r03b2 sketch. <strong>Not approved:</strong> drafted while the Creator was away, for her review. See <code>../session.md</code> for the states table, API mapping and open questions.</p></div></header>
${GROUPS.map(([g,a,b])=>`<section class="card" aria-labelledby="g-${a}"><h2 class="card-h" id="g-${a}">${esc(g)}</h2><div class="table-wrap"><table class="table states-table"><thead><tr><th scope="col">State</th><th scope="col">What it shows</th><th scope="col">Open</th></tr></thead><tbody>${STATES.filter(s=>n(s[0])>=n(a)&&n(s[0])<=n(b)).map(([id,url,title])=>`<tr><td>${id}</td><td>${esc(title)}</td><td><a class="btn btn-small" href="${esc(url)}">Open <span class="sr-only">${id}</span></a></td></tr>`).join('')}</tbody></table></div></section>`).join('')}
<p class="small muted">Sidebar sections other than Account requests open the approved r03b2 prototype (serve the <code>chef-operations</code> folder so both are reachable).</p></main>`;
})();
