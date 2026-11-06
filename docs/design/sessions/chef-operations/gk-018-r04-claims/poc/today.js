// GK-018 r04d-claims — Today, fragment (proposal, not approved): only the new "Needs you" line for requests to add
// orders. The rest of Today is the approved r03b2 page (../../gk-018-r03/poc/index.html), not redrawn here.
'use strict';
(function(){
const waiting=CLAIMS.filter(x=>x.state==='pending');
const oldest=waiting[0];
const line=isChef&&waiting.length?`<li class="need need-claims"><span class="need-dot" aria-hidden="true"></span><div><p class="need-title">${waiting.length} ${waiting.length===1?'request':'requests'} to add orders to accounts</p><p class="need-detail">Oldest ${esc(waitText(oldest.created_at).toLowerCase())} (${esc(whoName(oldest))}). Nothing is added until you add it.</p></div><a class="btn btn-small" href="requests.html">Review requests</a></li>`:'';
mount(shell(`<header class="page-head"><div><p class="eyebrow">Today · Fri 6 Nov · London time</p><h1>Today</h1></div></header>
<p class="design-note small">Fragment: only the highlighted “Needs you” line is new (chef only). Everything else on Today is the approved r03b2 page; <a href="../../gk-018-r03/poc/index.html">open it</a>.</p>
<section class="card" aria-labelledby="needs-h"><h2 id="needs-h" class="card-h">Needs you</h2>${line?`<ul class="needs-list">${line}<li class="need need-muted"><span class="need-dot" aria-hidden="true"></span><div><p class="need-title">… the r03b2 items (collection details, paid late, delivery quote, money cases)</p></div></li></ul>`:`<p class="role-note">${isChef?'Nothing waiting.':'Requests to add orders are Elina’s (chef) only, so this line isn’t shown to you.'}</p>`}</section>`,'today'));
})();
