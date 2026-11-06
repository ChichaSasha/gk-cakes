// GK-018 r04-claims — decide one request (proposal, not approved).
// Live calls it stands for:
//   approve: POST /api/v1/admin/order-claims/{id}/decision {expected_version, decision: "approve", order_id}
//   decline: POST /api/v1/admin/order-claims/{id}/decision {expected_version, decision: "decline", note?}
//   with an Idempotency-Key kept for retries of the same decision.
// Finding an order for an older-order request reads GET /api/v1/admin/orders?from&to (at most 93 days) and filters
// in the browser by the start of the order ID or the name on the order (the API has no text search).
'use strict';
(function(){
const STEPS=['review','approve','decline','busy','done'];
const ANSWERS=[['ok','Saved'],['order_owned','In another account'],['stale','Changed meanwhile'],['not_link_order','Not that order (422)'],['account_unavailable','Account disabled'],['paused','Paused (503)'],['lost','Answer lost']];
const APPROVE_ONLY=['order_owned','not_link_order','account_unavailable'];
const cl0=claimById(q.get('id')||'c7e1a904');
const groups=[['answer','Simulated answer',ANSWERS,'ok']];
if(!isChef){mount(shell(`<a class="back" href="requests.html">${icon('back')} Requests to add orders</a><header class="page-head"><div><p class="eyebrow">Account requests</p><h1>Request to add an order</h1></div></header>${roleNote}`,'claims',groups));return}
if(!cl0){mount(shell(`<a class="back" href="requests.html">${icon('back')} Requests to add orders</a><div class="card empty"><h1>Request not found</h1><p>This request isn’t in the design preview. The live workspace answers “not found” for an unknown request.</p><a class="btn" href="requests.html">Back to requests</a></div>`,'claims',groups));return}
const cl=JSON.parse(JSON.stringify(cl0));
// "Show the latest" after a stale answer: the customer withdrew it meanwhile (example).
if(q.get('latest')==='1'&&cl.state==='pending'){cl.state='withdrawn';cl.version+=1;cl.decided_at='2026-11-06T10:29:00Z'}
const isLink=cl.path==='email_link';
const pending=cl.state==='pending';
const linkOrder=isLink?orderById(cl.order_id):null;
const name=cl.account.full_name;
const theirAccount=name?`${esc(name)}’s account`:'their account';
const same=pending&&cl.order_id?[...CLAIMS,...MORE].filter(x=>x.state==='pending'&&x.order_id===cl.order_id&&x.id!==cl.id):[];
const nextPending=[...CLAIMS,...MORE].filter(x=>x.state==='pending'&&x.id!==cl.id&&new Date(x.created_at)>=new Date(cl.created_at))[0];

const S={step:STEPS.includes(q.get('step'))?q.get('step'):'review',decision:q.get('decision')==='decline'?'decline':'approve',pick:q.get('pick')||'',answer:ANSWERS.some(a=>a[0]===q.get('answer'))?q.get('answer'):'ok',note:q.get('note')||'',term:q.get('q')||''};
if(S.step==='decline')S.decision='decline';if(S.step==='approve')S.decision='approve';
if(isLink)S.pick=shortId(cl.order_id);
const answerFor=()=>S.decision==='decline'&&APPROVE_ONLY.includes(S.answer)?'ok':S.answer;
const picked=()=>S.pick?orderById(S.pick):null;

// Months for the search range (London handover months). Default: the customer's month ±1, else the last 3 months.
const MONTH_OPTS=[];for(let y=2026;y<=2026;y++)for(let m=1;m<=12;m++)MONTH_OPTS.push(`${y}-${String(m).padStart(2,'0')}`);
const shift=(ym,n)=>{let [y,m]=ym.split('-').map(Number);m+=n;while(m<1){m+=12;y--}while(m>12){m-=12;y++}return `${y}-${String(m).padStart(2,'0')}`};
const clampM=ym=>ym<MONTH_OPTS[0]?MONTH_OPTS[0]:ym>MONTH_OPTS[MONTH_OPTS.length-1]?MONTH_OPTS[MONTH_OPTS.length-1]:ym;
const base=cl.approximate_date?cl.approximate_date.slice(0,7):'2026-10';
S.from=MONTH_OPTS.includes(q.get('from'))?q.get('from'):clampM(cl.approximate_date?shift(base,-1):'2026-09');
S.to=MONTH_OPTS.includes(q.get('to'))?q.get('to'):clampM(cl.approximate_date?shift(base,1):'2026-11');
const monthsSpan=(a,b)=>{const [y1,m1]=a.split('-').map(Number),[y2,m2]=b.split('-').map(Number);return (y2-y1)*12+(m2-m1)+1};

function keepUrl(){const u=new URL(location.href);const set=(k,v,def)=>{if(v&&v!==def)u.searchParams.set(k,v);else u.searchParams.delete(k)};
 set('step',S.step,'review');set('decision',S.step==='busy'||S.step==='done'?S.decision:'','');set('pick',isLink?'':S.pick,'');set('q',S.term,'');history.replaceState(null,'',u.pathname.split('/').pop()+u.search)}

const orderCard=(od,extra='')=>`<div class="order-sum"><img src="assets/${IMG[od.cake]}.png" alt="" width="72" height="72"><div><p class="strong">Order <span class="mono">${shortId(od.id)}</span> · ${esc(CAKES[od.cake][0])} ${lifeChip(od)}</p><p class="small muted">${esc(CAKES[od.cake][1])} · ${esc(CAKES[od.cake][2])}</p><p class="small">${dayShort(od.date)}, ${od.slot} · ${MODES[od.mode]}</p><p class="small">Name on the order: <strong>${esc(od.customer_full_name)}</strong> · ${money(od.total)}</p>${extra}</div></div>`;

// ---------- left column
function sentCard(){
 const rows=isLink?[['How they asked',`<span class="how-chip">${icon('link','how-icon')}From the order’s email link</span>`],['Their note',cl.note?`<q>${esc(cl.note)}</q>`:'<span class="muted">No note (optional on this path)</span>']]
  :[['How they ordered',`<span class="how-chip">${esc(CHANNEL_CHEF[cl.channel])}</span>`],['Your reference',cl.erased?'<span class="muted">Erased</span>':`<span class="mono strong">${esc(cl.reference)}</span>`],['Roughly when',cl.erased?'<span class="muted">Erased</span>':cl.approximate_date?esc(monthLabel(cl.approximate_date)):'<span class="muted">Not given</span>'],['Their note',cl.erased?'<span class="muted">Erased</span>':`<q>${esc(cl.note)}</q>`]];
 return `<section class="card" aria-labelledby="sent-h"><h2 id="sent-h" class="card-h">What they sent</h2>${facts([['Received',`${when(cl.created_at)}`],...rows])}
 ${isLink?`<p class="small muted">They opened the private link to this order and asked from their signed-in account. Having the link shows they can see the order; it doesn’t prove the order is theirs (someone they shared the email with could ask too).</p>`:cl.erased?'':`<p class="small muted">The reference is whatever they copied from your message or receipt. It isn’t checked against anything.</p>`}</section>`}
const whoCard=()=>`<section class="card" aria-labelledby="who-h"><h2 id="who-h" class="card-h">Who’s asking · from their profile</h2>${cl.erased?'<p class="muted">The customer deleted their account, so their profile and what they wrote were erased. The link between the account and the order stays as a record without personal details.</p>':facts([['Name',name?esc(name):'<span class="muted">Not given</span>'],['Instagram',cl.account.instagram?`<span class="mono">${esc(cl.account.instagram)}</span>`:'<span class="muted">Not given</span>'],['Phone',cl.account.phone?`<span class="mono">${esc(cl.account.phone)}</span>`:'<span class="muted">Not given</span>']])+`<p class="small muted">Typed by the customer in their profile. Not checked and not proof of who they are. Their email address isn’t shown: the workspace doesn’t store it.</p>`}</section>`;
function linkOrderCard(){return `<section class="card" aria-labelledby="lo-h"><h2 id="lo-h" class="card-h">The order from their link</h2>${linkOrder?orderCard(linkOrder,`<p class="small"><a href="order.html?id=${shortId(linkOrder.id)}">Open order ${shortId(linkOrder.id)}</a></p>`):'<p class="muted">This order isn’t in the preview.</p>'}
 ${same.length?`<div class="banner banner-warn small">${icon('alert','banner-icon')}<div><p><strong>Another request asks for this same order.</strong> ${same.map(x=>`Request <a class="mono" href="request.html?id=${shortId(x.id)}">${shortId(x.id)}</a> from ${esc(whoName(x))} (${when(x.created_at)})`).join('; ')}. Only one account can have the order: if you add it here, adding it for the other request is refused.</p></div></div>`:''}
 <p class="small muted">For a request from an email link, only this order can be added.</p></section>`}
function findCard(){const sp=monthsSpan(S.from,S.to);
 return `<section class="card" aria-labelledby="find-h"><h2 id="find-h" class="card-h">${icon('search')}Find the order</h2><p class="small muted">Looks only at orders already in the system (website orders). Choosing one is your judgement: a matching name or reference isn’t proof.</p>
 <form class="find-form" data-find role="search" novalidate><label class="field find-term"><span>Start of the order ID, or the name on the order</span><span class="search-box">${icon('search')}<input type="search" name="q" value="${esc(S.term)}" placeholder="e.g. 8b20 or Becker" autocomplete="off" aria-describedby="find-hint"></span></label>
 <label class="field">Handover from<select name="from">${MONTH_OPTS.map(m=>`<option value="${m}" ${m===S.from?'selected':''}>${monthLabel(m)}</option>`).join('')}</select></label>
 <label class="field">to<select name="to">${MONTH_OPTS.map(m=>`<option value="${m}" ${m===S.to?'selected':''}>${monthLabel(m)}</option>`).join('')}</select></label></form>
 <p id="find-hint" class="small muted">Up to 3 months at a time (the order list reads at most 93 days). ${cl.approximate_date?`Starts around their month, ${esc(monthLabel(cl.approximate_date))}.`:'They gave no month: starts with the last 3 months.'}</p>
 ${cl.erased?'':`<div class="quick"><span class="small">Search for:</span> <button class="btn btn-small" type="button" data-fill="${esc(cl.reference)}">their reference “${esc(cl.reference)}”</button>${name?` <button class="btn btn-small" type="button" data-fill="${esc(name.split(' ').slice(-1)[0])}">their surname “${esc(name.split(' ').slice(-1)[0])}”</button>`:''}</div>`}
 <div id="find-out">${sp<1||sp>3?`<p class="bad-text small" role="alert">Choose up to 3 months, with “to” on or after “from”.</p>`:results()}</div></section>`}
function results(){const t=S.term.trim().toLowerCase();const inRange=ORDERS.filter(od=>od.date.slice(0,7)>=S.from&&od.date.slice(0,7)<=S.to);
 const list=inRange.filter(od=>!t||od.customer_full_name.toLowerCase().includes(t)||od.id.toLowerCase().startsWith(t)).sort((a,b)=>a.date.localeCompare(b.date));
 const range=`${monthLabel(S.from)}${S.from===S.to?'':' – '+monthLabel(S.to)}`;
 if(!inRange.length||!list.length)return `<p class="small strong" aria-live="polite" id="find-count">No orders found${t?` for “${esc(S.term)}”`:''} · handover ${range}</p>
  <div class="banner banner-info no-match">${icon('info','banner-icon')}<div><p><strong>No matching order in the system${inRange.length?'':' yet'}.</strong> ${cl.channel==='website_email_link'?'':`${CHANNEL_CHEF[cl.channel]==='Instagram'?'Instagram orders':'Orders made by email, phone or in person'} from before the website aren’t in the system, so they can’t be added to an account yet. Recording older orders by hand comes later (GK-027/GK-038, not built).`}</p>
  <p class="small">What you can do now:</p><ul class="small list-tight"><li><strong>Decline.</strong> The customer sees only “Couldn’t add” and is asked to contact us. You can add an internal note for yourself.</li><li><strong>Leave it waiting.</strong> They keep seeing “Waiting for the chef”, and it counts towards their limit of 5 waiting requests.</li>${inRange.length?'<li>Try another name, ID or months.</li>':''}</ul></div></div>`;
 return `<p class="small strong" aria-live="polite" id="find-count">${list.length} ${list.length===1?'order':'orders'} · handover ${range}${t?` · matching “${esc(S.term)}”`:''}</p>
 <fieldset class="pick-list"><legend class="sr-only">Choose the order to add</legend>${list.map(od=>`<label class="pick ${S.pick===shortId(od.id)?'is-picked':''}"><input type="radio" name="pick" value="${shortId(od.id)}" ${S.pick===shortId(od.id)?'checked':''}><span><span class="strong"><span class="mono">${shortId(od.id)}</span> · ${esc(od.customer_full_name)}</span> ${lifeChip(od)}<span class="block small muted">${esc(CAKES[od.cake][0])} · ${dayShort(od.date)}, ${od.slot} · ${money(od.total)}</span></span></label>`).join('')}</fieldset>
 <p class="small muted">This list can’t show whether an order is already in someone’s account. If it is, adding it is refused and nothing changes.</p>`}
function decidedCard(){const od=cl.order_id&&orderById(cl.order_id);const [t,tone]=STATE_CHEF[cl.state];
 return `<section class="card" aria-labelledby="out-h"><h2 id="out-h" class="card-h">Outcome</h2><p>${chip(t,tone)}</p>${facts([[cl.state==='withdrawn'?'Withdrawn':'Decided',`${when(cl.decided_at)}${cl.state==='withdrawn'?' by the customer':` by ${decidedBy(cl)}`}`],cl.state==='approved'&&od?['Order added',`<a class="mono" href="order.html?id=${shortId(od.id)}">${shortId(od.id)}</a> · ${esc(orderLine(od))}`]:null,cl.state==='approved'||cl.state==='declined'?['Your internal note',cl.erased?'<span class="muted">Erased with the account</span>':cl.decision_note?esc(cl.decision_note):'<span class="muted">None</span>']:null,['The customer sees',cl.erased?'<span class="muted">Nothing: the account was deleted</span>':CUSTOMER_SEES[cl.state]]])}
 ${cl.state==='approved'?'<p class="small muted">When it was added, the private links in that order’s emails stopped working; they see the order by signing in. Removing an order from an account isn’t built.</p>':''}${cl.state==='withdrawn'?'<p class="small muted">Nothing was added. A withdrawn request can’t be decided.</p>':''}</section>`}

// ---------- right column: the decision
function confirmApprove(od){return `<div class="confirm-box" role="group" aria-labelledby="ca-h"><p id="ca-h" class="strong" tabindex="-1" data-focus>Add order <span class="mono">${shortId(od.id)}</span> to ${theirAccount}?</p>
 <ul class="small list-tight"><li>The order appears in their account, in their order list.</li><li>The private links in this order’s emails stop working, for everyone who has them. They see the order by signing in.</li><li>Their request shows “Added to your account”.</li><li>There’s no undo here: removing an order from an account isn’t built.</li></ul>
 <div class="actions"><button class="btn btn-primary" type="button" data-go="approve-yes">Yes, add to their account</button><button class="btn btn-quiet" type="button" data-go="review">Not yet</button></div></div>`}
function declineForm(){return `<form class="confirm-box" data-decline novalidate aria-labelledby="cd-h"><p id="cd-h" class="strong" tabindex="-1" data-focus>Decline request <span class="mono">${shortId(cl.id)}</span>?</p>
 <ul class="small list-tight"><li>Nothing is added to their account.</li><li>They see only “Couldn’t add” and are asked to contact us: no reason, and nothing about any order.</li>${isLink?'<li>The private link in their order email keeps working.</li>':''}</ul>
 <label class="field">Internal note (optional) · only you see it, never the customer<input name="note" maxlength="500" value="${esc(S.note)}" autocomplete="off" aria-describedby="note-count" placeholder="e.g. No historical Instagram orders in the system yet"></label><p class="small muted counter" id="note-count" aria-live="polite">${500-[...S.note].length} characters left · one line</p>
 <div class="actions"><button class="btn btn-danger" type="submit">Decline request</button><button class="btn btn-quiet" type="button" data-go="review">Not yet</button></div></form>`}
function resultBlock(){const a=answerFor(),od=picked(),dec=S.decision;
 const retry=`<button class="btn btn-small" type="button" data-go="${dec==='approve'?'approve-yes':'decline-yes'}">Try again</button>`;
 const back=`<a class="btn btn-small btn-quiet" href="requests.html">Back to requests</a>`;
 const B=(tone,ic,html)=>`<div class="banner banner-${tone} result" role="${tone==='ok'?'status':'alert'}" tabindex="-1" data-focus>${icon(ic,'banner-icon')}<div>${html}</div></div>`;
 if(a==='ok'&&dec==='approve')return B('ok','check',`<p><strong>Added to ${theirAccount} ✓</strong> Order <span class="mono">${shortId(od.id)}</span> is now in their account.</p><ul class="small list-tight"><li>The private links in its emails no longer work.</li><li>They see “Added to your account”.</li></ul><p class="small proto">Prototype: nothing is saved.</p><div class="actions">${nextPending?`<a class="btn btn-small btn-primary" href="request.html?id=${shortId(nextPending.id)}">Next waiting request</a>`:''}${back}</div>`);
 if(a==='ok')return B('ok','check',`<p><strong>Declined ✓</strong> Nothing was added. They see “Couldn’t add” and are asked to contact us.</p>${S.note?`<p class="small">Your internal note (only you see it): “${esc(S.note)}”</p>`:''}<p class="small proto">Prototype: nothing is saved.</p><div class="actions">${nextPending?`<a class="btn btn-small btn-primary" href="request.html?id=${shortId(nextPending.id)}">Next waiting request</a>`:''}${back}</div>`);
 if(a==='order_owned')return B('bad','alert',`<p><strong>This order is already in another customer’s account,</strong> so it can’t be added to this one. Nothing changed, and the request is still waiting.</p><p class="small">The workspace doesn’t say which account.${isLink?' You can decline this request.':' Choose another order, or decline.'}</p><div class="actions">${isLink?'':'<button class="btn btn-small" type="button" data-go="review">Choose another order</button>'}<button class="btn btn-small" type="button" data-go="decline">Decline</button></div>`);
 if(a==='stale')return B('warn','alert',`<p><strong>This request changed while you were looking.</strong> It was decided on another device, or the customer withdrew it. Nothing was saved.</p><div class="actions"><a class="btn btn-small" href="request.html?id=${shortId(cl.id)}&latest=1">Show the latest</a></div>`);
 if(a==='not_link_order')return B('bad','alert',isLink?`<p><strong>That isn’t the order from their email link.</strong> For this request only order <span class="mono">${shortId(cl.order_id)}</span> can be added. Nothing changed.</p><div class="actions"><a class="btn btn-small" href="request.html?id=${shortId(cl.id)}">Reload the request</a></div>`:`<p><strong>That order can’t be found any more.</strong> Nothing changed. Search again and choose an order that’s in the list.</p><div class="actions"><button class="btn btn-small" type="button" data-go="review">Search again</button></div>`);
 if(a==='account_unavailable')return B('bad','alert',`<p><strong>Their account is disabled,</strong> so nothing can be added to it. Nothing changed.</p><div class="actions"><button class="btn btn-small" type="button" data-go="decline">Decline</button>${back}</div>`);
 if(a==='paused')return B('warn','clock',`<p><strong>Adding orders to accounts is paused for a moment</strong> while the data is being restored. Nothing changed.</p><p class="small">Try again in a few minutes.</p><div class="actions">${retry}</div>`);
 return B('warn','alert',`<p><strong>We couldn’t tell whether it was saved:</strong> the connection dropped before the answer arrived.</p><p class="small">Try again. It’s safe: the same decision is never applied twice.</p><div class="actions">${retry}</div>`)}
function decisionCard(){
 if(!pending)return `<section class="card decision sticky" aria-labelledby="dec-h"><h2 id="dec-h" class="card-h">Decision</h2><p class="small">${cl.state==='withdrawn'?'The customer withdrew this request.':`Already decided: “${esc(STATE_CHEF[cl.state][0])}”.`} Nothing to decide.</p><a class="btn btn-small" href="requests.html?tab=${cl.state}">Back to the list</a></section>`;
 let inner='';const od=picked();
 if(S.step==='review'){
  const add=isLink||od?`<button class="btn btn-primary" type="button" data-go="approve" data-focus>${isLink?'Add to their account':`Add order ${shortId(od.id)} to their account`}</button>`:`<button class="btn btn-primary" type="button" disabled aria-describedby="need-pick">Add to their account</button><p id="need-pick" class="small muted">Choose an order under “Find the order” first.</p>`;
  inner=`${!isLink&&od?`<p class="small">Chosen: ${esc(orderLine(od))} · name on the order <strong>${esc(od.customer_full_name)}</strong></p>`:''}<div class="actions stack">${add}<button class="btn" type="button" data-go="decline">Decline</button></div><p class="small muted">Not sure? Leave it waiting and <a href="requests.html">go back</a>; nothing changes.</p>`}
 if(S.step==='approve')inner=od?confirmApprove(od):'<p class="small bad-text">Choose an order first.</p>';
 if(S.step==='decline')inner=declineForm();
 if(S.step==='busy')inner=`<div class="actions stack"><button class="btn ${S.decision==='approve'?'btn-primary':'btn-danger'}" type="button" disabled aria-busy="true"><span class="spinner light" aria-hidden="true"></span>${S.decision==='approve'?'Adding…':'Declining…'}</button></div><p class="small" role="status" tabindex="-1" data-focus>${S.decision==='approve'?'Adding the order to their account…':'Declining the request…'}</p>`;
 if(S.step==='done')inner=resultBlock();
 return `<section class="card decision sticky" aria-labelledby="dec-h"><h2 id="dec-h" class="card-h">Your decision</h2>${inner}<p class="small muted foot">Only you (chef) can decide. Every decision is recorded with your name and the time.</p></section>`}

const verb=pending?'asks':'asked';
const h1=cl.erased?'Request (details erased)':name?`${esc(name)} ${verb} to add ${isLink?'an order':'an older order'}`:`A customer ${verb} to add ${isLink?'an order':'an older order'}`;
// After a saved decision the page shows the new state (left column read only).
const savedNow=()=>pending&&S.step==='done'&&answerFor()==='ok';
function leftColumn(){if(!pending)return decidedCard()+sentCard()+whoCard();
 if(savedNow()){const od=picked();const added=S.decision==='approve'&&od?`<section class="card" aria-labelledby="ad-h"><h2 id="ad-h" class="card-h">The order you added</h2>${orderCard(od)}</section>`:'';return added+sentCard()+whoCard()}
 return isLink?linkOrderCard()+sentCard()+whoCard():sentCard()+whoCard()+findCard()}
function headState(){if(savedNow())return S.decision==='approve'?'approved':'declined';return cl.state}
function render(focus){const hs=headState();mount(shell(`<a class="back" href="requests.html${pending?'':`?tab=${cl.state}`}">${icon('back')} Requests to add orders</a>
<header class="page-head"><div><p class="eyebrow">Request <span class="mono ref">${shortId(cl.id)}</span> · ${hs==='pending'?esc(waitText(cl.created_at).toLowerCase()):esc(STATE_CHEF[hs][0].toLowerCase())}</p><h1>${h1}</h1></div>${chip(STATE_CHEF[hs][0],STATE_CHEF[hs][1])}</header>
<div class="claim-grid"><div>${leftColumn()}</div><div>${decisionCard()}</div></div>`,'claims',groups));
 keepUrl();if(focus){const f=document.querySelector('[data-focus]');if(f)f.focus()}}
render(false);

function go(step,decision){if(decision)S.decision=decision;S.step=step;render(true)}
const main=()=>document.querySelector('#main');
document.addEventListener('click',e=>{const b=e.target.closest('[data-go],[data-fill]');if(!b||!main().contains(b))return;
 if(b.dataset.fill!==undefined){S.term=b.dataset.fill;render(false);const i=document.querySelector('[data-find] input[name=q]');i.focus();say();return}
 const g=b.dataset.go;
 if(g==='review'){S.step='review';render(true);return}
 if(g==='approve'){go('approve','approve');return}
 if(g==='decline'){go('decline','decline');return}
 if(g==='approve-yes'||g==='decline-yes'){S.decision=g==='approve-yes'?'approve':'decline';busyThen()}});
document.addEventListener('submit',e=>{if(e.target.matches('[data-decline]')){e.preventDefault();S.note=e.target.note.value.trim().replace(/\s+/g,' ');S.decision='decline';busyThen()}if(e.target.matches('[data-find]'))e.preventDefault()});
function busyThen(){S.step='busy';render(true);setTimeout(()=>{S.step='done';render(true);toast('Prototype: nothing is saved.');
 // A retry after "answer lost" or "paused" succeeds in this preview (same key, same decision).
 if(S.answer==='lost'||S.answer==='paused')S.answer='ok'},900)}
function say(){const c=document.getElementById('find-count');if(c){const t=c.textContent;c.textContent='';setTimeout(()=>{c.textContent=t},30)}}
document.addEventListener('input',e=>{const f=e.target.closest('[data-find]');if(f&&e.target.name==='q'){S.term=e.target.value;document.getElementById('find-out').innerHTML=results();keepUrl()}
 if(e.target.closest('[data-decline]')&&e.target.name==='note'){S.note=e.target.value;document.getElementById('note-count').textContent=`${500-[...S.note].length} characters left · one line`}});
document.addEventListener('change',e=>{const f=e.target.closest('[data-find]');if(f&&(e.target.name==='from'||e.target.name==='to')){S[e.target.name]=e.target.value;const sp=monthsSpan(S.from,S.to);document.getElementById('find-out').innerHTML=sp<1||sp>3?`<p class="bad-text small" role="alert">Choose up to 3 months, with “to” on or after “from”.</p>`:results();const u=new URL(location.href);u.searchParams.set('from',S.from);u.searchParams.set('to',S.to);history.replaceState(null,'',u.pathname.split('/').pop()+u.search)}
 if(e.target.name==='pick'){S.pick=e.target.value;if(S.step!=='review')S.step='review';document.querySelectorAll('.pick').forEach(l=>l.classList.toggle('is-picked',l.querySelector('input').checked));document.querySelector('.decision').outerHTML=decisionCard();keepUrl()}});
})();
