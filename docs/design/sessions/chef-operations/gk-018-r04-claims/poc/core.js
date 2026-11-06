// GK-018 r04-claims — chef side of "add an order to an account" (design proposal, NOT approved).
// Shared example data, helpers and the page shell, reusing GK-018 r03b2's shell and wording.
// Example data only (fake names, fake IDs). Nothing is saved, sent or linked. No network calls.
// API truth: api/README.md "Customer accounts (GK-016 r02b)" and 0033_customer_accounts.sql
// (order_claim_staff_list, order_claim_decide, order_owner_assign).
'use strict';
const PAGE=document.body.dataset.page;
const q=new URLSearchParams(location.search);
const esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const money=n=>new Intl.NumberFormat('en-GB',{style:'currency',currency:'GBP',minimumFractionDigits:n%100?2:0,maximumFractionDigits:2}).format(n/100);
const when=(iso,opts={weekday:'short',day:'numeric',month:'short',hour:'2-digit',minute:'2-digit',hourCycle:'h23'})=>new Intl.DateTimeFormat('en-GB',{timeZone:'Europe/London',...opts}).format(new Date(iso));
const dayShort=d=>new Intl.DateTimeFormat('en-GB',{timeZone:'Europe/London',weekday:'short',day:'numeric',month:'short'}).format(new Date(d+'T12:00:00Z'));
const MONTHS=['January','February','March','April','May','June','July','August','September','October','November','December'];
// "June 2026" from YYYY-MM or YYYY-MM-DD (the customer page's approximateLabel).
const monthLabel=v=>{const m=/^(\d{4})-(\d{2})(?:-\d{2})?$/.exec(v||'');return m?`${MONTHS[+m[2]-1]} ${m[1]}`:v};
const shortId=id=>id.slice(0,8); // live: the start of the UUID (chef order list shortId, customer claimRef)

// Preview clock: Fri 6 Nov 2026, 10:30 London time (GMT).
const NOW='2026-11-06T10:30:00Z';
// Waiting time in London calendar days (received yesterday evening reads "since yesterday", not "today").
const londonDay=iso=>new Intl.DateTimeFormat('en-CA',{timeZone:'Europe/London'}).format(new Date(iso));
const daysWaiting=iso=>Math.round((new Date(londonDay(NOW)+'T12:00:00Z')-new Date(londonDay(iso)+'T12:00:00Z'))/86400000);
const waitText=iso=>{const d=daysWaiting(iso);return d<1?'Waiting since today':d===1?'Waiting since yesterday':`Waiting ${d} days`};

// Review-only role switch (kept for this tab). The claim routes are chef only (fulfilment and technical support get 403).
const ROLES={chef:{label:'Chef',who:'Elina',initial:'E'},fulfillment:{label:'Fulfilment',who:'Kitchen helper',initial:'K'},support:{label:'Technical support',who:'Sasha',initial:'S'}};
const ROLE_KEY=(()=>{let r='chef';try{r=sessionStorage.getItem('gk018r04-role')||'chef'}catch{}const ok=Object.keys(ROLES);if(ok.includes(q.get('role')))r=q.get('role');if(!ok.includes(r))r='chef';try{sessionStorage.setItem('gk018r04-role',r)}catch{}return r})();
const ROLE=ROLES[ROLE_KEY];
const isChef=ROLE_KEY==='chef';

// Example data mode (review only, kept for this tab). "today" (default) = what customers can send now: older-order
// requests (path c, "evidence"), including guest website orders sent through "Add an older order" with the order's
// reference. "future" adds requests from an order page / order email (path a, "email_link"): the API accepts them, but
// the customer UI can't send them yet (GK-016 "Add this order to my account" on the order page; order emails GK-031).
const DATA=(()=>{let d='today';try{d=sessionStorage.getItem('gk018r04-data')||'today'}catch{}if(['today','future'].includes(q.get('data')))d=q.get('data');if(!['today','future'].includes(d))d='today';try{sessionStorage.setItem('gk018r04-data',d)}catch{}return d})();
const FUTURE=DATA==='future';
const FUTURE_NOTE='Arrives once GK-016’s “Add this order to my account” (on the order page) and order emails (GK-031) exist. Customers can’t send this kind of request today.';

// Orders already in the system (what the chef's order list returns: GET /api/v1/admin/orders?from&to, chef view).
// Note: the staff views don't say whether an order is already in a customer account; ORDER_OWNED on approve does.
const CAKES={pink:['Pink piping','Vanilla Raspberry','S · 6 inches'],rasp:['Raspberry ribbons','Chocolate Raspberry','M · 7 inches'],choc:['Chocolate swirl','Snickers','S · 6 inches'],garden:['Garden bloom','White Chocolate Raspberry','M · 7 inches']};
const IMG={pink:'pink',rasp:'raspberry',choc:'chocolate',garden:'garden'};
const MODES={collection:'Collect · St Katharine Docks',customer_courier:'Customer’s courier collects',gk_delivery:'GK delivery'};
const LIFE={completed:['Handed over','ok'],confirmed:['Paid · automatic start later','neutral'],in_production:['In production','info'],cancelled:['Cancelled','bad'],awaiting_payment:['Waiting for payment','info']};
// contact_email: chef-only field of the staff order read (api/README "Staff order reads"); shown only for the order
// the chef chose, to confirm the request from that address (never matched automatically).
const o=(id,name,cake,date,slot,mode,life,total)=>({id,customer_full_name:name,cake,date,slot,mode,life,total,contact_email:name.toLowerCase().replace(/[^a-z]+/g,'.').replace(/\.$/,'')+'@example.com'});
const ORDERS=[
 o('5b71e2c9-4f0a-4d1e-9c3b-1a2b3c4d5e6f','Noah K.','pink','2026-10-22','12:00–13:00','collection','completed',9000),
 o('71d04a5e-2c8b-4a6f-8e1d-6f5e4d3c2b1a','Hannah W.','choc','2026-10-23','15:00–16:00','collection','cancelled',9000),
 o('92e6b3d1-7a4c-4b2e-a1f0-0e9d8c7b6a5f','Oliver T.','pink','2026-10-24','13:00–14:00','collection','completed',9000),
 o('8b2a1f03-6d5c-4e7b-9a8f-3c2d1e0f9a8b','Tess K.','garden','2026-10-16','11:00–12:00','collection','completed',12000),
 o('8b20d4e6-1f2e-4a3b-8c9d-5e6f7a8b9c0d','Tom B.','rasp','2026-10-17','12:30–13:30','customer_courier','completed',10500),
 o('3f9c71a2-8e4d-4c1b-b2a3-9f8e7d6c5b4a','Amelia R.','pink','2026-10-31','13:00–14:00','collection','completed',9000),
 o('e5d2b817-3a9f-4d6c-8b7e-2f1a0b9c8d7e','Grace L.','rasp','2026-11-12','14:00–15:00','collection','confirmed',10500),
 o('a41c09e7-5b6c-4d8e-9f0a-1b2c3d4e5f6a','Daniel O.','choc','2026-11-15','11:00–12:00','collection','confirmed',9000),
 o('c3e8a6f2-9d1b-4f7a-a6c5-4b3a2f1e0d9c','Isla F.','rasp','2026-11-14','12:30–13:30','gk_delivery','in_production',10500),
 o('b6f30e94-2a7d-4c5e-9b1f-7e6d5c4b3a2f','Sam C.','garden','2026-11-21','12:30–13:30','collection','confirmed',12000)];
const orderById=id=>ORDERS.find(x=>x.id===id||shortId(x.id)===id);

// Requests (order_claim_staff_list rows): id, version, state, path, channel, reference, approximate_date, note,
// order_id (decided, or the email-link order), created_at, decided_at, decided_by, decision_note, account {id, full_name, instagram, phone}.
// Proposed API fields (not in the staff rows today): `erased` (the customer deleted their account: free text erased)
// and `account_deleted` (so an approved request can say the order is no longer in any account). See session.md.
const ME='acc-elina';
const acct=(id,full_name,instagram,phone)=>({id,full_name,instagram,phone});
const c=(x)=>Object.assign({version:1,state:'pending',reference:null,approximate_date:null,note:null,order_id:null,decided_at:null,decided_by:null,decision_note:null},x);
const TODAY_CLAIMS=[
 // Waiting (pending), oldest first: older-order requests, the only kind customers can send today.
 c({id:'1d5e88b0-6f7a-4b8c-9d0e-1f2a3b4c5d6e',path:'evidence',channel:'instagram',reference:'IG-0613',approximate_date:'2026-06',created_at:'2026-11-03T09:12:00Z',note:'Birthday cake for my mum in June, collected at St Katharine Docks. I paid by bank transfer after we agreed it in your DMs.',account:acct('acc-2','Priya Shah','priya.s.ldn','07700 900123')}),
 // A guest website order sent through "Add an older order" (the live form offers no website choice, so "Email, phone or in person").
 c({id:'9a3f2c71-0d1e-4f2a-8b3c-4d5e6f7a8b9c',path:'evidence',channel:'offline',reference:'8b20d4e6-1f2e-4a3b-8c9d-5e6f7a8b9c0d',approximate_date:'2026-10',created_at:'2026-11-04T14:05:00Z',note:'I ordered on the website as a guest for 17 October and can’t find a way to add it. The reference is from my order page.',account:acct('acc-3','Tom Becker',null,null)}),
 c({id:'7e2d9b14-1c2d-4e3f-8a4b-5c6d7e8f9a0b',path:'evidence',channel:'offline',reference:'Email 14 Sept',approximate_date:'2026-09',created_at:'2026-11-05T09:10:00Z',note:'Cake ordered by email in September, collected in person.',account:acct('acc-11','Ruby Jones',null,null)}),
 c({id:'5c1e7a02-4d3b-4a9c-8e2f-1b0a9c8d7e6f',path:'evidence',channel:'offline',reference:'a41c09e7',approximate_date:'2026-11',created_at:'2026-11-05T20:30:00Z',note:'Ordered on the website as a guest for Sunday 15 November. The reference is on my order page.',account:acct('acc-5','Daniel Okafor',null,'07700 900456')}),
 c({id:'d93a5c60-2e3f-4a4b-9c5d-6e7f8a9b0c1d',path:'evidence',channel:'instagram',reference:'IG-0702',approximate_date:'2026-07',created_at:'2026-11-06T09:41:00Z',note:'Two small cakes for a christening.',account:acct('acc-12','Ella Grant','ella.grant.ldn',null)}),
 // Added (approved), oldest first as the API returns them.
 c({id:'e41a7b2c-5d6e-4f7a-8b9c-0d1e2f3a4b5c',version:3,state:'approved',path:'evidence',channel:'offline',order_id:'8b2a1f03-6d5c-4e7b-9a8f-3c2d1e0f9a8b',created_at:'2026-10-19T08:00:00Z',decided_at:'2026-10-19T17:30:00Z',decided_by:ME,account:acct('acc-8',null,null,null),erased:true,account_deleted:true}),
 c({id:'2f6a9c03-4b5c-4d6e-9f7a-8b9c0d1e2f3a',version:2,state:'approved',path:'evidence',channel:'offline',reference:'5b71e2c9',approximate_date:'2026-10',order_id:'5b71e2c9-4f0a-4d1e-9c3b-1a2b3c4d5e6f',created_at:'2026-10-27T12:15:00Z',decided_at:'2026-10-29T09:40:00Z',decided_by:ME,decision_note:'Name and date match the order; he confirmed the cake by phone.',note:'Website order as a guest, collected 22 Oct.',account:acct('acc-7','Noah Kowalski',null,'07700 900789')}),
 c({id:'b8e04f17-1a2b-4c3d-8e4f-5a6b7c8d9e0f',version:2,state:'approved',path:'evidence',channel:'offline',reference:'92e6b3d1',approximate_date:'2026-10',order_id:'92e6b3d1-7a4c-4b2e-a1f0-0e9d8c7b6a5f',created_at:'2026-10-28T10:02:00Z',decided_at:'2026-10-28T16:20:00Z',decided_by:ME,note:'Guest order on the website, collected 24 Oct.',account:acct('acc-6','Oliver Thompson','olly.t',null)}),
 // Couldn't add (declined).
 c({id:'4e19b7a2-6f7a-4b8c-9d0e-1f2a3b4c5d6f',version:2,state:'declined',path:'evidence',channel:'instagram',reference:'IG-0544',approximate_date:'2026-05',created_at:'2026-10-29T19:22:00Z',decided_at:'2026-10-30T11:05:00Z',decided_by:ME,decision_note:'Instagram order from May: not in the system yet (older orders come later).',note:'Wedding tasting box in May, paid in cash.',account:acct('acc-9','Sofia Pereira','sofia.p.cakes',null)}),
 // Withdrawn by the customer.
 c({id:'0b7d3e55-8a9b-4c0d-9e1f-2a3b4c5d6e7f',version:2,state:'withdrawn',path:'evidence',channel:'instagram',reference:'IG-0601',created_at:'2026-10-30T15:48:00Z',decided_at:'2026-10-31T09:02:00Z',note:'Cake for 1 June.',account:acct('acc-10','Leo Park','leo.park',null)}),
 // Closed: the customer deleted their account while it was waiting (state "closed", which the list refuses; only a
 // proposed single-request read could show it).
 c({id:'3a7c5e10-6b4d-4f2a-9c8e-7d6f5a4b3c2e',version:2,state:'closed',path:'evidence',channel:'offline',created_at:'2026-11-02T11:30:00Z',account:acct('acc-14',null,null,null),erased:true})];
const FUTURE_CLAIMS=[
 c({id:'c7e1a904-2b3c-4d5e-8f9a-0b1c2d3e4f5a',path:'email_link',channel:'website_email_link',order_id:'3f9c71a2-8e4d-4c1b-b2a3-9f8e7d6c5b4a',created_at:'2026-11-02T18:40:00Z',note:'Collected on Saturday. Please add it to my account so I can order the same cake again.',account:acct('acc-1','Amelia Rose','amelia.rose.ldn',null)}),
 c({id:'f20b6d18-3c4d-4e5f-9a0b-6c7d8e9f0a1b',path:'email_link',channel:'website_email_link',order_id:'b6f30e94-2a7d-4c5e-9b1f-7e6d5c4b3a2f',created_at:'2026-11-05T21:15:00Z',account:acct('acc-4',null,null,null)}),
 c({id:'6c48e1aa-7d8e-4f9a-8b1c-2d3e4f5a6b7c',path:'email_link',channel:'website_email_link',order_id:'b6f30e94-2a7d-4c5e-9b1f-7e6d5c4b3a2f',created_at:'2026-11-06T08:55:00Z',note:'My partner placed this order for me from my laptop.',account:acct('acc-15','Sam Carter',null,'07700 900321')})];
const byReceived=(a,b)=>new Date(a.created_at)-new Date(b.created_at);
// Pages always contain every example request (so links work in either mode); the queue lists future ones only in "future" mode.
const CLAIMS=[...TODAY_CLAIMS,...(FUTURE?FUTURE_CLAIMS:[])].sort(byReceived);
const ALL_CLAIMS=[...TODAY_CLAIMS,...FUTURE_CLAIMS];
// Further waiting requests for the "more than a page" state (page size 50 live; 5 in this preview).
const MORE=[
 c({id:'a05f81c3-3f4a-4b5c-8d6e-7f8a9b0c1d2e',path:'evidence',channel:'offline',reference:'c3e8a6f2',approximate_date:'2026-11',created_at:'2026-11-06T10:12:00Z',note:'Website order as a guest, delivery on 14 November.',account:acct('acc-13','Isla Fraser',null,null)}),
 c({id:'2b9d7f41-5e6f-4a7b-8c9d-0e1f2a3b4c5d',path:'evidence',channel:'instagram',reference:'IG-0811',approximate_date:'2026-08',created_at:'2026-11-06T10:20:00Z',note:'Small chocolate cake in August.',account:acct('acc-16','Max Turner','max.t.ldn',null)}),
 c({id:'8e4a2c19-6f7a-4b8c-9d0e-1f2a3b4c5d6e',path:'evidence',channel:'offline',reference:'Phone, Aug',created_at:'2026-11-06T10:25:00Z',note:'Ordered by phone for my office in August.',account:acct('acc-17','Hugo King',null,'07700 900654')})];
const claimById=id=>[...ALL_CLAIMS,...MORE].find(x=>x.id===id||shortId(x.id)===id);

// Customer-facing words (ui/src/lib/account/claims.ts, older.ts) and the chef's words for the same thing.
// Labelled by path first: a website order sent with a reference (path c) is "On this website", never "email link".
const CHANNEL_CHEF={instagram:'Instagram',offline:'Email, phone or in person',website_email_link:'On this website'};
// The live guest order page shows the full order ID as “Order reference”; customers may paste it whole or in part.
const looksLikeOrderRef=r=>/^[0-9a-f]{8}(-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})?$/i.test(r||'');
const howLabel=cl=>cl.path==='email_link'?'From the order page or email (future)':CHANNEL_CHEF[cl.channel];
const STATE_CHEF={pending:['Waiting','info'],approved:['Added to their account','ok'],declined:['Couldn’t add','neutral'],withdrawn:['Withdrawn by the customer','neutral'],closed:['Closed · account deleted','neutral']};
const CUSTOMER_SEES={pending:'“Waiting for the chef”',approved:'“Added to your account”',declined:'“Couldn’t add”: “The chef couldn’t add an order from the details given, so nothing was added. Please contact us if you think this is a mistake.”',withdrawn:'“Withdrawn”',closed:'Nothing: the account was deleted'};
const whoName=cl=>cl.erased?'Details erased':cl.account.full_name||'No name in their profile';
const orderLine=od=>od?`${CAKES[od.cake][0]} · ${dayShort(od.date)}, ${od.slot.slice(0,5)}`:'';
const decidedBy=cl=>cl.decided_by===ME?'you':'another chef account';
// Plain words about guest access, true today: the guest capability is kept only in the sessionStorage of the tab that
// placed the order (ui/src/lib/guest.ts) and is gone when that tab closes.
const GUEST_GONE='Guest access to this order stops working: today that’s the order page in the tab that placed it, if it’s still open (and, once order emails exist, the links in them). They see the order by signing in.';
const BUSINESS_EMAIL='gk.cake.london@gmail.com'; // the address the live customer pages give
const GUEST_KEPT='Their guest access to the order keeps working as before.';

const ICON={today:'<path d="M3 10h18M7 3v4M17 3v4"/><rect x="3" y="5" width="18" height="16" rx="3"/>',orders:'<path d="M5 4h14v16H5z"/><path d="M9 9h6M9 13h6M9 17h3"/>',plus:'<circle cx="12" cy="12" r="9"/><path d="M12 8v8M8 12h8"/>',calendar:'<rect x="3" y="5" width="18" height="16" rx="3"/><path d="M3 10h18M8 14h2M14 14h2M8 17h2"/>',stock:'<path d="M4 8l8-4 8 4-8 4-8-4Z"/><path d="M4 8v8l8 4 8-4V8"/>',basket:'<path d="M4 9h16l-2 11H6L4 9Z"/><path d="M9 9l3-5 3 5"/>',mail:'<rect x="3" y="5" width="18" height="14" rx="3"/><path d="m4 7 8 6 8-6"/>',support:'<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="3.5"/><path d="m5.6 5.6 3.9 3.9M14.5 14.5l3.9 3.9M18.4 5.6l-3.9 3.9M9.5 14.5l-3.9 3.9"/>',money:'<rect x="3" y="6" width="18" height="12" rx="3"/><circle cx="12" cy="12" r="2.5"/>',claims:'<circle cx="10" cy="8" r="3.5"/><path d="M3.5 20c.6-3.6 3.2-5.5 6.5-5.5 1.3 0 2.5.3 3.5.9"/><path d="M18 14v6M15 17h6"/>',arrow:'<path d="M5 12h14m-5-5 5 5-5 5"/>',back:'<path d="M19 12H5m5-5-5 5 5 5"/>',check:'<path d="m5 12 4 4 10-10"/>',alert:'<path d="M12 4 2 20h20L12 4Z"/><path d="M12 10v4M12 17h.01"/>',clock:'<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',search:'<circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/>',info:'<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7.5h.01"/>',link:'<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>'};
const icon=(n,cl='icon')=>`<svg class="${cl}" viewBox="0 0 24 24" aria-hidden="true">${ICON[n]||ICON.arrow}</svg>`;
const chip=(text,tone)=>`<span class="chip-s chip-${tone}">${esc(text)}</span>`;
const lifeChip=od=>chip(LIFE[od.life][0],LIFE[od.life][1]);
const mount=html=>{document.querySelector('#app').innerHTML=html};
const pendingCount=()=>CLAIMS.filter(x=>x.state==='pending').length;
const futureTag=cl=>cl.path==='email_link'?' <span class="tag-future">Future</span>':'';

// Review bar: role, plus page-specific groups [[param, label, [[value, text]...], default]].
function reviewBar(groups=[]){const link=(k,v,l,def)=>{const u=new URL(location.href);u.searchParams.set(k,v);if(k==='role')u.searchParams.delete('step');const cur=k==='role'?ROLE_KEY===v:k==='data'?DATA===v:(q.get(k)||def)===v;return `<a href="${esc(u.pathname.split('/').pop()+u.search)}" ${cur?'aria-current="true"':''}>${esc(l)}</a>`};
 return `<div class="review-bar" role="region" aria-label="Design preview controls"><span><strong>Proposal GK-018 r04c-claims · not approved</strong> · example data · prototype: nothing is saved</span><span class="review-group"><span class="review-label">Role</span>${link('role','chef','Chef')}${link('role','fulfillment','Fulfilment')}${link('role','support','Technical support')}</span><span class="review-group"><span class="review-label">Example data</span>${link('data','today','Today (what customers can send)')}${link('data','future','+ Future order-page requests')}</span>${groups.map(([k,label,opts,def])=>`<span class="review-group"><span class="review-label">${esc(label)}</span>${opts.map(([v,l])=>link(k,v,l,def)).join('')}</span>`).join('')}<a href="index.html">All states</a></div>`}
function shell(main,active,groups){const nav=[['today','Today','today.html'],['orders','Orders','orders'],['plus','New order',''],['calendar','Calendar & capacity',''],['stock','Ingredients',''],['mail','Messages',''],['support','Needs attention',''],['claims','Account requests','requests.html'],['basket','Groceries',''],['money','Money','']];
 // Sections that this proposal doesn't redraw link to the approved r03b2 prototype.
 const r03={today:'../../gk-018-r03/poc/index.html',plus:'../../gk-018-r03/poc/new-order.html',calendar:'../../gk-018-r03/poc/calendar.html',stock:'../../gk-018-r03/poc/stock.html',mail:'../../gk-018-r03/poc/messages.html',support:'../../gk-018-r03/poc/support.html',orders:'../../gk-018-r03/poc/orders.html'};
 const n=pendingCount();
 const items=nav.filter(([i])=>i!=='claims'||isChef).map(([i,l,h])=>{if(i==='basket'||i==='money')return `<li><span class="nav-later" title="Designed in a later revision">${icon(i)}<span>${l}</span><small>later</small></span></li>`;
  const href=h&&h!=='orders'?h:r03[i];const badge=i==='claims'&&n?`<span class="nav-count" aria-hidden="true">${n}</span><span class="sr-only"> (${n} waiting)</span>`:'';
  return `<li><a href="${href}" ${active===i?'aria-current="page"':''}>${icon(i)}<span>${l}</span>${badge}</a></li>`}).join('');
 return `<a class="skip" href="#main">Skip to content</a>${reviewBar(groups)}<div class="layout"><aside class="sidebar" aria-label="Chef workspace"><a class="brand" href="requests.html" aria-label="GK Cake London chef workspace"><img src="assets/chrome-emblem.png" alt="" width="40" height="40"><span><strong>GK Cake London</strong><small>Chef workspace</small></span></a><nav aria-label="Sections"><ul>${items}</ul></nav><div class="who"><span class="avatar" aria-hidden="true">${ROLE.initial}</span><span><strong>${ROLE.who}</strong><small>${ROLE.label}</small></span></div></aside><main id="main" class="main" tabindex="-1">${main}</main></div><div id="toast" class="toast" role="status" aria-live="polite"></div>`}
// Mobile: the sidebar becomes a horizontal bar; bring the current section into view.
document.addEventListener('DOMContentLoaded',()=>{const a=document.querySelector('.sidebar a[aria-current]');const sb=document.querySelector('.sidebar');if(a&&sb&&sb.scrollWidth>sb.clientWidth)sb.scrollLeft=Math.max(0,a.offsetLeft-sb.clientWidth/2+a.offsetWidth/2)});
function toast(msg){const t=document.querySelector('#toast');if(!t)return;t.textContent='';setTimeout(()=>{t.textContent=msg},30);clearTimeout(toast.t);toast.t=setTimeout(()=>t.textContent='',5200)}
const roleNote=`<div class="card"><p class="role-note">Requests to add orders to customer accounts are Elina’s (chef) only. Fulfilment and technical support don’t see them: the workspace refuses them (403), as the API does.</p></div>`;
const facts=rows=>`<dl class="facts claim-facts">${rows.filter(Boolean).map(([k,v])=>`<div><dt>${k}</dt><dd>${v}</dd></div>`).join('')}</dl>`;
const profileText=cl=>{if(cl.erased)return '<span class="muted">Erased when the customer deleted their account</span>';const a=cl.account;const parts=[a.full_name?esc(a.full_name):'<span class="muted">No name</span>',a.instagram?`Instagram <span class="mono">${esc(a.instagram)}</span>`:'<span class="muted">no Instagram</span>',a.phone?`phone <span class="mono">${esc(a.phone)}</span>`:'<span class="muted">no phone</span>'];return parts.join(' · ')};
