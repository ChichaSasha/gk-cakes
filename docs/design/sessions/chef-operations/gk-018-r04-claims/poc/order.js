// GK-018 r04b-claims — order detail, "Customer account" card: the chef adding an order to a customer's account herself
// (proposal, not approved). The API exists: POST /api/v1/admin/orders/{id}/owner {expected_version (the order's),
// account_id | account_subject, evidence} → 200 {order_id, linked}; 409 DUPLICATE (already in that account),
// 409 ORDER_OWNED (another account), 422 (unknown/disabled/deleted account, bad evidence), 409 STALE_VERSION, 503 restore freeze.
// The customer can't see any account ID today, so this sketch shows two options for the Creator:
//   A (recommended, needs GK-016 wording changes): ask the customer to send a request, then add it from Requests to add orders.
//   B (needs a GK-016 change): the customer copies an "account ID" from their profile; the chef pastes it here.
// Only the "Customer account" card is new; the rest of the order page is the approved r03b2 order page, shortened.
'use strict';
(function(){
const od=orderById(q.get('id')||'e5d2b817')||orderById('e5d2b817');
const OPTS=[['a','A · Ask for a request (recommended)'],['b','B · Account ID (needs GK-016 change)']];
const ANSWERS=[['ok','Saved'],['duplicate','Already in that account'],['order_owned','In another account'],['account_unknown','No such account (422)'],['stale','Order changed'],['paused','Paused (503)']];
const opt=OPTS.some(x=>x[0]===q.get('opt'))?q.get('opt'):'a';
const groups=[['opt','Proposal',OPTS,'a'],...(opt==='b'?[['answer','Simulated answer',ANSWERS,'ok']]:[])];
const S={step:['form','confirm','busy','done'].includes(q.get('step'))?q.get('step'):'form',answer:ANSWERS.some(a=>a[0]===q.get('answer'))?q.get('answer'):'ok',acct:q.get('acct')||'',evidence:q.get('evidence')||''};
const ref=shortId(od.id);
const fromClaim=CLAIMS.find(x=>x.state==='approved'&&!x.account_deleted&&x.order_id===od.id);
const UUID=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const reply=`Hello! To add your order to your GK Cake London account, please sign in on gkcake.com and open Account → Add an older order. Under “How did you order?” choose “Email, phone or in person” (there’s no website choice yet), and as the reference enter the “Order reference” from your order page (it starts ${ref}). Add a short note, and I’ll add it as soon as it arrives.`;

function optionA(){return `<p>To add this order to a customer’s account, the customer sends a request from their account. It then appears in <a href="requests.html">Requests to add orders</a>, where you add it with the same checks.</p>
 <ol class="steps small"><li>Reply to the customer (Instagram, email or phone): ask them to send the order’s reference, which starts <span class="mono strong">${ref}</span>.</li><li>They sign in and send it from <em>Account → Add an older order</em>, choosing “Email, phone or in person”.</li><li>You find the order from the request and add it: they see “Added to your account”.</li></ol>
 <details class="draft"><summary class="btn btn-small">Show a reply you can copy</summary><div class="draft-body"><p class="mail-body" id="reply-text">${esc(reply)}</p><button class="btn btn-small" type="button" data-copy>Copy the reply</button><p class="small muted">Prototype: nothing is copied or sent. You send it yourself where you talk to the customer.</p></div></details>
 <div class="banner banner-warn small">${icon('alert','banner-icon')}<div><p><strong>A works smoothly only after a GK-016 wording change.</strong> Today the customer’s “Add an older order” page is titled for Instagram, email or in-person orders, asks for the “Order reference from the chef (e.g. IG-0613)” and offers only Instagram or “Email, phone or in person”. Its side panel promises “She can add an order for you”. Question 6 (a website choice and wording) is a prerequisite of A; until then the reply above explains the workaround.</p></div></div>`}
function optionB(){
 if(S.step==='done')return result();
 const ready=UUID.test(S.acct.trim())&&S.evidence.trim().length>0;
 const form=`<form class="form-grid tight" data-assign novalidate><label class="field span2">Customer’s account ID<input name="acct" value="${esc(S.acct)}" autocomplete="off" spellcheck="false" inputmode="text" placeholder="e.g. 0f6d1a2b-3c4d-4e5f-8a9b-0c1d2e3f4a5b" aria-describedby="acct-hint acct-err" ${S.acct&&!UUID.test(S.acct.trim())?'aria-invalid="true"':''}></label>
 <p class="small muted span2" id="acct-hint">The customer copies it from <em>Account → Profile → Your account ID</em> (a GK-016 change; not on their page today) and sends it to you. Nothing about the account is shown here before or after you add it.</p><p class="small bad-text span2" id="acct-err" aria-live="polite">${S.acct&&!UUID.test(S.acct.trim())?'That doesn’t look like a full account ID: 36 characters, like the example.':''}</p>
 <label class="field span2">Why you’re adding it (required) · kept as evidence, never shown to the customer<input name="evidence" value="${esc(S.evidence)}" autocomplete="off" aria-describedby="ev-err" placeholder="e.g. Customer asked by Instagram DM on 5 Nov; sent their account ID"></label><p class="small bad-text span2" id="ev-err" role="alert"></p>
 <div class="actions span2"><button class="btn btn-primary" type="submit" ${S.step==='busy'?'disabled aria-busy="true"':''}>${S.step==='busy'?'<span class="spinner light" aria-hidden="true"></span>Adding…':'Add to their account'}</button></div></form>`;
 if(S.step==='confirm'&&ready)return form.replace(/<div class="actions span2">[\s\S]*<\/div><\/form>$/,'</form>')+`<div class="confirm-box" role="group" aria-labelledby="cb-h"><p id="cb-h" class="strong" tabindex="-1" data-focus>Add order <span class="mono">${ref}</span> to account <span class="mono">${esc(S.acct.trim().slice(0,8))}…</span>?</p><ul class="small list-tight"><li>The order appears in that customer’s account.</li><li>${esc(GUEST_GONE)}</li><li>Check the account ID came from the customer themselves: the workspace can’t show whose account it is.</li><li>There’s no undo here: removing an order from an account isn’t built.</li></ul><div class="actions"><button class="btn btn-primary" type="button" data-b="yes">Yes, add to their account</button><button class="btn btn-quiet" type="button" data-b="back">Not yet</button></div></div>`;
 return form}
function result(){const B=(tone,ic,html)=>`<div class="banner banner-${tone} result" role="${tone==='ok'?'status':'alert'}" tabindex="-1" data-focus>${icon(ic,'banner-icon')}<div>${html}</div></div>`;const again='<button class="btn btn-small" type="button" data-b="back">Back to the form</button>';
 return {ok:B('ok','check',`<p><strong>Added ✓</strong> Order <span class="mono">${ref}</span> is now in the customer’s account. Guest access to the order no longer works.</p><p class="small proto">Prototype: nothing is saved.</p>`),
  duplicate:B('info','info',`<p><strong>This order is already in that account.</strong> Nothing changed.</p>`),
  order_owned:B('bad','alert',`<p><strong>This order is already in another customer’s account.</strong> Nothing changed. The workspace doesn’t say which account.</p>${again}`),
  account_unknown:B('bad','alert',`<p><strong>No active account has that ID.</strong> It may be mistyped, or the account is disabled or deleted. Nothing changed. Ask the customer to copy it again.</p>${again}`),
  stale:B('warn','alert',`<p><strong>This order changed while you were looking.</strong> Nothing was saved. Reload the order, then try again.</p><a class="btn btn-small" href="order.html?id=${ref}&opt=b">Reload the order</a>`),
  paused:B('warn','clock',`<p><strong>Adding orders to accounts is paused for a moment</strong> while the data is being restored. Nothing changed. Try again in a few minutes.</p><button class="btn btn-small" type="button" data-b="yes">Try again</button>`)}[S.answer]}

function accountCard(){if(!isChef)return '';
 return `<section class="card" aria-labelledby="acc-h" id="account-card"><h2 id="acc-h" class="card-h">${icon('claims')}Customer account <span class="chip-s chip-warn">proposal · not approved</span></h2>
 ${fromClaim?`<p class="small">Added to a customer’s account from request <a class="mono" href="request.html?id=${shortId(fromClaim.id)}">${shortId(fromClaim.id)}</a>, ${when(fromClaim.decided_at)}.</p>`:''}
 <p class="design-note small">Design note: the staff order view doesn’t yet say whether an order is in a customer account (only the customer’s own view does). ${fromClaim?'This line assumes the proposed chef-only yes/no (question 8).':'This card can’t show it until the API adds it (question 8); adding is refused if it already is.'}</p>
 ${fromClaim?`<p><strong>This order is in a customer’s account.</strong> Nothing to add here.</p>`:`<div data-opt>${opt==='a'?optionA():optionB()}</div>`}</section>`}

function render(focus){mount(shell(`<a class="back" href="../../gk-018-r03/poc/orders.html">${icon('back')} Orders</a>
<header class="page-head"><div><p class="eyebrow">Order · London time</p><h1>Order <span class="mono">${ref}</span></h1></div>${lifeChip(od)}</header>
<div class="detail-grid"><div><section class="card" aria-labelledby="cake-h"><h2 id="cake-h" class="card-h">Cake</h2><div class="cake-row"><img src="assets/${IMG[od.cake]}.png" alt="" width="112" height="112"><div><p class="strong">${esc(CAKES[od.cake][0])}</p><p>${esc(CAKES[od.cake][1])} · ${esc(CAKES[od.cake][2])}</p><p class="small muted">The rest of this page is the approved r03b2 order page (cake, money, emails, progress), not redrawn here.</p></div></div></section>${accountCard()}</div>
<div><section class="card" aria-labelledby="f-h"><h2 id="f-h" class="card-h">Handover</h2>${facts([['Customer (name on the order)',esc(od.customer_full_name)],['When',`${dayShort(od.date)}, ${od.slot}`],['How',MODES[od.mode]],...(isChef?[['Total',money(od.total)]]:[])])}</section></div></div>`,'orders',groups));
 if(focus){const f=document.querySelector('[data-focus]');if(f)f.focus()}}
render(false);
const keep=()=>{const u=new URL(location.href);['step','acct','evidence'].forEach(k=>u.searchParams.delete(k));if(S.step!=='form')u.searchParams.set('step',S.step);if(S.acct)u.searchParams.set('acct',S.acct);if(S.evidence)u.searchParams.set('evidence',S.evidence);history.replaceState(null,'',u.pathname.split('/').pop()+u.search)};
document.addEventListener('submit',e=>{if(!e.target.matches('[data-assign]'))return;e.preventDefault();S.acct=e.target.acct.value.trim();S.evidence=e.target.evidence.value.trim().replace(/\s+/g,' ');
 const acctErr=!S.acct?'Enter the customer’s account ID.':!UUID.test(S.acct)?'That doesn’t look like a full account ID: 36 characters, like the example.':'';
 const evErr=!S.evidence?'Add why you’re adding it.':[...S.evidence].length>500?'Keep it to 500 characters.':'';
 if(acctErr||evErr){render(false);const ai=document.querySelector('[name=acct]'),ei=document.querySelector('[name=evidence]');
  document.getElementById('acct-err').textContent=acctErr;document.getElementById('ev-err').textContent=evErr;
  if(acctErr)ai.setAttribute('aria-invalid','true');else ai.removeAttribute('aria-invalid');if(evErr)ei.setAttribute('aria-invalid','true');
  (acctErr?ai:ei).focus();return}
 S.step='confirm';render(true);keep()});
document.addEventListener('click',e=>{const b=e.target.closest('[data-b],[data-copy]');if(!b)return;
 if(b.hasAttribute('data-copy')){toast('Prototype: nothing is copied or sent.');return}
 if(b.dataset.b==='back'){S.step='form';render(false);keep();const i=document.querySelector('[name=acct]');if(i)i.focus();return}
 if(b.dataset.b==='yes'){S.step='busy';document.querySelector('[data-opt]').innerHTML=`<div class="actions"><button class="btn btn-primary" type="button" disabled aria-busy="true"><span class="spinner light" aria-hidden="true"></span>Adding…</button></div><p class="small" role="status" tabindex="-1" data-focus>Adding the order to the account…</p>`;document.querySelector('[data-focus]').focus();
  setTimeout(()=>{S.step='done';render(true);keep();toast('Prototype: nothing is saved.');if(S.answer==='paused')S.answer='ok'},900)}});
})();
