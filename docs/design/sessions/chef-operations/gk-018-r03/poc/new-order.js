// GK-018 r03 — New order: a chef-entered (Instagram, email or in-person) order with a 24-hour payment link.
// 192 h or more is ordinary notice; under 192 h is the chef's deliberate choice (Creator answer 1; needs a K04 amendment).
// r03: K04 wording — "Can start paying until" (initiation) and "Paid in full by" (completion); a late completion is "Paid late · your decision".
'use strict';
(function(){
const startDate=q.get('date')&&q.get('date')>=TODAY?q.get('date'):'2026-10-24';
if(!can('neworder')){mount(shell(`<header class="page-head"><div><p class="eyebrow">New order</p><h1>New order</h1></div></header><div class="card"><p class="role-note">Entering orders: ${ONLY_CHEF} ${REVIEW.role==='support'?'Technical support has read-only access.':'Fulfilment helpers start, finish and hand over cakes.'}</p><a class="btn" href="orders.html">See orders</a></div>`,'plus'));return}
const variantOpts=Object.entries(CAKES).map(([k,c])=>`<option value="${k}">${esc(c.cake)} · ${esc(c.flavour)} · ${esc(c.size)} · ${money(c.price)}</option>`).join('');
const lotOpts=LOTS.filter(lotOffered).map(l=>{const c=CAKES[l.cake];return `<option value="${l.id}">${l.id} · ${l.kind==='resale'?'Resale':'Ready'} · ${esc(c.cake)} ${esc(c.flavour)} · ${money(l.price)} · use by ${dayShort(l.expires)}</option>`}).join('');

mount(shell(`<a class="back" href="orders.html">${icon('back')} Orders</a><header class="page-head"><div><p class="eyebrow">New order · entered by Elina</p><h1>New order</h1><p class="muted">For an order agreed on Instagram, by email or in person. The customer pays through the website with a link: they can start paying for up to 24 hours, and never once fewer than 192 hours remain unless you choose short notice.</p></div></header>
<form class="new-order" data-new novalidate>
<div class="detail-grid"><div class="detail-main">
<section class="card" aria-labelledby="s1"><h2 id="s1" class="card-h">1 · Customer</h2><fieldset class="seg"><legend>Agreed by</legend><label><input type="radio" name="source" value="instagram" checked> Instagram</label><label><input type="radio" name="source" value="email"> Email</label><label><input type="radio" name="source" value="person"> In person</label></fieldset>
<div class="form-grid"><label class="field">Customer’s full name<input name="name" autocomplete="off" value="Chloe A." required></label><label class="field">Email for order notices<input name="email" type="email" autocomplete="off" value="chloe.a@example.com" required></label><label class="check span2"><input type="checkbox" name="otherPayer"> Someone else is paying</label><label class="field span2" data-payer hidden>Payer’s name<input name="payer" autocomplete="off"></label></div></section>
<section class="card" aria-labelledby="s2"><h2 id="s2" class="card-h">2 · Cake</h2><fieldset class="seg"><legend>Kind of cake</legend><label><input type="radio" name="kind" value="menu" checked> From the menu</label><label><input type="radio" name="kind" value="custom"> Custom</label><label><input type="radio" name="kind" value="lot" ${lotOpts?'':'disabled'}> Finished cake (ready or resale)</label></fieldset>
<div class="form-grid"><label class="field span2" data-k="menu">Cake, flavour and size<select name="variant">${variantOpts}</select></label>
<label class="field span2" data-k="custom" hidden>Agreed design (what you and the customer agreed)<textarea name="design" rows="3">Garden bloom style, blush and sage palette, “Happy 30th Maya” in white piping.</textarea></label>
<label class="field" data-k="custom" hidden>Agreed price<input name="customPrice" inputmode="decimal" value="145.00"></label>
<label class="field" data-k="custom" hidden>Recipe for allergens<select name="customRecipe">${Object.entries(CAKES).map(([k,c])=>`<option value="${k}">${esc(c.flavour)} · ${c.recipe}</option>`).join('')}</select></label>
<label class="field span2" data-k="lot" hidden>Finished cake<select name="lot">${lotOpts}</select></label>
<label class="field" data-k="menu custom">How many cakes<input name="qty" type="number" min="1" max="20" value="1"></label></div>
<p class="allergens small" data-allergens></p></section>
<section class="card" aria-labelledby="s3"><h2 id="s3" class="card-h">3 · Date, time and handover</h2><div class="form-grid"><label class="field">Handover date<input name="date" type="date" min="${TODAY}" value="${startDate}"></label>
<fieldset class="seg"><legend>How</legend><label><input type="radio" name="mode" value="collection" checked> Collection</label><label><input type="radio" name="mode" value="courier"> Their courier</label><label><input type="radio" name="mode" value="delivery"> GK delivery</label></fieldset></div>
<fieldset class="slots" data-slots><legend>Time (London)</legend></fieldset><p class="small muted" data-mode-note></p></section>
<section class="card" aria-labelledby="s4"><h2 id="s4" class="card-h">4 · Day places and notice</h2><div data-places aria-live="polite"></div><div data-lead aria-live="polite"></div></section>
</div><div class="detail-side">
<section class="card sticky" aria-labelledby="s5"><h2 id="s5" class="card-h">5 · Price and payment link</h2><label class="field">Discount code (optional, one per order)<input name="code" autocomplete="off" placeholder="e.g. DEMO10"></label><div data-summary aria-live="polite"></div>
<fieldset class="checks"><legend class="small strong">The customer agreed (in your messages)</legend>${['the final price','the date and time','the size or servings','the flavour and filling','the design and details','collection or delivery'].map((t,k)=>`<label class="check"><input type="checkbox" name="agreed" value="${k}"> ${t}</label>`).join('')}</fieldset>
<div class="actions stack"><button class="btn btn-primary" type="submit">Create payment link</button></div><div data-errors role="alert"></div><div data-link aria-live="polite"></div>
<p class="muted small">The date isn’t held until the customer pays. While they pay, it’s held for up to 15 minutes. If they don’t start paying by the “can start paying until” time, you send a new link (which may then be short notice).</p></section>
</div></div></form>`,'plus'));

const f=document.querySelector('[data-new]');
let payKey=null,payVal=null,payPlan=null;
const setHTML=(sel,html)=>{const el=f.querySelector(sel);if(el.innerHTML&&el.contains(document.activeElement)&&document.activeElement!==el)return false;el.innerHTML=html;return true};
const val=n=>{const el=f.elements[n];return el instanceof RadioNodeList?el.value:el.type==='checkbox'?el.checked:el.value};
function pick(){const kind=val('kind');let c,price,recipeKey,lot=null;
 if(kind==='lot'){lot=LOTS.find(l=>l.id===val('lot'));c=CAKES[lot.cake];price=lot.price}else if(kind==='custom'){recipeKey=val('customRecipe');c=CAKES[recipeKey];price=pence(val('customPrice'))}else{recipeKey=val('variant');c=CAKES[recipeKey];price=c.price}
 const qty=kind==='lot'?1:Math.max(1,Math.min(20,Number(val('qty'))||1));return {kind,c,price,qty,lot}}
function render(){const p=pick();const kind=p.kind;
 f.querySelectorAll('[data-k]').forEach(el=>{el.hidden=!el.dataset.k.split(' ').includes(kind)});
 f.querySelector('[data-payer]').hidden=!val('otherPayer');
 f.querySelector('[data-allergens]').innerHTML=`<strong>Contains: ${ALLERGENS}${p.c.peanuts?', <span class="peanut">PEANUTS</span>':''}.</strong> From recipe ${esc(p.c.recipe)}${kind==='custom'?' — check any design extras against it':''}.`;
 // Date and slots from the published schedule's explicit starts.
 const dateEl=f.elements.date;if(p.lot&&p.lot.originalHandover){dateEl.value=when(p.lot.originalHandover,{year:'numeric',month:'2-digit',day:'2-digit'}).split('/').reverse().join('-');dateEl.disabled=true}else dateEl.disabled=false;
 if(p.lot)dateEl.max=p.lot.expires;else dateEl.removeAttribute('max');
 const date=dateEl.value||TODAY;const pastUseBy=p.lot&&date>p.lot.expires;const s=dayStats(date);const prevSlot=(f.querySelector('[name=slot]:checked')||{}).value;
 let slotsHtml='';const starts=s.hours?s.hours.starts:[];
 if(!s.hours)slotsHtml='<p class="small warn-text">No times on this day: it isn’t a working day in the published schedule.</p>';
 else if(s.status==='closed')slotsHtml='<p class="small warn-text">This is one of your closed dates. Choose another day, or reopen it from Calendar &amp; capacity.</p>';
 else if(s.status==='outside_horizon')slotsHtml=`<p class="small warn-text">After your bookable-until date (${dayShort(SCHEDULE.bookableUntil)}). You can still agree it, but check the date is concrete with the customer.</p>`;
 if(pastUseBy)slotsHtml+=`<p class="small bad-text">After this cake’s use-by date (${dayLabel(p.lot.expires)}). It can’t be sold for a handover after that: choose a date on or before it.</p>`;
 if(s.hours&&s.status!=='closed'){const fixed=p.lot&&p.lot.originalHandover?when(p.lot.originalHandover,{hour:'2-digit',minute:'2-digit',hourCycle:'h23'}):null;
  slotsHtml+=`<div class="slot-list">${starts.map(st=>{const past=!before(NOW,london(date,st));const dis=past||pastUseBy||(fixed&&st!==fixed);return `<label class="slot-opt ${dis?'is-off':''}"><input type="radio" name="slot" value="${st}" ${dis?'disabled':''} ${(prevSlot===st||fixed===st)&&!dis?'checked':''}> ${st}–${plusHour(st)}${past?' · passed':''}</label>`}).join('')}</div>${fixed?'<p class="small muted">A resale cake keeps its original handover time.</p>':''}`}
 const slotBox=f.querySelector('[data-slots]');setHTML('[data-slots]','<legend>Time (London)</legend>'+slotsHtml);if(!slotBox.querySelector('[name=slot]:checked')){const first=slotBox.querySelector('[name=slot]:not(:disabled)');if(first)first.checked=true}
 const mode=val('mode');f.querySelector('[data-mode-note]').textContent=mode==='delivery'?'GK delivery: you price it separately after this (from £100) and the customer pays it with its own 24-hour link. The cake can’t go out until delivery is paid.':mode==='courier'?'Their courier collects at St Katharine Docks and the customer pays the courier.':'Collection at St Katharine Docks is free. The full address goes out about 48 hours before.';
 const slot=(f.querySelector('[name=slot]:checked')||{}).value;
 // Day places: soft limit; chef extras sit outside the standard count but in the workload.
 let placesHtml='';
 if(kind==='lot')placesHtml=`<p class="small">A finished cake uses no day place (it’s already made). ${p.lot.id}: ${p.lot.qty-p.lot.reserved} available, reserved for this customer only when they pay.</p>`;
 else if(s.hours){const fits=s.committed+p.qty<=s.limit;const wasExtra=(f.querySelector('[name=place]:checked')||{}).value==='extra';
  placesHtml=`<p class="small"><strong>${dayShort(date)}:</strong> ${s.committed} of ${s.limit} standard places used${s.extras?`, +${s.extras} extra`:''}${s.tentative?`, ${s.tentative} customer paying now`:''} · workload ${s.workload} ${s.workload===1?'cake':'cakes'} ${chip(DAY_STATUS[s.status][0],DAY_STATUS[s.status][1])}</p>
  <fieldset class="choice"><legend>Places</legend><label class="${fits?'':'is-off'}"><input type="radio" name="place" value="standard" ${fits&&!wasExtra?'checked':''} ${fits?'':'disabled'}> Use ${p.qty===1?'a standard place':p.qty+' standard places'}${fits?'':` · not enough left (limit ${s.limit})`}</label><label><input type="radio" name="place" value="extra" ${!fits||wasExtra?'checked':''}> Extra, outside the standard ${s.limit} (counts in your workload: ${s.workload+p.qty} cakes)</label></fieldset>`}
 setHTML('[data-places]',placesHtml);
 // Notice (K04 r03): the link has two deadlines. "Can start paying until" (initiation) is the earlier of the
 // 24-hour link end and the last moment the lead time still holds; "Paid in full by" (completion) is when a
 // payment must succeed to be accepted automatically. A payment started in time that completes later becomes
 // "Paid late · your decision" (paid_awaiting_agreement), never accepted silently.
 // 192 h or more before handover: ordinary notice (no short-notice choice), even when the start window is under 24 h.
 // Under 192 h: the chef's deliberate choice (Creator answer 1), with a completion deadline no later than handover − PAY_BEFORE_H.
 let leadHtml='';payPlan=null;if(slot&&s.hours){const at=london(date,slot);const hNow=hoursBetween(NOW,at);const linkEnd=addH(NOW,24);const leadPoint=addH(at,-LEAD_H);const deadline=addH(at,-120);const isLot=kind==='lot';
  const key=date+' '+slot;if(payKey!==key){payKey=key;payVal=null}
  const short=hNow<LEAD_H;const cap=addH(at,-PAY_BEFORE_H);const earlier=(x,y)=>before(x,y)?x:y;const mins=t=>Math.round(hoursBetween(NOW,t)*60);
  const away=`Handover ${dayShort(date)}, ${slot} is <strong>${hNow<1?Math.round(hNow*60)+' minutes':Math.floor(hNow)+' hours'}</strong> away. Online orders need at least 192 hours (8 days) between payment and handover.`;
  const lateRule=t=>`If a payment they started in time completes after ${when(t)}, it isn’t accepted automatically: the order shows <strong>“Paid late · your decision”</strong> and you decide with the customer (make it if you can and they agree, or return everything paid).`;
  if(!short){const startBy=earlier(linkEnd,leadPoint);const win=hoursBetween(NOW,startBy);payPlan={startBy,completeBy:leadPoint,short:false};
   leadHtml=`<p class="small">${away}</p><div class="pay-deadlines small"><p>${icon('check','pill-icon')} <strong>Ordinary notice.</strong> No short-notice choice needed.</p>${before(leadPoint,linkEnd)?`<dl class="facts"><div><dt>Must start and finish paying by</dt><dd>${when(leadPoint)} · 192 hours before the handover, sooner than the usual 24 hours</dd></div></dl><p class="warn-text">Starting and finishing are the same moment here, so a payment method that takes longer to complete (for example a bank payment) will end up “Paid late · your decision”. Card payments usually complete straight away.</p>`:`<dl class="facts"><div><dt>Can start paying until</dt><dd>${when(startBy)} · 24 hours after you create the link</dd></div><div><dt>Paid in full by</dt><dd>${when(leadPoint)} · 192 hours before the handover</dd></div></dl>`}
   <p>${lateRule(leadPoint)}</p>${win<1?`<p class="warn-text">Only ${mins(startBy)} minutes to start paying. Make sure the customer is ready, or choose a later date.</p>`:''}</div>`}
  else if(!before(addH(NOW,0.5),cap)){payPlan={impossible:true};leadHtml=`<p class="small">${away}</p><div class="banner banner-bad small" role="alert">${icon('alert','banner-icon')}<div><p><strong>Too close to take payment.</strong> A payment must be complete at least ${PAY_BEFORE_H} hours before the handover (by ${when(cap)}), and that leaves less than 30 minutes. Choose a later time or date.</p></div></div>`}
  else{const def=[addH(at,-24),cap].filter(t=>before(addH(NOW,0.5),t)).sort((x,y)=>new Date(x)-new Date(y))[0];const chosen=payVal?fromLocalInput(payVal):def;
   const payBad=!chosen?'Enter a date and time.':!before(addH(NOW,0.5),chosen)?'Give the customer at least 30 minutes.':before(cap,chosen)?`It must be no later than ${when(cap)} (${PAY_BEFORE_H} hours before the handover).`:'';
   const completeBy=payBad?def:chosen;const startBy=earlier(linkEnd,completeBy);const hLatest=hoursBetween(completeBy,at);
   const help=payBad?`<span class="bad-text">${payBad}</span>`:`The customer can start paying until <strong>${when(startBy)}</strong>${before(linkEnd,completeBy)?' (24 hours after you create the link)':' (the same moment: the link closes then)'}. ${lateRule(completeBy)} The link never takes a payment after the handover.`;payPlan={startBy,completeBy,short:true,bad:payBad,help};
   const prevOk=(f.querySelector('[name=shortOk]')||{}).checked,prevWhy=(f.querySelector('[name=shortWhy]')||{}).value||'';
   leadHtml=`<p class="small">${away}</p><div class="banner banner-warn short-notice">${icon('alert','banner-icon')}<div><p><strong>Less than 192 hours’ notice.</strong> Online customers can’t book this. You can still take it as your deliberate choice.</p>
   <label class="field">Paid in full by (London time)<input name="payBy" type="datetime-local" value="${payVal||localInput(def)}" min="${localInput(addH(NOW,0.5))}" max="${localInput(cap)}" aria-describedby="pay-help"></label><p id="pay-help" class="small" aria-live="polite">${help}</p>
   ${!before(completeBy,deadline)?`<p class="small">The customer’s full-refund deadline (handover minus 120 hours: ${when(deadline)}) ${before(NOW,deadline)?'may pass before they pay':'has already passed'}. If they cancel after it, they get 50%. Tell them before they pay.</p>`:''}${hLatest<48?`<p class="small"><strong>Under 48 hours if paid at the latest time.</strong> The automatic production start would come after handover, so start making yourself. Whether you can go below 48 hours is still an open question for the owners.</p>`:''}
   <label class="check"><input type="checkbox" name="shortOk" ${prevOk?'checked':''}> I’ve checked I can make this in time — it’s my choice</label><label class="field">Why (kept with the order)<input name="shortWhy" value="${esc(prevWhy)}" autocomplete="off" placeholder="e.g. Regular customer, simple design"></label></div></div>
   <p class="design-note small"><strong>Design note:</strong> ${isLot?'K04 describes a chef-approved shorter notice for a specific finished cake (lead_time_exception: “Paid in full by” would be its latest confirmation, never after the handover or the cake’s use-by date), but it isn’t built yet: the backend currently refuses to start any payment under 192 hours (LEAD_TIME). Proposed only.':'Not yet allowed by the current rules: GK-001 lead_time says “no blanket reduction for ordinary made-to-order cakes”, GK-001 urgent_requests says online handover under 48 hours isn’t orderable and gives no blanket exception for chef-entered orders, and K04 allows a shorter lead only for a specific ready-made or resale cake (the backend refuses to start a payment under 192 hours: LEAD_TIME). The “GK-018 Creator answers / 2026-11-04” (answer 1) must first be added to the owner intake (docs/creator, checked by scripts/check_owner_intake.py) and K04 amended before this is built. Orders 192 hours or more away are ordinary and need none of this.'}</p>`}}
 if(!setHTML('[data-lead]',leadHtml)&&payPlan&&payPlan.short){const ph=f.querySelector('#pay-help');if(ph)ph.innerHTML=payPlan.help}
 // Price.
 const code=val('code').trim().toUpperCase();const cd=CODES[code];const sub=isNaN(p.price)?NaN:p.price*p.qty;const disc=cd&&cd.kind==='percentage'&&!isNaN(sub)?Math.floor((sub*cd.bps+5000)/10000):0;
 f.querySelector('[data-summary]').innerHTML=`<dl class="facts small"><div><dt>${esc(p.c.cake)}${p.qty>1?` × ${p.qty}`:''}</dt><dd>${isNaN(sub)?'—':money(sub)}</dd></div>${code?`<div><dt>${cd?esc(code)+' · '+esc(cd.label):esc(code)}</dt><dd>${cd?(disc?'−'+money(disc):'applied later'):'<span class="bad-text">not a current code</span>'}</dd></div>`:''}<div><dt class="strong">Customer pays now</dt><dd class="strong">${isNaN(sub)?'—':money(sub-disc)}</dd></div>${val('mode')==='delivery'?'<div><dt>GK delivery</dt><dd>quoted separately</dd></div>':''}</dl>`}
f.addEventListener('input',e=>{if(e.target.name==='payBy')payVal=e.target.value;render()});f.addEventListener('change',e=>{if(e.target.name==='payBy')payVal=e.target.value;render()});render();
f.addEventListener('submit',e=>{e.preventDefault();const p=pick();const errs=[];
 if(!val('name').trim())errs.push('Add the customer’s full name.');if(!f.elements.email.checkValidity()||!val('email').includes('.'))errs.push('Add a full email address for order notices.');
 if(val('otherPayer')&&!val('payer').trim())errs.push('Add the payer’s name.');if(isNaN(p.price)||p.price<=0)errs.push('Enter the agreed price, like 145.00.');
 if(!f.querySelector('[name=slot]:checked'))errs.push('Choose a handover time.');
 if(p.lot&&val('date')>p.lot.expires)errs.push(`${p.lot.id} must be handed over by its use-by date (${dayShort(p.lot.expires)}).`);
 if(payPlan&&payPlan.impossible)errs.push('Too close to the handover to take payment: choose a later time.');
 if(payPlan&&payPlan.bad)errs.push('Fix the “Paid in full by” time.');
 const so=f.querySelector('[name=shortOk]');if(so&&(!so.checked||!val('shortWhy').trim()))errs.push('Short notice: tick that it’s your choice and add why.');
 const code=val('code').trim();if(code&&!CODES[code.toUpperCase()])errs.push('That discount code isn’t current. Remove it or check the spelling.');
 if(f.querySelectorAll('[name=agreed]:checked').length<6)errs.push('Tick each detail the customer agreed (price, date, size, flavour, design, handover).');
 const box=f.querySelector('[data-errors]');const out=f.querySelector('[data-link]');
 if(errs.length){box.innerHTML=`<div class="banner banner-bad small"><div><p><strong>Before creating the link:</strong></p><ul>${errs.map(x=>`<li>${x}</li>`).join('')}</ul></div></div>`;out.innerHTML='';return}
 box.innerHTML='';out.innerHTML=`<div class="banner banner-ok" tabindex="-1">${icon('check','banner-icon')}<div><p><strong>Payment link ready (preview).</strong> Order GK-1077 · waiting for payment.</p><p class="small"><code>https://gkcake.com/pay/•••••••• (example)</code></p><p class="small">${payPlan?(payPlan.startBy===payPlan.completeBy?`The customer must start and finish paying by <strong>${when(payPlan.completeBy)}</strong>${payPlan.short?'':' (192 hours before the handover)'}; a payment completing later shows as “Paid late · your decision”.`:`${payPlan.short?'Short notice (your choice). ':''}The customer can start paying until <strong>${when(payPlan.startBy)}</strong>. Accepted automatically if paid in full by <strong>${when(payPlan.completeBy)}</strong>${payPlan.short?'':' (192 hours before the handover)'}; a payment completing later shows as “Paid late · your decision”.`):''} Sending the same link again doesn’t extend it.</p><div class="actions"><button class="btn btn-small" type="button" data-copy>Copy link</button><button class="btn btn-small" type="button" data-copy>Email it from “GK Cakes”</button></div></div></div>`;out.firstElementChild.focus();toast('Preview only: no order or link was created.')});
f.addEventListener('click',e=>{if(e.target.closest('[data-copy]'))toast('Preview only: nothing was copied or sent.')});
})();
