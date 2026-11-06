// GK-018 r04d-claims — decide one request (proposal, not approved).
// Live calls it stands for:
//   read one: none today (the page finds the request in the four list states); proposed GET /api/v1/admin/order-claims/{id}
//   approve: POST /api/v1/admin/order-claims/{id}/decision {expected_version, decision: "approve", order_id, note?}
//   decline: POST /api/v1/admin/order-claims/{id}/decision {expected_version, decision: "decline", note?}
//   with an Idempotency-Key kept for retries of the same decision.
// Finding an order for an older-order request reads GET /api/v1/admin/orders?from&to (at most 93 days) and filters
// in the browser by the start of the order ID or the name on the order (the API has no text search). Every page of the
// range is loaded first (100 a page, up to 10 pages, like the built Orders page).
'use strict';
(function(){
const STEPS=['review','approve','decline','busy','done'];
const ANSWERS=[['ok','Saved'],['order_owned','In another account'],['stale','Changed meanwhile'],['stale_closed','Changed: account deleted'],['not_link_order','Not that order (422)'],['account_unavailable','Account disabled'],['paused','Paused (503)'],['lost','Answer lost']];
const APPROVE_ONLY=['order_owned','not_link_order','account_unavailable'];
const cl0=claimById(q.get('id')||'9a3f2c71');
const groups=[['answer','Simulated answer',ANSWERS,'ok']];
if(!isChef){mount(shell(`<a class="back" href="requests.html">${icon('back')} Requests to add orders</a><header class="page-head"><div><p class="eyebrow">Account requests</p><h1>Request to add an order</h1></div></header>${roleNote}`,'claims',groups));return}
if(!cl0){mount(shell(`<a class="back" href="requests.html">${icon('back')} Requests to add orders</a><div class="card empty"><h1>Request not found</h1><p>This request isn’t in the design preview. The live workspace answers “not found” for an unknown request.</p><a class="btn" href="requests.html">Back to requests</a></div>`,'claims',groups));return}
const cl=JSON.parse(JSON.stringify(cl0));
// "Show the latest" after a stale answer (needs the proposed single-request read): withdrawn meanwhile, or closed
// because the customer deleted their account (free text erased).
if(q.get('latest')==='1'&&cl.state==='pending'){cl.state='withdrawn';cl.version+=1;cl.decided_at='2026-11-06T10:29:00Z'}
if(q.get('latest')==='closed'&&cl.state==='pending'){cl.state='closed';cl.version+=1;cl.erased=true;cl.reference=null;cl.note=null;cl.approximate_date=null;cl.account={id:cl.account.id,full_name:null,instagram:null,phone:null}}
const isLink=cl.path==='email_link';
const pending=cl.state==='pending';
const linkOrder=isLink?orderById(cl.order_id):null;
const name=cl.erased?null:cl.account.full_name;
const theirAccount=name?`${esc(name)}’s account`:'their account';
const same=pending&&isLink?ALL_CLAIMS.filter(x=>x.state==='pending'&&x.order_id===cl.order_id&&x.id!==cl.id):[];
const nextPending=CLAIMS.filter(x=>x.state==='pending'&&x.id!==cl.id&&new Date(x.created_at)>=new Date(cl.created_at))[0];

const S={confirmed:q.get('confirmed')==='1',step:STEPS.includes(q.get('step'))?q.get('step'):'review',decision:q.get('decision')==='decline'?'decline':'approve',pick:q.get('pick')||'',answer:ANSWERS.some(a=>a[0]===q.get('answer'))?q.get('answer'):'ok',note:q.get('note')||'',term:q.get('q')||''};
if(S.step==='decline')S.decision='decline';if(S.step==='approve')S.decision='approve';
if(isLink)S.pick=shortId(cl.order_id);
const answerFor=()=>S.decision==='decline'&&APPROVE_ONLY.includes(S.answer)?'ok':S.answer;
// Path c ("evidence") adds a website order on the strength of a reference that isn't secret: the chef confirms from
// the order's contact email first (K00: verified account + order evidence + chef confirmation). Path a (future) proves
// guest access instead.
const needsCheck=()=>!isLink;
const NOTE_MAX=500;const noteLen=v=>[...v].length;
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
 set('step',S.step,'review');set('decision',S.step==='busy'||S.step==='done'?S.decision:'','');set('pick',isLink?'':S.pick,'');
 // The search term and the internal note are never written back to the URL (personal data); the live page keeps
 // nothing personal in the URL, as the built Orders page does. Review links may still open a state with ?q= / ?note=.
 u.searchParams.delete('q');u.searchParams.delete('note');u.searchParams.delete('confirmed');history.replaceState(null,'',u.pathname.split('/').pop()+u.search)}

const orderCard=(od,extra='')=>`<div class="order-sum"><img src="assets/${IMG[od.cake]}.png" alt="" width="72" height="72"><div><p class="strong">Order <span class="mono">${shortId(od.id)}</span> · ${esc(CAKES[od.cake][0])} ${lifeChip(od)}</p><p class="small muted">${esc(CAKES[od.cake][1])} · ${esc(CAKES[od.cake][2])}</p><p class="small">${dayShort(od.date)}, ${od.slot} · ${MODES[od.mode]}</p><p class="small">Name on the order: <strong>${esc(od.customer_full_name)}</strong> · ${money(od.total)}</p>${extra}</div></div>`;

// ---------- left column
function sentCard(cls=''){
 const rows=isLink?[['How they asked',`<span class="how-chip">${icon('link','how-icon')}From the order page or email</span><span class="tag-future">Future</span>`],['Their note',cl.note?`<q>${esc(cl.note)}</q>`:'<span class="muted">No note (optional on this path)</span>']]
  :[['How they ordered',`<span class="how-chip">${esc(howLabel(cl))}</span>`],['Their reference',cl.erased?'<span class="muted">Erased</span>':`<span class="mono strong">${esc(cl.reference)}</span>${looksLikeOrderRef(cl.reference)&&pending?'<span class="block small info-text">Looks like a website order reference</span>':''}`],['Roughly when',cl.erased?'<span class="muted">Erased</span>':cl.approximate_date?esc(monthLabel(cl.approximate_date)):'<span class="muted">Not given</span>'],['Their note',cl.erased?'<span class="muted">Erased</span>':`<q>${esc(cl.note)}</q>`]];
 return `<section class="card ${cls}" aria-labelledby="sent-h"><h2 id="sent-h" class="card-h">What they sent</h2>${facts([['Received',`${when(cl.created_at)}`],...rows])}
 ${isLink?`<p class="small muted">They asked while signed in, from the order’s guest page (later also from the link in the order email). Having guest access shows they can see the order; it doesn’t prove the order is theirs: anyone with the link or that browser could ask.</p>`:cl.erased?'':`<p class="small muted">The reference is whatever they typed: yours from a message or receipt, or the “Order reference” from a website order’s page. It isn’t checked against anything.</p>`}</section>`}
const whoCard=(cls='')=>`<section class="card ${cls}" aria-labelledby="who-h"><h2 id="who-h" class="card-h">Who’s asking · from their profile</h2>${cl.erased?'<p class="muted">The customer deleted their account, so their profile and what they wrote were erased. A record that this account and request existed stays, without personal details.</p>':facts([['Name',name?esc(name):'<span class="muted">Not given</span>'],['Instagram',cl.account.instagram?`<span class="mono">${esc(cl.account.instagram)}</span>`:'<span class="muted">Not given</span>'],['Phone',cl.account.phone?`<span class="mono">${esc(cl.account.phone)}</span>`:'<span class="muted">Not given</span>']])+`<p class="small muted">Typed by the customer in their profile. Not checked and not proof of who they are. Their email address isn’t shown: the workspace doesn’t store it.</p>`}</section>`;
function linkOrderCard(){return `<section class="card" aria-labelledby="lo-h"><h2 id="lo-h" class="card-h">The order they asked from</h2>${linkOrder?orderCard(linkOrder,`<p class="small"><a href="order.html?id=${shortId(linkOrder.id)}">Open order ${shortId(linkOrder.id)}</a></p>`):'<p class="muted">This order isn’t in the preview.</p>'}
 ${same.length?`<div class="banner banner-warn small">${icon('alert','banner-icon')}<div><p><strong>Another request asks for this same order.</strong> ${same.map(x=>`Request <a class="mono" href="request.html?id=${shortId(x.id)}">${shortId(x.id)}</a> from ${esc(whoName(x))} (${when(x.created_at)})`).join('; ')}. Only one account can have the order: if you add it here, adding it for the other request is refused.</p></div></div>`:''}
 <p class="small muted">For a request from the order page, only this order can be added.</p></section>`}
function findCard(){const sp=monthsSpan(S.from,S.to);
 return `<section class="card" aria-labelledby="find-h"><h2 id="find-h" class="card-h">${icon('search')}Find the order</h2><p class="small muted">Looks only at orders already in the system (website orders). Choosing one is your judgement: a matching name or reference isn’t proof.</p>
 <form class="find-form" data-find role="search" novalidate><label class="field find-term"><span>Start of the order ID, or the name on the order</span><span class="search-box">${icon('search')}<input type="search" name="q" value="${esc(S.term)}" placeholder="e.g. 8b20 or Becker" autocomplete="off" aria-describedby="find-hint"></span></label>
 <label class="field">Handover from<select name="from">${MONTH_OPTS.map(m=>`<option value="${m}" ${m===S.from?'selected':''}>${monthLabel(m)}</option>`).join('')}</select></label>
 <label class="field">to<select name="to">${MONTH_OPTS.map(m=>`<option value="${m}" ${m===S.to?'selected':''}>${monthLabel(m)}</option>`).join('')}</select></label></form>
 <p id="find-hint" class="small muted">Up to 3 months at a time (the order list reads at most 93 days); every order in those months is loaded before filtering. ${cl.approximate_date?`Starts around their month, ${esc(monthLabel(cl.approximate_date))}.`:'They gave no month: starts with the last 3 months.'}</p>
 ${cl.erased?'':`<div class="quick"><span class="small">Search for:</span> <button class="btn btn-small" type="button" data-fill="${esc(cl.reference)}">their reference “${esc(cl.reference)}”</button>${name?` <button class="btn btn-small" type="button" data-fill="${esc(name.split(' ').slice(-1)[0])}">their surname “${esc(name.split(' ').slice(-1)[0])}”</button>`:''}</div>`}
 <div id="find-out">${sp<1||sp>3?`<p class="bad-text small" role="alert">Choose up to 3 months, with “to” on or after “from”.</p>`:results()}</div></section>`}
function results(){const t=S.term.trim().toLowerCase();const inRange=ORDERS.filter(od=>od.date.slice(0,7)>=S.from&&od.date.slice(0,7)<=S.to);
 const list=inRange.filter(od=>!t||od.customer_full_name.toLowerCase().includes(t)||od.id.toLowerCase().startsWith(t)).sort((a,b)=>a.date.localeCompare(b.date));
 const range=`${monthLabel(S.from)}${S.from===S.to?'':' – '+monthLabel(S.to)}`;
 if(!inRange.length||!list.length)return `<p class="small strong" aria-live="polite" id="find-count">No orders found${t?` for “${esc(S.term)}”`:''} · handover ${range}</p>
  <div class="banner banner-info no-match">${icon('info','banner-icon')}<div><p><strong>No matching order in the system${inRange.length?'':' yet'}.</strong> ${looksLikeOrderRef(cl.reference)?'Their reference looks like a website order’s: check the months or try its first characters.':`${cl.channel==='instagram'?'Instagram orders':'Orders made by email, phone or in person'} from before the website aren’t in the system, so they can’t be added to an account yet. Recording older orders by hand comes later (GK-027/GK-038, not built).`}</p>
  <p class="small">What you can do now:</p><ul class="small list-tight"><li><strong>Decline.</strong> The customer sees only “Couldn’t add … from the details given” and is asked to contact us, which can read as if their details were wrong. Once older orders can be recorded they’d have to ask again. You can add an internal note for yourself.</li><li><strong>Leave it waiting.</strong> They keep seeing “Waiting for the chef”, and it counts towards their limit of 5 waiting requests.</li>${inRange.length?'<li>Try another name, ID or months.</li>':''}</ul></div></div>`;
 return `<p class="small strong" aria-live="polite" id="find-count">${list.length} ${list.length===1?'order':'orders'} · handover ${range}${t?` · matching “${esc(S.term)}”`:''}</p>
 <fieldset class="pick-list"><legend class="sr-only">Choose the order to add</legend>${list.map(od=>`<label class="pick ${S.pick===shortId(od.id)?'is-picked':''}"><input type="radio" name="pick" value="${shortId(od.id)}" ${S.pick===shortId(od.id)?'checked':''}><span><span class="strong"><span class="mono">${shortId(od.id)}</span> · ${esc(od.customer_full_name)}</span> ${lifeChip(od)}<span class="block small muted">${esc(CAKES[od.cake][0])} · ${dayShort(od.date)}, ${od.slot} · ${money(od.total)}</span></span></label>`).join('')}</fieldset>
 <p class="small muted">This list can’t show whether an order is already in someone’s account. If it is, adding it is refused and nothing changes.</p>`}
// An order the account owner could still act on (api/README: the owner (bearer) may preview and cancel, move the pickup
// time on the same day, and answer a late-payment offer). Handed over or cancelled orders can only be seen.
const actionable=od=>od&&!['completed','cancelled'].includes(od.life);
// The customer's own move: collection and customer-courier orders only, at least 6 hours before the current start, to
// another published start that day (api/README "Customer move"; BO-GK-001.move_time). GK delivery: chef only.
const canMove=od=>actionable(od)&&od.mode!=='gk_delivery';
const ownerCan=od=>canMove(od)?'can cancel it and may be able to move its pickup time':'can cancel it';
// A one-off code the chef makes up for this check (example derived from the request ID, so screenshots are stable).
const checkCode=()=>String(parseInt(cl.id.slice(0,4),16)%9000+1000);
function evidenceCard(){const od=picked();if(!needsCheck()||!pending||!od||savedNow())return '';
 const code=checkCode();
 const msg=`Hello! Someone has asked to add your GK Cake London order (${orderLine(od)}) to their account on our website. If that was you, please reply to this email with the code ${code} and the request reference shown in your account under “Your requests”. If it wasn’t you, just tell us and nothing will be added.`;
 return `<section class="card card-alert" aria-labelledby="ev-h"><h2 id="ev-h" class="card-h">${icon('alert')}Before adding: confirm it’s theirs</h2>
 <p class="small">A website order’s reference isn’t secret: it’s on the order page, in screenshots and anywhere the buyer shared it. Their profile isn’t proof either. Confirm through the order’s contact email before you add it.</p>
 ${facts([['Contact email on this order',`<span class="mono">${esc(od.contact_email)}</span> <span class="chip-s chip-neutral">chef only</span>`],['A code you make up for this check',`<span class="mono">${code}</span> <span class="small muted">(example)</span>`]])}
 <ol class="steps small"><li><strong>You</strong> email <span class="mono">${esc(od.contact_email)}</span> from <span class="mono">${BUSINESS_EMAIL}</span> with the code, and <strong>without</strong> the request reference (message below).</li><li>Wait for a reply <strong>from that address</strong> that quotes your code <em>and</em> the request reference <span class="mono">${shortId(cl.id)}</span>. Only someone who reads that mailbox has the code; only the person signed in to this account can see the reference.</li><li>If nobody replies, the reply lacks either, or the buyer says it wasn’t them, don’t add it: decline, or leave it waiting.</li></ol>
 ${actionable(od)?`<p class="small wrong-person">${icon('alert','note-icon')}<span>This order is still upcoming: whoever it’s added to ${ownerCan(od)}. Take extra care, or wait for the order-page route (question 3).</span></p>`:''}
 <details class="draft"><summary class="btn btn-small">Show the email to the order’s address</summary><div class="draft-body"><p class="small">To: <span class="mono">${esc(od.contact_email)}</span></p><p class="mail-body">${esc(msg)}</p><button class="btn btn-small" type="button" data-copy>Copy the email</button><p class="small muted">Never include the request reference in this email: the real buyer could reply with it and confirm someone else’s request. Prototype: nothing is copied or sent.</p></div></details>
 <p class="small muted">Why not ask the requester to email you from that address? A sender address can be forged, and email warnings catch only some of it. Your email to the address, with a code, can’t be answered without reading that mailbox, and it also tells the real buyer if someone else asked. Once the order page’s “Add this order to my account” exists (GK-016), a request from there proves guest access instead (questions 3 and 6). Matching the address is your check, never automatic.</p></section>`}
function decidedCard(){const od=cl.order_id&&orderById(cl.order_id);const [t,tone]=STATE_CHEF[cl.state];
 if(cl.state==='closed')return `<section class="card" aria-labelledby="out-h"><h2 id="out-h" class="card-h">Outcome</h2><p>${chip(t,tone)}</p><p>The customer deleted their account while this request was waiting, so it was closed and what they wrote was erased. Nothing was added.</p><p class="small muted">Closed requests don’t appear in any list (the list reads only waiting, added, couldn’t add and withdrawn). Only the proposed single-request read could show this page; without it the request simply disappears from Waiting.</p></section>`;
 return `<section class="card" aria-labelledby="out-h"><h2 id="out-h" class="card-h">Outcome</h2><p>${chip(t,tone)}${cl.account_deleted?' '+chip('Account deleted since','warn'):''}</p>${facts([[cl.state==='withdrawn'?'Withdrawn':'Decided',`${when(cl.decided_at)}${cl.state==='withdrawn'?' by the customer':` by ${decidedBy(cl)}`}`],cl.state==='approved'&&od?['Order added',`<a class="mono" href="order.html?id=${shortId(od.id)}">${shortId(od.id)}</a> · ${esc(orderLine(od))}`]:null,cl.state==='approved'||cl.state==='declined'?['Your internal note',cl.erased?'<span class="muted">Erased with the account</span>':cl.decision_note?esc(cl.decision_note):'<span class="muted">None</span>']:null,['The customer sees',cl.erased?'<span class="muted">Nothing: the account was deleted</span>':CUSTOMER_SEES[cl.state]]])}
 ${cl.account_deleted?`<p class="small"><strong>The order is no longer in any account.</strong> Deleting the account unlinked all its orders; the order itself stays for its retention period. (Shown with a proposed <code>account_deleted</code> flag: the list doesn’t say this today.)</p>`:''}
 ${cl.state==='approved'&&!cl.account_deleted?'<p class="small muted">When it was added, guest access to that order stopped working; they see it by signing in. Removing an order from an account isn’t built.</p>':''}${cl.state==='withdrawn'?'<p class="small muted">Nothing was added. A withdrawn request can’t be decided.</p>':''}</section>`}

// ---------- right column: the decision
const noteField=(ph,label='Internal note (optional) · only you see it, never the customer')=>`<label class="field">${label}<input name="note" value="${esc(S.note)}" autocomplete="off" aria-describedby="note-count note-err" placeholder="${ph}"></label><p class="small muted counter" id="note-count">${NOTE_MAX-noteLen(S.note)} characters left · one line</p><p class="small bad-text" id="note-err" role="alert"></p>`;
// Owner capabilities from api/README: cancellation preview/cancel, same-day pickup move, late-payment answer, each for
// "the owner (bearer)". A cancellation refund is reserved against the order's own payment collections and sent through
// the payment provider (Stripe), not to the person who cancels.
const wrongPerson=od=>actionable(od)
 ?`If this is the wrong person, they see the order in their account, including the name and contact email (${esc(od.contact_email)}) on it and the handover details. Because it’s still upcoming, they can also <strong>cancel it</strong> (the refund, 100% or 50% under the cancellation policy, goes back to the order’s own payment, not to them)${canMove(od)?', may be able to <strong>move its pickup time</strong> to another start that day (until 6 hours before the current start)':''}, or answer a late-payment question if there is one. The real buyer’s guest access stops, so they couldn’t step in, and there’s no undo here (removing an order from an account isn’t built).`
 :`If this is the wrong person, they see the order in their account, including the name and contact email (${esc(od.contact_email)}) on it and the handover details; the real buyer’s guest access stops; and there’s no undo here (removing an order from an account isn’t built). The order is ${od.life==='cancelled'?'cancelled':'handed over'}, so they can’t change it.`;
function confirmApprove(od){const chk=needsCheck();return `<form class="confirm-box" data-approve novalidate aria-labelledby="ca-h"><p id="ca-h" class="strong" tabindex="-1" data-focus>Add order <span class="mono">${shortId(od.id)}</span> to ${theirAccount}?</p>
 <ul class="small list-tight"><li>The order appears in their account, in their order list.</li><li>${esc(GUEST_GONE)}</li><li>Their request shows “Added to your account”.</li><li>If the order is already in their account, it’s just marked added; nothing else changes.</li></ul>
 <p class="small wrong-person">${icon('alert','note-icon')}<span>${wrongPerson(od)}</span></p>
 ${chk?`<label class="check"><input type="checkbox" name="confirmed" ${S.confirmed?'checked':''} aria-describedby="conf-err"><span>I emailed the order’s contact address (<span class="mono">${esc(od.contact_email)}</span>) and got a reply from it quoting my code and their request reference</span></label><p class="small bad-text" id="conf-err" role="alert"></p>`:`<p class="small muted">They asked from the order’s guest page, which shows they had guest access.</p>`}
 ${chk?noteField(`e.g. Emailed ${esc(od.contact_email)} 6 Nov with code ${checkCode()}; reply quoted ${checkCode()} and ${shortId(cl.id)}`,'How you confirmed (required) · internal note, never shown to the customer'):noteField('e.g. Asked from the order page; name matches')}
 <div class="actions"><button class="btn btn-primary" type="submit">Yes, add to their account</button><button class="btn btn-quiet" type="button" data-go="review">Not yet</button></div></form>`}
function declineForm(){return `<form class="confirm-box" data-decline novalidate aria-labelledby="cd-h"><p id="cd-h" class="strong" tabindex="-1" data-focus>Decline request <span class="mono">${shortId(cl.id)}</span>?</p>
 <ul class="small list-tight"><li>Nothing is added to their account.</li><li>They see only “Couldn’t add”, with “from the details given” and a request to contact us: no reason, and nothing about any order.</li>${isLink?`<li>${esc(GUEST_KEPT)}</li>`:''}</ul>
 ${noteField('e.g. No historical Instagram orders in the system yet')}
 <div class="actions"><button class="btn btn-danger" type="submit">Decline request</button><button class="btn btn-quiet" type="button" data-go="review">Not yet</button></div></form>`}
function resultBlock(){const a=answerFor(),od=picked(),dec=S.decision;
 const retry=`<button class="btn btn-small" type="button" data-go="${dec==='approve'?'approve-yes':'decline-yes'}">Try again</button>`;
 const back=`<a class="btn btn-small btn-quiet" href="requests.html">Back to requests</a>`;
 const B=(tone,ic,html)=>`<div class="banner banner-${tone} result" role="${tone==='ok'?'status':'alert'}" tabindex="-1" data-focus>${icon(ic,'banner-icon')}<div>${html}</div></div>`;
 if(a==='ok'&&dec==='approve')return B('ok','check',`<p><strong>Added to ${theirAccount} ✓</strong> Order <span class="mono">${shortId(od.id)}</span> is now in their account.</p><ul class="small list-tight"><li>Guest access to the order no longer works; they see it by signing in.</li><li>They see “Added to your account”.</li></ul>${S.note?`<p class="small">Your internal note (only you see it): “${esc(S.note)}”</p>`:''}<p class="small proto">Prototype: nothing is saved.</p><div class="actions">${nextPending?`<a class="btn btn-small btn-primary" href="request.html?id=${shortId(nextPending.id)}">Next waiting request</a>`:''}${back}</div>`);
 if(a==='ok')return B('ok','check',`<p><strong>Declined ✓</strong> Nothing was added. They see “Couldn’t add” and are asked to contact us.</p>${S.note?`<p class="small">Your internal note (only you see it): “${esc(S.note)}”</p>`:''}<p class="small proto">Prototype: nothing is saved.</p><div class="actions">${nextPending?`<a class="btn btn-small btn-primary" href="request.html?id=${shortId(nextPending.id)}">Next waiting request</a>`:''}${back}</div>`);
 if(a==='order_owned')return B('bad','alert',`<p><strong>This order is already in another customer’s account,</strong> so it can’t be added to this one. Nothing changed, and the request is still waiting.</p><p class="small">The workspace doesn’t say which account.${isLink?' You can decline this request.':' Choose another order, or decline.'}</p><div class="actions">${isLink?'':'<button class="btn btn-small" type="button" data-go="review">Choose another order</button>'}<button class="btn btn-small" type="button" data-go="decline">Decline</button></div>`);
 if(a==='stale')return B('warn','alert',`<p><strong>This request changed while you were looking.</strong> It was decided on another device, withdrawn by the customer, or closed because they deleted their account. Nothing was saved.</p><div class="actions"><a class="btn btn-small" href="request.html?id=${shortId(cl.id)}&latest=1">Show the latest</a></div><p class="small">“Show the latest” needs the proposed single-request read.</p>`);
 if(a==='stale_closed')return B('warn','alert',`<p><strong>This request changed while you were looking.</strong> It was decided on another device, withdrawn by the customer, or closed because they deleted their account. Nothing was saved.</p><div class="actions"><a class="btn btn-small" href="request.html?id=${shortId(cl.id)}&latest=closed">Show the latest</a></div><p class="small">“Show the latest” needs the proposed single-request read.</p>`);
 if(a==='not_link_order')return B('bad','alert',isLink?`<p><strong>That isn’t the order they asked from.</strong> For this request only order <span class="mono">${shortId(cl.order_id)}</span> can be added. Nothing changed.</p><div class="actions"><a class="btn btn-small" href="request.html?id=${shortId(cl.id)}">Reload the request</a></div>`:`<p><strong>That order can’t be found any more.</strong> Nothing changed. Search again and choose an order that’s in the list.</p><div class="actions"><button class="btn btn-small" type="button" data-go="review">Search again</button></div>`);
 if(a==='account_unavailable')return B('bad','alert',`<p><strong>Their account is disabled,</strong> so nothing can be added to it. Nothing changed.</p><div class="actions"><button class="btn btn-small" type="button" data-go="decline">Decline</button>${back}</div>`);
 if(a==='paused')return B('warn','clock',`<p><strong>Adding orders to accounts is paused for a moment</strong> while the data is being restored. Nothing changed.</p><p class="small">Try again in a few minutes.</p><div class="actions">${retry}</div>`);
 return B('warn','alert',`<p><strong>We couldn’t tell whether it was saved:</strong> the connection dropped before the answer arrived.</p><p class="small">Try again. It’s safe: the same decision is never applied twice.</p><div class="actions">${retry}</div>`)}
function decisionCard(){
 if(!pending)return `<section class="card decision sticky" aria-labelledby="dec-h"><h2 id="dec-h" class="card-h">Decision</h2><p class="small">${cl.state==='withdrawn'?'The customer withdrew this request.':cl.state==='closed'?'Closed when the customer deleted their account.':`Already decided: “${esc(STATE_CHEF[cl.state][0])}”.${cl.account_deleted?' The customer has since deleted their account, so the order is in no account.':''}`} Nothing to decide.</p><a class="btn btn-small" href="requests.html${cl.state==='closed'?'':`?tab=${cl.state}`}">Back to the list</a></section>`;
 let inner='';const od=picked();
 if(S.step==='review'){
  const add=isLink||od?`<button class="btn btn-primary" type="button" data-go="approve" data-focus>${isLink?'Add to their account':`Add order ${shortId(od.id)} to their account`}</button>`:`<button class="btn btn-primary" type="button" disabled aria-describedby="need-pick">Add to their account</button><p id="need-pick" class="small muted">Choose an order under “Find the order” first.</p>`;
  inner=`${!isLink&&od?`<p class="small">Chosen: ${esc(orderLine(od))} · name on the order <strong>${esc(od.customer_full_name)}</strong> · contact email <span class="mono">${esc(od.contact_email)}</span></p><p class="small warn-text">Confirm through that address before adding (see “Before adding”).</p>${actionable(od)?`<p class="small bad-text">Still upcoming: whoever it’s added to ${ownerCan(od)}. Take extra care (see “Before adding”).</p>`:''}`:''}<div class="actions stack">${add}<button class="btn" type="button" data-go="decline">Decline</button></div><p class="small muted">Not sure? Leave it waiting and <a href="requests.html">go back</a>; nothing changes.</p>`}
 if(S.step==='approve')inner=od?confirmApprove(od):'<p class="small bad-text">Choose an order first.</p>';
 if(S.step==='decline')inner=declineForm();
 if(S.step==='busy')inner=`<div class="actions stack"><button class="btn ${S.decision==='approve'?'btn-primary':'btn-danger'}" type="button" disabled aria-busy="true"><span class="spinner light" aria-hidden="true"></span>${S.decision==='approve'?'Adding…':'Declining…'}</button></div><p class="small" role="status" tabindex="-1" data-focus>${S.decision==='approve'?'Adding the order to their account…':'Declining the request…'}</p>`;
 if(S.step==='done')inner=resultBlock();
 return `<section class="card decision sticky" aria-labelledby="dec-h"><h2 id="dec-h" class="card-h">Your decision</h2>${inner}<p class="small muted foot">Only you (chef) can decide. Every decision is recorded with your staff account and the time.</p></section>`}

const verb=pending?'asks':'asked';
const h1=cl.erased?'Request (details erased)':name?`${esc(name)} ${verb} to add ${isLink?'an order':'an older order'}`:`A customer ${verb} to add ${isLink?'an order':'an older order'}`;
// After a saved decision the page shows the new state (left column read only).
const savedNow=()=>pending&&S.step==='done'&&answerFor()==='ok';
function leftColumn(){if(!pending)return decidedCard()+sentCard('card-later')+whoCard('card-later');
 if(savedNow()){const od=picked();const added=S.decision==='approve'&&od?`<section class="card" aria-labelledby="ad-h"><h2 id="ad-h" class="card-h">The order you added</h2>${orderCard(od)}</section>`:'';return added+sentCard('card-later')+whoCard('card-later')}
 return isLink?linkOrderCard()+sentCard('card-later')+whoCard('card-later'):sentCard()+whoCard('card-later')+findCard()+`<div id="evidence-slot">${evidenceCard()}</div>`}
function headState(){if(savedNow())return S.decision==='approve'?'approved':'declined';return cl.state}
const futureBanner=()=>isLink?`<div class="banner banner-warn future-banner">${icon('clock','banner-icon')}<div><p><strong>Future state.</strong> ${esc(FUTURE_NOTE)}</p></div></div>`:'';
function render(focus){const hs=headState();mount(shell(`<a class="back" href="requests.html${pending||cl.state==='closed'?'':`?tab=${cl.state}`}">${icon('back')} Requests to add orders</a>
<header class="page-head"><div><p class="eyebrow">Request <span class="mono ref">${shortId(cl.id)}</span> · ${hs==='pending'?esc(waitText(cl.created_at).toLowerCase()):cl.account_deleted?'added · account deleted since':esc(STATE_CHEF[hs][0].toLowerCase())}</p><h1>${h1}</h1></div>${cl.account_deleted?chip('Added · account deleted since','warn'):chip(STATE_CHEF[hs][0],STATE_CHEF[hs][1])}</header>
${futureBanner()}<div class="claim-grid"><div class="claim-main">${leftColumn()}</div><div class="claim-side">${decisionCard()}</div></div>`,'claims',groups));
 keepUrl();if(focus){const f=document.querySelector('[data-focus]');if(f)f.focus()}}
render(false);

function go(step,decision){if(decision)S.decision=decision;S.step=step;render(true)}
const main=()=>document.querySelector('#main');
document.addEventListener('click',e=>{const cp=e.target.closest('[data-copy]');if(cp){toast('Prototype: nothing is copied or sent.');return}
 const b=e.target.closest('[data-go],[data-fill]');if(!b||!main().contains(b))return;
 if(b.dataset.fill!==undefined){S.term=b.dataset.fill;render(false);const i=document.querySelector('[data-find] input[name=q]');i.focus();say();return}
 const g=b.dataset.go;
 if(g==='review'){S.step='review';render(true);return}
 if(g==='approve'){go('approve','approve');return}
 if(g==='decline'){go('decline','decline');return}
 if(g==='approve-yes'||g==='decline-yes'){S.decision=g==='approve-yes'?'approve':'decline';busyThen()}});
document.addEventListener('submit',e=>{const f=e.target;if(f.matches('[data-find]')){e.preventDefault();return}
 if(!f.matches('[data-decline],[data-approve]'))return;e.preventDefault();const v=f.note.value.trim().replace(/\s+/g,' ');
 const approve=f.matches('[data-approve]'),chk=approve&&needsCheck();
 const confErr=chk&&!f.confirmed.checked?'Confirm through the order’s contact email first (your code and their reference in a reply from it), or choose “Not yet”.':'';
 const noteErr=noteLen(v)>NOTE_MAX?`The note can be up to ${NOTE_MAX} characters; it has ${noteLen(v)}.`:chk&&!v?'Say how you confirmed (kept as your internal note).':'';
 if(chk){document.getElementById('conf-err').textContent=confErr;if(confErr)f.confirmed.setAttribute('aria-invalid','true');else f.confirmed.removeAttribute('aria-invalid')}
 document.getElementById('note-err').textContent=noteErr;if(noteErr)f.note.setAttribute('aria-invalid','true');else f.note.removeAttribute('aria-invalid');
 if(confErr||noteErr){(confErr?f.confirmed:f.note).focus();return}
 if(chk)S.confirmed=true;S.note=v;S.decision=f.matches('[data-approve]')?'approve':'decline';busyThen()});
function busyThen(){S.step='busy';render(true);setTimeout(()=>{S.step='done';render(true);toast('Prototype: nothing is saved.');
 // A retry after "answer lost" or "paused" succeeds in this preview (same key, same decision).
 if(S.answer==='lost'||S.answer==='paused')S.answer='ok'},900)}
function say(){const c=document.getElementById('find-count');if(c){const t=c.textContent;c.textContent='';setTimeout(()=>{c.textContent=t},30)}}
document.addEventListener('input',e=>{const f=e.target.closest('[data-find]');if(f&&e.target.name==='q'){S.term=e.target.value;document.getElementById('find-out').innerHTML=results();keepUrl()}
 if(e.target.closest('[data-decline],[data-approve]')&&e.target.name==='note'){S.note=e.target.value;const left=NOTE_MAX-noteLen(S.note);const c=document.getElementById('note-count');c.textContent=left>=0?`${left} characters left · one line`:`${-left} characters too many`;c.classList.toggle('bad-text',left<0)}});
document.addEventListener('change',e=>{const f=e.target.closest('[data-find]');if(f&&(e.target.name==='from'||e.target.name==='to')){S[e.target.name]=e.target.value;const sp=monthsSpan(S.from,S.to);document.getElementById('find-out').innerHTML=sp<1||sp>3?`<p class="bad-text small" role="alert">Choose up to 3 months, with “to” on or after “from”.</p>`:results();const u=new URL(location.href);u.searchParams.set('from',S.from);u.searchParams.set('to',S.to);history.replaceState(null,'',u.pathname.split('/').pop()+u.search)}
 if(e.target.name==='pick'){S.pick=e.target.value;if(S.step!=='review')S.step='review';document.querySelectorAll('.pick').forEach(l=>l.classList.toggle('is-picked',l.querySelector('input').checked));document.querySelector('.decision').outerHTML=decisionCard();document.getElementById('evidence-slot').innerHTML=evidenceCard();keepUrl()}});
})();
