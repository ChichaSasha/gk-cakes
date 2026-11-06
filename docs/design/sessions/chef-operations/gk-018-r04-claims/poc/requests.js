// GK-018 r04-claims — "Requests to add orders": the chef's queue (proposal, not approved).
// Live call it stands for: GET /api/v1/admin/order-claims?state=pending|approved|declined|withdrawn[&cursor][&limit]
// (pending by default, oldest first, at most 50 a page, next_cursor when there are more).
'use strict';
(function(){
const TABS=[['pending','Waiting'],['approved','Added'],['declined','Couldn’t add'],['withdrawn','Withdrawn']];
const STATES=[['normal','Loaded'],['loading','Loading'],['error','Couldn’t load'],['empty','Nothing waiting'],['more','More than a page']];
const tab=TABS.some(t=>t[0]===q.get('tab'))?q.get('tab'):'pending';
const st=STATES.some(s=>s[0]===q.get('state'))?q.get('state'):'normal';
const groups=[['state','List',STATES,'normal']];
if(!isChef){mount(shell(`<header class="page-head"><div><p class="eyebrow">Account requests</p><h1>Requests to add orders</h1></div></header>${roleNote}`,'claims',groups));return}

const tabLink=([k,l])=>{const u=new URL(location.href);u.searchParams.set('tab',k);if(k!=='pending'&&st==='more')u.searchParams.delete('state');const n=k==='pending'&&st!=='empty'&&st!=='loading'&&st!=='error'?` <span class="tab-count">${st==='more'?'5+':pendingCount()}</span>`:'';
 return `<a class="filter-chip tab" href="${esc(u.pathname.split('/').pop()+u.search)}" ${k===tab?'aria-current="page"':''}>${l}${n}</a>`};

function howCell(cl){const od=cl.order_id&&orderById(cl.order_id);
 if(cl.path==='email_link')return `<span class="how-chip">${icon('link','how-icon')}From the order’s email link</span><span class="block">Website order <a class="mono" href="order.html?id=${shortId(cl.order_id)}">${shortId(cl.order_id)}</a></span><span class="block muted small">${esc(orderLine(od))}</span>`;
 const ref=cl.erased?'<span class="muted">erased</span>':`<span class="mono">${esc(cl.reference)}</span>`;
 return `<span class="how-chip">${esc(CHANNEL_CHEF[cl.channel])}</span><span class="block">Your reference: ${ref}</span><span class="block muted small">${cl.approximate_date?`Roughly ${esc(monthLabel(cl.approximate_date))}`:'No month given'}</span>${cl.state==='approved'&&od?`<span class="block small">Added as order <a class="mono" href="order.html?id=${shortId(od.id)}">${shortId(od.id)}</a></span>`:''}`}
const noteCell=cl=>cl.erased?'<span class="muted">Erased</span>':cl.note?`<p class="clamp">“${esc(cl.note)}”</p>`:'<span class="muted">No note</span>';
function outcomeCell(cl){if(cl.state==='pending'){const same=[...CLAIMS,...MORE].filter(x=>x.state==='pending'&&x.order_id&&x.order_id===cl.order_id&&x.id!==cl.id);
  return `${chip(waitText(cl.created_at),daysWaiting(cl.created_at)>=3?'warn':'info')}${same.length?`<span class="block small warn-text">Another request asks for this order</span>`:''}`}
 const [t,tone]=STATE_CHEF[cl.state];
 return `${chip(t,tone)}<span class="block small muted">${when(cl.decided_at)}${cl.state==='withdrawn'?'':` · by ${decidedBy(cl)}`}</span>${cl.decision_note?`<span class="block small internal-note"><span class="sr-only">Your internal note: </span>${icon('info','note-icon')}${esc(cl.decision_note)}</span>`:''}`}
const cell=(label,html,tag='td')=>`<${tag} ${tag==='th'?'scope="row" ':''}data-label="${label}"><div>${html}</div></${tag}>`;
const rows=list=>list.map(cl=>`<tr>${cell('Received',`${when(cl.created_at)}<span class="block mono small muted">Request ${shortId(cl.id)}</span>`)}
 ${cell('Who',`${cl.erased?'<span class="muted">Details erased</span>':esc(whoName(cl))}<span class="block small muted">${cl.erased?'The customer deleted their account':[cl.account.instagram?`Instagram ${esc(cl.account.instagram)}`:'',cl.account.phone?esc(cl.account.phone):''].filter(Boolean).join(' · ')||'No Instagram or phone'}</span>`,'th')}
 ${cell('How',howCell(cl))}${cell('Their note',noteCell(cl))}${cell(tab==='pending'?'Waiting':'Outcome',outcomeCell(cl))}
 <td class="td-action"><a class="btn ${tab==='pending'?'btn-primary':''} btn-small" href="request.html?id=${shortId(cl.id)}" aria-label="${tab==='pending'?'Review':'View'} request ${shortId(cl.id)} from ${esc(whoName(cl))}">${tab==='pending'?'Review':'View'}</a></td></tr>`).join('');

let list=CLAIMS.filter(x=>x.state===tab);
let body;
const head=`<thead><tr><th scope="col">Received</th><th scope="col">Who <span class="th-note">(their profile)</span></th><th scope="col">How</th><th scope="col">Their note</th><th scope="col">${tab==='pending'?'Waiting':'Outcome'}</th><th scope="col"><span class="sr-only">Action</span></th></tr></thead>`;
if(st==='loading')body=`<div class="loading" role="status" aria-live="polite"><span class="spinner" aria-hidden="true"></span>Loading requests…</div><div class="skeleton" aria-hidden="true"><span></span><span></span><span></span></div>`;
else if(st==='error')body=`<div class="banner banner-bad" role="alert">${icon('alert','banner-icon')}<div><p><strong>The requests couldn’t be loaded right now.</strong> Nothing was changed. Check your connection and try again.</p><a class="btn btn-small" href="requests.html?tab=${tab}">Try again</a></div></div>`;
else if(st==='empty'&&tab==='pending')list=[];
if(!body){
 const empty={pending:'Nothing waiting. When a customer asks to add an order to their account, it shows here, oldest first.',approved:'No orders added to accounts yet.',declined:'No declined requests.',withdrawn:'No withdrawn requests.'}[tab];
 const showMore=st==='more'&&tab==='pending';
 body=list.length?`<p class="small strong" id="result-count" aria-live="polite">${showMore?`Showing the oldest ${list.length} waiting · more below`:`${list.length} ${list.length===1?'request':'requests'}`} · ${tab==='pending'?'oldest first':'oldest received first'}</p>
 <div class="table-wrap"><table class="table orders-table claims-table"><caption class="sr-only">${TABS.find(t=>t[0]===tab)[1]} requests to add orders</caption>${head}<tbody id="rows">${rows(list)}</tbody></table></div>
 ${showMore?`<div class="more-row"><button class="btn" type="button" data-more>Show the next requests</button><p class="small muted">Live: 50 at a time, then “Show the next 50” (the list’s next page). This preview pages at 5.</p></div>`:''}`
 :`<div class="empty"><p class="strong">${empty}</p>${tab==='pending'?'<p class="small muted">Customers ask from their account: from the private link in a website order’s email, or with your reference for an older order.</p>':''}</div>`}

mount(shell(`<header class="page-head"><div><p class="eyebrow">Account requests · chef only · London time</p><h1>Requests to add orders</h1><p class="muted lead">Customers ask to add an order to their account. Nothing is added until you add it, and nothing is ever matched automatically by email, payer, name or Instagram.</p></div></header>
<div class="banner banner-info">${icon('info','banner-icon')}<div><p><strong>Instagram, email, phone and in-person orders aren’t in the system yet.</strong> Only website orders can be added for now. Recording older orders by hand comes later (GK-027/GK-038, not built), so a request for an older order usually ends with “Decline” or waits.</p></div></div>
<section class="card" aria-labelledby="list-h"><h2 id="list-h" class="sr-only">Requests</h2><nav class="chips tabs" aria-label="Request status">${TABS.map(tabLink).join('')}</nav>${body}
<p class="muted small foot">Name, Instagram and phone come from the customer’s profile: they typed them, nobody checked them, and they aren’t proof. The workspace doesn’t store the customer’s email address.</p></section>`,'claims',groups));

const more=document.querySelector('[data-more]');
if(more)more.addEventListener('click',()=>{document.getElementById('rows').insertAdjacentHTML('beforeend',rows(MORE));const first=document.querySelectorAll('#rows tr')[CLAIMS.filter(x=>x.state==='pending').length];more.closest('.more-row').innerHTML='<p class="small muted">That’s everything waiting (8).</p>';document.getElementById('result-count').textContent='8 requests · oldest first · all shown';const a=first&&first.querySelector('a.btn');if(a)a.focus();toast('Prototype: loaded the next page (example data).')});
})();
