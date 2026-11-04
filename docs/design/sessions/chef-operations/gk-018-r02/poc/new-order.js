// GK-018 r02 — New order: a chef-entered (Instagram, email or in-person) order with a 24-hour payment link.
// Under 192 hours is allowed as the chef's deliberate choice (Creator answer 1; needs a K04 amendment).
'use strict';
(function(){
const startDate=q.get('date')&&q.get('date')>=TODAY?q.get('date'):'2026-10-24';
if(!can('neworder')){mount(shell(`<header class="page-head"><div><p class="eyebrow">New order</p><h1>New order</h1></div></header><div class="card"><p class="role-note">Entering orders: ${ONLY_CHEF} ${REVIEW.role==='support'?'Technical support has read-only access.':'Fulfilment helpers start, finish and hand over cakes.'}</p><a class="btn" href="orders.html">See orders</a></div>`,'plus'));return}
const variantOpts=Object.entries(CAKES).map(([k,c])=>`<option value="${k}">${esc(c.cake)} · ${esc(c.flavour)} · ${esc(c.size)} · ${money(c.price)}</option>`).join('');
const lotOpts=LOTS.filter(lotOffered).map(l=>{const c=CAKES[l.cake];return `<option value="${l.id}">${l.id} · ${l.kind==='resale'?'Resale':'Ready'} · ${esc(c.cake)} ${esc(c.flavour)} · ${money(l.price)} · use by ${dayShort(l.expires)}</option>`}).join('');

mount(shell(`<a class="back" href="orders.html">${icon('back')} Orders</a><header class="page-head"><div><p class="eyebrow">New order · entered by Elina</p><h1>New order</h1><p class="muted">For an order agreed on Instagram, by email or in person. The customer pays through the website with a link that works for 24 hours.</p></div></header>
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
<p class="muted small">The date isn’t held until the customer pays. While they pay, it’s held for up to 15 minutes. If they don’t start paying within 24 hours, you send a new link.</p></section>
</div></div></form>`,'plus'));

const f=document.querySelector('[data-new]');
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
 const date=dateEl.value||TODAY;const s=dayStats(date);const prevSlot=(f.querySelector('[name=slot]:checked')||{}).value;
 let slotsHtml='';const starts=s.hours?s.hours.starts:[];
 if(!s.hours)slotsHtml='<p class="small warn-text">No times on this day: it isn’t a working day in the published schedule.</p>';
 else if(s.status==='closed')slotsHtml='<p class="small warn-text">This is one of your closed dates. Choose another day, or reopen it from Calendar &amp; capacity.</p>';
 else if(s.status==='outside_horizon')slotsHtml=`<p class="small warn-text">After your bookable-until date (${dayShort(SCHEDULE.bookableUntil)}). You can still agree it, but check the date is concrete with the customer.</p>`;
 if(s.hours&&s.status!=='closed'){const fixed=p.lot&&p.lot.originalHandover?when(p.lot.originalHandover,{hour:'2-digit',minute:'2-digit',hourCycle:'h23'}):null;
  slotsHtml+=`<div class="slot-list">${starts.map(st=>{const past=!before(NOW,london(date,st));const dis=past||(fixed&&st!==fixed);return `<label class="slot-opt ${dis?'is-off':''}"><input type="radio" name="slot" value="${st}" ${dis?'disabled':''} ${(prevSlot===st||fixed===st)&&!dis?'checked':''}> ${st}–${plusHour(st)}${past?' · passed':''}</label>`}).join('')}</div>${fixed?'<p class="small muted">A resale cake keeps its original handover time.</p>':''}`}
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
 // Notice: 192 hours from payment to handover. Shown for paying now and at the end of the 24-hour link.
 let leadHtml='';if(slot&&s.hours){const at=london(date,slot);const hNow=hoursBetween(NOW,at),hLate=hNow-24;const deadline=addH(at,-120);const short=hLate<LEAD_H;const isLot=kind==='lot';
  leadHtml=`<p class="small">Handover ${dayShort(date)}, ${slot} is <strong>${Math.floor(hNow)} hours</strong> away if paid now, ${Math.max(0,Math.floor(hLate))} if paid at the end of the 24-hour link. Online orders need at least 192 hours (8 days).</p>`;
  if(!short)leadHtml+=`<p class="small ok-text">${icon('check','pill-icon')} Enough notice even if they pay at the end of the link.</p>`;
  else{const prevOk=(f.querySelector('[name=shortOk]')||{}).checked,prevWhy=(f.querySelector('[name=shortWhy]')||{}).value||'';
   leadHtml+=`<div class="banner banner-warn short-notice">${icon('alert','banner-icon')}<div><p><strong>Less than 192 hours’ notice.</strong> Online customers can’t book this. You can still take it — for any cake — as your deliberate choice.</p>${!before(NOW,deadline)||!before(addH(NOW,24),deadline)?`<p class="small">The customer’s full-refund deadline (handover minus 120 hours: ${when(deadline)}) ${before(NOW,deadline)?'may pass before they pay':'has already passed'}. If they cancel after it, they get 50%. Tell them before they pay.</p>`:''}${hNow<48?'<p class="small">Under 48 hours: the automatic production start would come after handover, so start making yourself.</p>':''}
   <label class="check"><input type="checkbox" name="shortOk" ${prevOk?'checked':''}> I’ve checked I can make this in time — it’s my choice</label><label class="field">Why (kept with the order)<input name="shortWhy" value="${esc(prevWhy)}" autocomplete="off" placeholder="e.g. Regular customer, simple design"></label></div></div>
   <p class="design-note small"><strong>Design note:</strong> ${isLot?'A chef-approved shorter notice for a specific finished cake is already in K04 (lead_time_exception).':'Needs a contract amendment: K04 allows a shorter lead only for a specific ready-made or resale cake. The “GK-018 Creator answers / 2026-11-04” (answer 1) extend it to any chef-entered or Instagram order when the chef chooses.'}</p>`}}
 setHTML('[data-lead]',leadHtml);
 // Price.
 const code=val('code').trim().toUpperCase();const cd=CODES[code];const sub=isNaN(p.price)?NaN:p.price*p.qty;const disc=cd&&cd.kind==='percentage'&&!isNaN(sub)?Math.floor((sub*cd.bps+5000)/10000):0;
 f.querySelector('[data-summary]').innerHTML=`<dl class="facts small"><div><dt>${esc(p.c.cake)}${p.qty>1?` × ${p.qty}`:''}</dt><dd>${isNaN(sub)?'—':money(sub)}</dd></div>${code?`<div><dt>${cd?esc(code)+' · '+esc(cd.label):esc(code)}</dt><dd>${cd?(disc?'−'+money(disc):'applied later'):'<span class="bad-text">not a current code</span>'}</dd></div>`:''}<div><dt class="strong">Customer pays now</dt><dd class="strong">${isNaN(sub)?'—':money(sub-disc)}</dd></div>${val('mode')==='delivery'?'<div><dt>GK delivery</dt><dd>quoted separately</dd></div>':''}</dl>`}
f.addEventListener('input',render);f.addEventListener('change',render);render();
f.addEventListener('submit',e=>{e.preventDefault();const p=pick();const errs=[];
 if(!val('name').trim())errs.push('Add the customer’s full name.');if(!f.elements.email.checkValidity()||!val('email').includes('.'))errs.push('Add a full email address for order notices.');
 if(val('otherPayer')&&!val('payer').trim())errs.push('Add the payer’s name.');if(isNaN(p.price)||p.price<=0)errs.push('Enter the agreed price, like 145.00.');
 if(!f.querySelector('[name=slot]:checked'))errs.push('Choose a handover time.');
 const so=f.querySelector('[name=shortOk]');if(so&&(!so.checked||!val('shortWhy').trim()))errs.push('Short notice: tick that it’s your choice and add why.');
 const code=val('code').trim();if(code&&!CODES[code.toUpperCase()])errs.push('That discount code isn’t current. Remove it or check the spelling.');
 if(f.querySelectorAll('[name=agreed]:checked').length<6)errs.push('Tick each detail the customer agreed (price, date, size, flavour, design, handover).');
 const box=f.querySelector('[data-errors]');const out=f.querySelector('[data-link]');
 if(errs.length){box.innerHTML=`<div class="banner banner-bad small"><div><p><strong>Before creating the link:</strong></p><ul>${errs.map(x=>`<li>${x}</li>`).join('')}</ul></div></div>`;out.innerHTML='';return}
 box.innerHTML='';out.innerHTML=`<div class="banner banner-ok" tabindex="-1">${icon('check','banner-icon')}<div><p><strong>Payment link ready (preview).</strong> Order GK-1077 · waiting for payment.</p><p class="small"><code>https://gkcake.com/pay/•••••••• (example)</code></p><p class="small">Works for 24 hours: the customer can start paying until ${when(addH(NOW,24))}. Sending the same link again doesn’t extend it.</p><div class="actions"><button class="btn btn-small" type="button" data-copy>Copy link</button><button class="btn btn-small" type="button" data-copy>Email it from “GK Cakes”</button></div></div></div>`;out.firstElementChild.focus();toast('Preview only: no order or link was created.')});
f.addEventListener('click',e=>{if(e.target.closest('[data-copy]'))toast('Preview only: nothing was copied or sent.')});
})();
