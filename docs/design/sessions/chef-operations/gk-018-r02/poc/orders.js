// GK-018 r02 — Orders: list and search by name or order number, by handover or production date.
'use strict';
(function(){
const NEED_IDS=new Set(needs().map(n=>n.order).filter(Boolean));
const weekStart=d=>addDays(d,-((weekdayNum(d)+6)%7));
const thisWeek=weekStart(TODAY);
const PERIODS={upcoming:'From today',this:'This week (Mon 19 – Sun 25 Oct)',next:'Next week (Mon 26 Oct – Sun 1 Nov)',past:'Before today',all:'All dates'};
const FILTERS=[['all','All'],['needs','Needs action'],['confirmed','Paid · not started'],['production','In production'],['ready','Ready'],['cancelled','Cancelled'],['payment','Payment unknown or late']];
const showMoney=REVIEW.role!=='fulfillment';
const prodDate=o=>o.startedAt?o.startedAt:o.autoAt;
const prodText=o=>o.stage==='awaiting'?'After payment':o.stage==='agreement'?'Held · your decision':o.startedAt?`Started ${when(o.startedAt,{weekday:'short',day:'numeric',month:'short'})}`:`${before(o.autoAt,NOW)?'Automatic since':'Automatic from'} ${when(o.autoAt,{weekday:'short',day:'numeric',month:'short'})}`;
function moneyText(o){if(o.stage==='awaiting')return 'Result unknown';if(o.stage==='cancelled'){const open=o.collections.some(c=>c.refunds.some(r=>['pending','unknown','reserved','submitting','requires_action'].includes(r.status)));const T=Math.min(o.collections.reduce((n,c)=>n+c.paid,0),halfUp(o.collections.reduce((n,c)=>n+c.paid,0),o.cancel.rule)+(o.cancel.goodwill||0));const S=o.collections.reduce((n,c)=>n+c.refunds.filter(r=>r.status==='succeeded').reduce((a,r)=>a+r.amount,0),0);return open?`Refund ${money(T)} · not confirmed`:T>S?`${money(T-S)} still owed`:`Refunded ${money(S)}`}
 const d=deliveryPaid(o);return `${money(paidFor(o))}${d?` + ${money(d)} delivery`:o.mode==='delivery'?' · delivery not paid':''}`}
const init={q:q.get('q')||'',by:q.get('by')==='production'?'production':'handover',period:PERIODS[q.get('period')]?q.get('period'):'upcoming',filter:FILTERS.some(f=>f[0]===q.get('filter'))?q.get('filter'):'all',mode:MODES[q.get('mode')]?q.get('mode'):''};

mount(shell(`<header class="page-head"><div><p class="eyebrow">Orders · London time</p><h1>Orders</h1></div>${can('neworder')?`<a class="btn btn-primary" href="new-order.html">${icon('plus')}New order</a>`:''}</header>
<section class="card" aria-label="Find orders"><form class="filters" data-filters role="search"><label class="field search-field"><span>Search by name or order number</span><span class="search-box">${icon('search')}<input type="search" name="q" value="${esc(init.q)}" placeholder="e.g. Mia or GK-1044" autocomplete="off"></span></label>
<fieldset class="seg"><legend>Dates by</legend><label><input type="radio" name="by" value="handover" ${init.by==='handover'?'checked':''}> Handover</label><label><input type="radio" name="by" value="production" ${init.by==='production'?'checked':''}> Production</label></fieldset>
<label class="field">When<select name="period">${Object.entries(PERIODS).map(([k,l])=>`<option value="${k}" ${k===init.period?'selected':''}>${l}</option>`).join('')}</select></label>
<label class="field">How<select name="mode"><option value="">Any</option>${Object.entries(MODES).map(([k,l])=>`<option value="${k}" ${k===init.mode?'selected':''}>${l}</option>`).join('')}</select></label>
<div class="chips" role="group" aria-label="Status">${FILTERS.map(([k,l])=>`<button type="button" class="filter-chip" data-filter="${k}" aria-pressed="${k===init.filter}">${l}</button>`).join('')}</div></form>
<p class="small strong" id="result-count" aria-live="polite"></p>
<div class="table-wrap"><table class="table orders-table"><caption class="sr-only">Orders matching the search</caption><thead><tr><th scope="col">Order</th><th scope="col">Customer</th><th scope="col">Cake</th><th scope="col" data-col="date">Handover</th><th scope="col">Production</th><th scope="col">Status</th>${showMoney?'<th scope="col">Money</th>':''}</tr></thead><tbody id="rows"></tbody></table></div>
<p class="muted small foot">Production dates show when you started, or the automatic start 48 hours after payment (which uses no ingredients). Technical support sees this list read only.</p></section>`,'orders'));

const form=document.querySelector('[data-filters]');
function render(){const f=new FormData(form);const term=String(f.get('q')||'').trim().toLowerCase();const by=f.get('by'),period=f.get('period'),mode=f.get('mode');const filter=form.querySelector('[aria-pressed="true"]').dataset.filter;
 const keyDate=o=>by==='production'?when(prodDate(o),{year:'numeric',month:'2-digit',day:'2-digit'}).split('/').reverse().join('-'):o.date;
 let list=ORDERS.filter(o=>{const d=keyDate(o);
  if(term&&!(o.id.toLowerCase().includes(term)||o.customer.toLowerCase().includes(term)||o.email.toLowerCase().includes(term)))return false;
  if(mode&&o.mode!==mode)return false;
  if(period==='upcoming'&&d<TODAY)return false;if(period==='past'&&d>=TODAY)return false;if(period==='this'&&(d<thisWeek||d>addDays(thisWeek,6)))return false;if(period==='next'&&(d<addDays(thisWeek,7)||d>addDays(thisWeek,13)))return false;
  if(filter==='needs'&&!(NEED_IDS.has(o.id)||detailsOverdue(o)))return false;if(filter==='confirmed'&&o.stage!=='confirmed')return false;if(filter==='production'&&!['preparing','making'].includes(o.stage))return false;if(filter==='ready'&&!['ready','out'].includes(o.stage))return false;if(filter==='cancelled'&&o.stage!=='cancelled')return false;if(filter==='payment'&&!['awaiting','agreement'].includes(o.stage))return false;
  return true});
 list.sort((a,b)=>by==='production'?new Date(prodDate(a))-new Date(prodDate(b)):(a.date+a.slot).localeCompare(b.date+b.slot));
 document.getElementById('rows').innerHTML=list.length?list.map(o=>`<tr><td data-label="Order"><a href="order.html?id=${o.id}" class="strong">${o.id}</a>${o.extra?' <span class="tag-extra">Extra</span>':''}${sourceTag(o)}${o.shortNotice?' <span class="tag-short">Short notice</span>':''}</td><td data-label="Customer">${esc(o.customer)}</td><td data-label="Cake">${esc(o.cake)}${o.peanuts?' · <strong class="peanut">PEANUTS</strong>':''}<span class="block muted small">${MODES[o.mode]}</span></td><td data-label="Handover">${dayShort(o.date)}<span class="block muted small">${o.slot}</span></td><td data-label="Production">${prodText(o)}</td><td data-label="Status">${stagePill(o)}${detailsOverdue(o)?'<span class="block small bad-text">Details not sent</span>':''}</td>${showMoney?`<td data-label="Money">${moneyText(o)}</td>`:''}</tr>`).join(''):`<tr><td colspan="${showMoney?7:6}" class="empty-row">No orders match${term?` “${esc(term)}”`:''}. Try another name, “GK-” number or date range.</td></tr>`;
 say('result-count',`${list.length} ${list.length===1?'order':'orders'} · sorted by ${by==='production'?'production date':'handover date and time'}`);
 const u=new URL(location.href);['q','by','period','mode','filter'].forEach(k=>u.searchParams.delete(k));if(term)u.searchParams.set('q',term);if(by!=='handover')u.searchParams.set('by',by);if(period!=='upcoming')u.searchParams.set('period',period);if(mode)u.searchParams.set('mode',mode);if(filter!=='all')u.searchParams.set('filter',filter);history.replaceState(null,'',u.pathname.split('/').pop()+u.search)}
form.addEventListener('submit',e=>e.preventDefault());form.addEventListener('input',render);form.addEventListener('change',render);
form.addEventListener('click',e=>{const b=e.target.closest('[data-filter]');if(!b)return;form.querySelectorAll('[data-filter]').forEach(x=>x.setAttribute('aria-pressed',String(x===b)));render()});
render();
})();
