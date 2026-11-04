// GK-018 r02 — Messages: compose a service message with a fixed 15-minute preview (K08), and the email trail.
'use strict';
(function(){
const states=[['normal','Normal'],['expired','Preview expired'],['recipient','Recipient changed after preview'],['unknown','Send result unknown']];
const st=stateParam(states.map(s=>s[0]));
const oid=order(q.get('order'))?q.get('order'):'GK-1036';
const o=order(oid);
const initSubject=`About your cake on ${dayLabel(o.date)}`,initBody=`Hello ${o.customer.split(' ')[0]},\n\nJust to let you know your ${o.cake} is on track for ${dayLabel(o.date)}, ${o.slot}. If anything changes on your side, reply to this email.\n\nElina\nGK Cakes`;
const all=ORDERS.flatMap(x=>x.emails.map(e=>({o:x,subject:e[0],status:e[1],at:e[2]})));
if(st==='unknown')all.push({o:order('GK-1043'),subject:'Message: “Collection reminder”',status:'unknown',at:'2026-10-22T09:15:00Z',custom:true});
all.sort((a,b)=>new Date(b.at)-new Date(a.at));
const problems=all.filter(m=>['bounced','failed','unknown'].includes(m.status));
const onlyProblems=q.get('show')==='problems';
const list=onlyProblems?problems:all.slice(0,18);
const row=m=>`<li><span><a href="order.html?id=${m.o.id}">${m.o.id}</a> · ${esc(m.subject)}<span class="block small muted">${esc(m.o.customer)} · ${esc(m.o.email)}</span></span><span class="mail-status mail-${EMAIL[m.status][1]}">${EMAIL[m.status][0]}</span><span class="muted small">${when(m.at)}</span>${m.status==='unknown'?`<div class="mail-extra">${can('message')?`<p class="small">We couldn’t confirm whether this was sent. We checked within the email service’s safe-retry window and still don’t know. Resending might send it twice.</p><label class="check small"><input type="checkbox" data-dup> I understand the customer may get it twice</label><button class="btn btn-small" type="button" data-resend disabled>Resend</button>`:'<p class="small role-note">Resending a message with an unknown result: '+ONLY_CHEF+'</p>'}</div>`:''}${m.status==='bounced'?`<div class="mail-extra"><a class="small" href="support.html?state=${m.o.id==='GK-1044'?'flagged':'new'}">Bounced: see the recovery queue</a></div>`:''}</li>`;

const compose=can('message')?`<section class="card" aria-labelledby="c-h" id="compose"><h2 id="c-h" class="card-h">Write a message</h2><form class="compose" data-compose novalidate>
<label class="field">Order<select name="order">${ORDERS.filter(x=>x.stage!=='cancelled'||x.id===oid).map(x=>`<option value="${x.id}" ${x.id===oid?'selected':''}>${x.id} · ${esc(x.customer)} · ${dayShort(x.date)}</option>`).join('')}</select></label>
<dl class="facts small"><div><dt>From</dt><dd>${esc(FROM)}</dd></div><div><dt>Reply-To</dt><dd>${REPLY_TO}</dd></div><div><dt>To</dt><dd>${esc(o.customer)} &lt;${esc(o.email)}&gt;</dd></div></dl>
<p class="small muted">Only messages about this order (no marketing or newsletters). The recipient comes from the order and can’t be typed here.</p>
<label class="field">Subject<input name="subject" maxlength="200" value="${esc(initSubject)}" aria-describedby="subj-n"></label><p id="subj-n" class="small muted counter"></p>
<label class="field">Message<textarea name="body" rows="9" maxlength="10000" aria-describedby="body-n">${esc(initBody)}</textarea></label><p id="body-n" class="small muted counter"></p>
<div class="actions"><button class="btn btn-primary" type="submit">Preview</button></div><p class="small" data-c-msg aria-live="polite"></p></form><div data-preview aria-live="polite"></div></section>`:`<section class="card"><h2 class="card-h">Write a message</h2><p class="role-note">Writing to customers: ${ONLY_CHEF}${REVIEW.role==='support'?' Technical support can see the email trail and handle bounced emails in the recovery queue.':''}</p></section>`;

mount(shell(`<header class="page-head"><div><p class="eyebrow">Messages</p><h1>Messages</h1><p class="muted">Emails go out from “GK Cakes”. “Delivered” means their email server accepted it — not that they’ve read it.</p></div></header>
<div class="detail-grid"><div class="detail-main">${compose}</div><div class="detail-side"><section class="card" aria-labelledby="t-h"><div class="card-row"><h2 id="t-h" class="card-h">Email trail</h2><a class="small" href="messages.html?order=${oid}&show=${onlyProblems?'all':'problems'}${st!=='normal'?'&state='+st:''}">${onlyProblems?'Show all':`Problems only (${problems.length})`}</a></div><ul class="mail-list trail">${list.map(row).join('')||'<li class="muted small">No problems.</li>'}</ul></section></div></div>`,'mail',states));

const main=document.querySelector('#main');
main.addEventListener('change',e=>{if(e.target.matches('[data-dup]'))e.target.parentElement.nextElementSibling.disabled=!e.target.checked;if(e.target.name==='order')location.href=`messages.html?order=${e.target.value}${st!=='normal'?'&state='+st:''}#compose`});
main.addEventListener('click',e=>{const b=e.target.closest('button');if(!b)return;if(b.hasAttribute('data-resend')){b.disabled=true;b.textContent='Resent ✓ (preview)';toast('Preview only: nothing was resent. Live, the resend is linked to the first attempt and marked “possible duplicate”.')}
 if(b.hasAttribute('data-send')){b.disabled=true;b.textContent='Sent ✓ (preview)';toast('Preview only: nothing was sent.')}
 if(b.hasAttribute('data-repreview')){f.requestSubmit()}});
const f=main.querySelector('[data-compose]');if(!f)return;
let previewed=null;
const counts=()=>{say('subj-n',`${f.subject.value.length} / 200 characters`);say('body-n',`${f.body.value.length.toLocaleString('en-GB')} / 10,000 characters`)};
function preview(problem,quiet){const pv=main.querySelector('[data-preview]');const until=problem==='expired'?'2026-10-22T09:40:00Z':problem==='recipient'?'2026-10-22T09:53:00Z':addH(NOW,0.25);previewed={subject:f.subject.value,body:f.body.value,email:o.email};
 const blocked=problem==='expired'?`<div class="banner banner-warn small" role="alert">${icon('clock','banner-icon')}<div><p><strong>This preview expired at ${when(until,{hour:'2-digit',minute:'2-digit',hourCycle:'h23'})}</strong> (previews last 15 minutes). Preview again to send.</p><button class="btn btn-small" type="button" data-repreview>Preview again</button></div></div>`
  :problem==='recipient'?`<div class="banner banner-bad small" role="alert">${icon('alert','banner-icon')}<div><p><strong>The customer’s email address changed after you previewed</strong> (at 10:42 on another device, now <code>${esc(o.email.replace('@','.new@'))}</code>). This preview was for <code>${esc(o.email)}</code>. We won’t send it to a different address without a new preview.</p><button class="btn btn-small" type="button" data-repreview>Preview for the new address</button></div></div>`:'';
 pv.innerHTML=`<div class="mail-preview" tabindex="-1"><p class="small muted">Fixed preview · ${problem==='expired'?'expired':`valid until ${when(until,{hour:'2-digit',minute:'2-digit',hourCycle:'h23'})} (15 minutes)`} · nothing is sent until you press Send.</p>${blocked}<dl class="facts small"><div><dt>From</dt><dd>${esc(FROM)}</dd></div><div><dt>To</dt><dd>${esc(o.email)}</dd></div><div><dt>Subject</dt><dd>${esc(f.subject.value)}</dd></div></dl><div class="mail-body">${esc(f.body.value)}</div><div class="actions"><button class="btn btn-primary" type="button" data-send ${problem?'disabled':''}>Send this message</button></div><p class="small" data-pv-status></p></div>`;
 if(!quiet)pv.firstElementChild.focus()}
f.addEventListener('submit',e=>{e.preventDefault();const msg=f.querySelector('[data-c-msg]');const sub=f.subject.value.trim(),body=f.body.value.trim();
 if(!sub||!body){msg.innerHTML='<span class="bad-text">Add a subject and a message.</span>';return}if(/[\r\n]/.test(f.subject.value)){msg.innerHTML='<span class="bad-text">The subject must be one line.</span>';return}msg.textContent='';
 const problem=previewed?null:(st==='expired'||st==='recipient'?st:null);preview(problem)});
f.addEventListener('input',()=>{counts();if(previewed&&(f.subject.value!==previewed.subject||f.body.value!==previewed.body)){const s=main.querySelector('[data-send]');if(s&&!s.disabled){s.disabled=true;main.querySelector('[data-pv-status]').innerHTML='<span class="warn-text">You changed the message after previewing. Preview again to send it.</span>'}}});
counts();
if(st==='expired'||st==='recipient'){preview(st,true);previewed=null}
})();
