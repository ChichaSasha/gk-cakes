// GK-018 r03 — chef workspace design prototype: shared data, helpers and page shell.
// Example data only. Nothing is saved, sent, refunded or charged. No network calls.
// r02 built on the reviewed r01b (Today + order detail) and added cancel and refund,
// manual orders, calendar and capacity, delivery, stock warnings, messages, the
// recovery queue and the order list ("GK-018 Creator answers / 2026-11-04").
// r03: K04 payment deadlines (start paying until / paid in full by), 192–216 h manual
// orders are ordinary notice, and the payment, order and refund states that are live in
// the backend (api/README.md at origin/main bed0a67: Payments, Full return), with the
// manager cases the worker raises on a "Needs attention" queue.
'use strict';
const PAGE=document.body.dataset.page;
const q=new URLSearchParams(location.search);
const esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const money=n=>new Intl.NumberFormat('en-GB',{style:'currency',currency:'GBP',minimumFractionDigits:n%100?2:0,maximumFractionDigits:2}).format(n/100);
const pence=v=>{const m=String(v??'').trim().replace(/^£/,'');if(!/^\d+(\.\d{1,2})?$/.test(m))return NaN;const [a,b='']=m.split('.');return Number(a)*100+Number((b+'00').slice(0,2))};
const when=(iso,opts={weekday:'short',day:'numeric',month:'short',hour:'2-digit',minute:'2-digit',hourCycle:'h23'})=>new Intl.DateTimeFormat('en-GB',{timeZone:'Europe/London',...opts}).format(new Date(iso));
const dayLabel=d=>new Intl.DateTimeFormat('en-GB',{timeZone:'Europe/London',weekday:'long',day:'numeric',month:'long'}).format(new Date(d+'T12:00:00Z'));
const dayShort=d=>new Intl.DateTimeFormat('en-GB',{timeZone:'Europe/London',weekday:'short',day:'numeric',month:'short'}).format(new Date(d+'T12:00:00Z'));

// Europe/London local time -> UTC instant, without a fixed offset (K00).
const tzOff=ms=>{const p=Object.fromEntries(new Intl.DateTimeFormat('en-GB',{timeZone:'Europe/London',hourCycle:'h23',year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit'}).formatToParts(new Date(ms)).map(x=>[x.type,x.value]));return Date.UTC(+p.year,+p.month-1,+p.day,+p.hour,+p.minute,+p.second)-ms};
const iso=ms=>new Date(ms).toISOString().replace('.000Z','Z');
const london=(date,hm)=>{const [y,m,d]=date.split('-').map(Number),[H,M]=hm.split(':').map(Number);const local=Date.UTC(y,m-1,d,H,M);let t=local-tzOff(local);t=local-tzOff(t);return iso(t)};
const addH=(t,h)=>iso(new Date(t).getTime()+h*3600000);
const hoursBetween=(a,b)=>(new Date(b)-new Date(a))/3600000;
const before=(a,b)=>new Date(a)<new Date(b);
const addDays=(d,n)=>{const t=new Date(d+'T12:00:00Z');t.setUTCDate(t.getUTCDate()+n);return t.toISOString().slice(0,10)};
const weekdayNum=d=>new Date(d+'T12:00:00Z').getUTCDay();
const plusHour=hm=>{const [h,m]=hm.split(':').map(Number);return String(h+1).padStart(2,'0')+':'+String(m).padStart(2,'0')};
const halfUp=(amount,pct)=>Math.floor((amount*pct+50)/100); // nearest penny, halves up (GK-003 refund_rounding)
// Floor shares then largest remainder; ties by order (K05). Each part never exceeds what it paid.
function allocate(total,paids){const sum=paids.reduce((a,b)=>a+b,0);if(!sum)return paids.map(()=>0);const fl=paids.map(p=>Math.floor(total*p/sum));let rem=total-fl.reduce((a,b)=>a+b,0);
 const order=paids.map((p,i)=>[(total*p)%sum,i]).sort((a,b)=>b[0]-a[0]||a[1]-b[1]);for(const [,i] of order){if(rem<=0)break;if(fl[i]<paids[i]){fl[i]++;rem--}}return fl}

// Review-only options: the role is kept for this tab; states come from the link.
const ROLES={chef:{label:'Chef',who:'Elina',initial:'E',caps:['capacity','schedule','lots','neworder','cancel','refund','move','message','decide','quote','email','details','batches','stock','start','ready','dispatch','handover']},
 fulfillment:{label:'Fulfilment',who:'Kitchen helper',initial:'K',caps:['start','ready','dispatch','handover']},
 support:{label:'Technical support',who:'Sasha',initial:'S',caps:['flag','resend-fixed','recheck']}};
const REVIEW=(()=>{let r={};try{r=JSON.parse(sessionStorage.getItem('gk018r03-review')||'{}')||{}}catch{}const ok=['chef','fulfillment','support'];if(ok.includes(q.get('role')))r.role=q.get('role');if(!ok.includes(r.role))r.role='chef';try{sessionStorage.setItem('gk018r03-review',JSON.stringify(r))}catch{}return r})();
const ROLE=ROLES[REVIEW.role];
const can=cap=>ROLE.caps.includes(cap);
// K00 minimal access: fulfilment helpers see no money (prices, payments, refunds).
const seesMoney=REVIEW.role!=='fulfillment';
const mm=n=>seesMoney?money(n):'';
// Proposed set point (open question): a short-notice payment must be complete this many hours before handover.
const PAY_BEFORE_H=2;
const localInput=t=>{const p=Object.fromEntries(new Intl.DateTimeFormat('en-GB',{timeZone:'Europe/London',year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hourCycle:'h23'}).formatToParts(new Date(t)).map(x=>[x.type,x.value]));return `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}`};
const fromLocalInput=v=>/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(v||'')?london(v.slice(0,10),v.slice(11,16)):null;
const ONLY_CHEF='Only Elina (chef) can do this.';
const stateParam=(allowed)=>allowed.includes(q.get('state'))?q.get('state'):allowed[0];

const TODAY='2026-10-22';
const NOW='2026-10-22T09:45:00Z'; // preview clock: Thu 22 Oct, 10:45 London time (BST until Sun 25 Oct)
const LEAD_H=192;
const ALLERGENS='EGGS, MILK, CEREALS CONTAINING GLUTEN (WHEAT), SOYBEANS';
const FROM='GK Cakes <orders@gkcake.com>', REPLY_TO='gk.cake.london@gmail.com';

// Schedule version (immutable, new bookings only). Starts are the chef's explicit list.
const SCHEDULE={version:3,revision:3,publishedAt:'2026-10-01T08:00:00Z',standardLimit:7,bookableUntil:'2026-12-20',
 weekdays:{4:{name:'Thursday',opens:'11:00',closes:'17:00',starts:['11:00','12:00','13:00','14:00','15:00','16:00']},
  5:{name:'Friday',opens:'10:00',closes:'17:00',starts:['10:00','11:00','12:00','13:00','14:00','15:00','16:00']},
  6:{name:'Saturday',opens:'12:30',closes:'15:00',starts:['12:30','13:00','13:30','14:00']},
  0:{name:'Sunday',opens:'11:00',closes:'12:00',starts:['11:00']}},
 closed:['2026-11-08']}; // Sun 8 Nov is example data: GK-001 schedule_admin has no closed dates currently.
// Day limit overrides (soft capacity). Thu 29 Oct: lowered 7 -> 5 with 7 accepted; all 7 stay.
const DAY_OVERRIDES={'2026-10-29':{limit:5,version:2,changedAt:'2026-10-21T18:05:00Z'}};
// Tentative holds: a customer paying now (15-minute hold, counts until reconciled).
const TENTATIVE={}; // r03: customers paying now are real example orders (GK-1078 on Fri 30 Oct).

const CAKES={pink:{img:'pink',cake:'Pink piping',flavour:'Vanilla Raspberry',size:'S · 6 inches · 7–10 servings',price:9000,recipe:'VR-6 v1'},
 rasp:{img:'raspberry',cake:'Raspberry ribbons',flavour:'Chocolate Raspberry',size:'M · 7 inches · 12–14 servings',price:10500,recipe:'CR-7 v1'},
 choc:{img:'chocolate',cake:'Chocolate swirl',flavour:'Snickers',size:'S · 6 inches · 7–10 servings',price:9000,peanuts:true,recipe:'SN-6 v1'},
 garden:{img:'garden',cake:'Garden bloom',flavour:'White Chocolate Raspberry',size:'M · 7 inches · 12–14 servings',price:12000,recipe:'WCR-7 v1'}};
const MODES={collection:'Collect · St Katharine Docks',courier:'Customer’s courier collects',delivery:'GK delivery'};
const SOURCES={online:'Online',instagram:'Instagram · entered by Elina',email:'Email · entered by Elina'};

// r01b orders (Today) keep their exact values; deadlineAt is saved at payment as
// the original handover minus 120 elapsed hours and never moves.
const ORDERS=[
 {id:'GK-1036',img:'pink',cake:'Pink piping',flavour:'Vanilla Raspberry',size:'S · 6 inches · 7–10 servings',recipe:'VR-6 v1',cakes:1,price:9000,discount:900,code:'DEMO10',customer:'Amelia R.',email:'amelia.r@example.com',date:TODAY,slot:'11:00–12:00',mode:'collection',stage:'ready',paidAt:'2026-10-12T17:04:00Z',autoAt:'2026-10-14T17:04:00Z',startedAt:'2026-10-20T08:10:00Z',readyAt:'2026-10-22T09:42:00Z',deadlineAt:'2026-10-17T10:00:00Z',version:'2026-10-22T09:42:00Z',
  emails:[['Order confirmation','delivered','2026-10-12T17:05:00Z'],['Collection details','delivered','2026-10-20T09:00:00Z']]},
 {id:'GK-1040',img:'raspberry',cake:'Raspberry ribbons',flavour:'Chocolate Raspberry',size:'M · 7 inches · 12–14 servings',recipe:'CR-7 v1',cakes:1,price:10500,customer:'Noah K.',email:'noah.k@example.com',date:TODAY,slot:'12:00–13:00',mode:'courier',stage:'making',paidAt:'2026-10-11T10:20:00Z',autoAt:'2026-10-13T10:20:00Z',startedAt:'2026-10-21T07:30:00Z',deadlineAt:'2026-10-17T11:00:00Z',version:'2026-10-21T07:30:00Z',
  emails:[['Order confirmation','delivered','2026-10-11T10:21:00Z'],['Collection details','delivered','2026-10-20T09:00:00Z']]},
 {id:'GK-1038',img:'garden',cake:'Custom cake · Garden bloom style',flavour:'White Chocolate Raspberry',size:'Agreed design · 17–20 servings',recipe:'WCR-8 custom v1',cakes:1,price:14500,custom:true,source:'instagram',customer:'Priya S.',email:'priya.s@example.com',date:TODAY,slot:'14:00–15:00',mode:'delivery',address:'Flat 4, 18 Example Mews, London SE1 2XX',delivery:{state:'paid',quote:11000,distance:'6.2 miles (Google Maps, measured by Elina)',sentAt:'2026-10-10T11:30:00Z',paidAt:'2026-10-10T12:05:00Z'},stage:'preparing',paidAt:'2026-10-09T15:00:00Z',autoAt:'2026-10-11T15:00:00Z',deadlineAt:'2026-10-17T13:00:00Z',version:'2026-10-10T12:05:00Z',
  emails:[['Order confirmation','delivered','2026-10-09T15:01:00Z'],['Delivery quote','delivered','2026-10-10T11:30:00Z'],['Delivery payment received','delivered','2026-10-10T12:05:00Z'],['Delivery details','delivered','2026-10-20T09:00:00Z']]},
 {id:'GK-1041',img:'chocolate',cake:'Chocolate swirl',flavour:'Snickers',size:'S · 6 inches · 7–10 servings',recipe:'SN-6 v1',cakes:1,price:9000,peanuts:true,customer:'Grace L.',email:'grace.l@example.com',date:TODAY,slot:'15:00–16:00',mode:'collection',stage:'preparing',paidAt:'2026-10-13T19:40:00Z',autoAt:'2026-10-15T19:40:00Z',deadlineAt:'2026-10-17T14:00:00Z',version:'2026-10-20T09:00:00Z',
  emails:[['Order confirmation','delivered','2026-10-13T19:41:00Z'],['Collection details','delivered','2026-10-20T09:00:00Z']]},
 {id:'GK-1043',img:'pink',cake:'Pink piping',flavour:'Vanilla Raspberry',size:'S · 6 inches · 7–10 servings',recipe:'VR-6 v1',cakes:1,price:9000,customer:'Oliver T.',email:'oliver.t@example.com',date:TODAY,slot:'16:00–17:00',mode:'collection',stage:'preparing',paidAt:'2026-10-14T08:12:00Z',autoAt:'2026-10-16T08:12:00Z',deadlineAt:'2026-10-17T15:00:00Z',version:'2026-10-20T09:00:00Z',
  emails:[['Order confirmation','delivered','2026-10-14T08:13:00Z'],['Collection details','delivered','2026-10-20T09:00:00Z']]},
 {id:'GK-1045',img:'raspberry',cake:'Raspberry ribbons',flavour:'Chocolate Raspberry',size:'M · 7 inches · 12–14 servings',recipe:'CR-7 v1',cakes:1,price:10500,extra:true,source:'instagram',customer:'Hannah W.',email:'hannah.w@example.com',date:TODAY,slot:'16:00–17:00',mode:'courier',stage:'preparing',paidAt:'2026-10-13T12:00:00Z',autoAt:'2026-10-15T12:00:00Z',deadlineAt:'2026-10-17T15:00:00Z',version:'2026-10-20T09:00:00Z',
  emails:[['Order confirmation','delivered','2026-10-13T12:01:00Z'],['Collection details','delivered','2026-10-20T09:00:00Z']]},
 {id:'GK-1031',img:'chocolate',cake:'Chocolate swirl',flavour:'Snickers',size:'S · 6 inches · 7–10 servings',recipe:'SN-6 v1',cakes:1,price:9000,peanuts:true,customer:'Ethan M.',email:'ethan.m@example.com',date:'2026-10-24',slot:'12:30–13:30',mode:'collection',stage:'cancelled',paidAt:'2026-10-10T09:00:00Z',autoAt:'2026-10-12T09:00:00Z',deadlineAt:'2026-10-19T11:30:00Z',version:'2026-10-21T14:02:00Z',
  cancel:{by:'customer',at:'2026-10-21T13:58:00Z',rule:50,goodwill:0,reason:'The customer asked to cancel (Instagram message)',confirmed:{channel:'Instagram message',at:'2026-10-21T13:50:00Z',evidence:'“Yes please cancel, £45 back is fine” (DM 14:50)'}},
  collections:[{kind:'cake',paid:9000,refunds:[{amount:4500,status:'unknown',at:'2026-10-21T14:02:00Z'}]}],
  emails:[['Order confirmation','delivered','2026-10-10T09:01:00Z'],['Cancellation and refund started','delivered','2026-10-21T14:03:00Z']]},
 {id:'GK-1044',img:'raspberry',cake:'Raspberry ribbons',flavour:'Chocolate Raspberry',size:'M · 7 inches · 12–14 servings',recipe:'CR-7 v1',cakes:1,price:10500,customer:'Mia D.',email:'mia.d@exmaple.com',date:'2026-10-25',slot:'11:00–12:00',mode:'collection',stage:'preparing',paidAt:'2026-10-16T20:15:00Z',autoAt:'2026-10-18T20:15:00Z',deadlineAt:'2026-10-20T11:00:00Z',version:'2026-10-16T20:16:00Z',
  emails:[['Order confirmation','bounced','2026-10-16T20:16:00Z']]},
 // A customer-requested late cancellation on a delivery order: cake refund confirmed, delivery refund still owed.
 {id:'GK-1029',img:'garden',cake:'Garden bloom',flavour:'White Chocolate Raspberry',size:'M · 7 inches · 12–14 servings',recipe:'WCR-7 v1',cakes:1,price:12000,customer:'Sofia P.',email:'sofia.p@example.com',date:'2026-10-24',slot:'13:00–14:00',mode:'delivery',address:'22 Example Road, London E14 3XX',delivery:{state:'paid',quote:11000,distance:'4.8 miles (Google Maps, measured by Elina)',sentAt:'2026-10-14T16:00:00Z',paidAt:'2026-10-15T10:00:00Z'},stage:'cancelled',paidAt:'2026-10-13T18:30:00Z',autoAt:'2026-10-15T18:30:00Z',deadlineAt:'2026-10-19T12:00:00Z',version:'2026-10-21T09:02:00Z',
  cancel:{by:'customer',at:'2026-10-20T16:10:00Z',rule:50,goodwill:0,reason:'The customer asked to cancel (email)',confirmed:{channel:'Email',at:'2026-10-20T16:05:00Z',evidence:'Reply “Agreed: £115 back in total” (email 17:05)'}},
  collections:[{kind:'cake',paid:12000,refunds:[{amount:6000,status:'succeeded',at:'2026-10-20T16:12:00Z',confirmedAt:'2026-10-21T09:02:00Z'}]},{kind:'delivery',paid:11000,refunds:[]}],
  emails:[['Order confirmation','delivered','2026-10-13T18:31:00Z'],['Delivery quote','delivered','2026-10-14T16:00:00Z'],['Delivery payment received','delivered','2026-10-15T10:01:00Z'],['Cancellation and refund started','delivered','2026-10-20T16:13:00Z'],['Cake refund confirmed','delivered','2026-10-21T09:03:00Z']]},
];
// Further example orders, so every day in the calendar and order list adds up.
const NAMES={};
function stub(id,date,start,key,customer,mode,paidAt,extra={}){const c=CAKES[key];const startAt=london(date,start);const o={id,img:c.img,cake:c.cake,flavour:c.flavour,size:c.size,recipe:c.recipe,cakes:1,price:c.price,peanuts:!!c.peanuts,customer,email:customer.toLowerCase().replace(/[^a-z]+/g,'.').replace(/\.$/,'')+'@example.com',date,slot:start+'–'+plusHour(start),mode,paidAt,autoAt:addH(paidAt,48),deadlineAt:addH(startAt,-120),version:addH(paidAt,1/60),...extra};
 if(!o.stage)o.stage=before(o.autoAt,NOW)?'preparing':'confirmed';
 if(!o.emails){const label=mode==='delivery'?'Delivery details':'Collection details';o.emails=[['Order confirmation','delivered',addH(paidAt,1/60)]];const due=addH(startAt,-48);if(before(due,NOW)&&!o.noDetails)o.emails.push([label,'delivered',due])}
 return o}
ORDERS.push(
 stub('GK-1048','2026-10-23','15:00','pink','Isla F.','collection','2026-10-12T14:00:00Z',{noDetails:true}),
 stub('GK-1049','2026-10-23','11:00','rasp','Leo P.','courier','2026-10-11T09:30:00Z',{stage:'making',startedAt:'2026-10-22T08:30:00Z',startedBy:'Kitchen helper',batchesPending:true}),
 stub('GK-1050','2026-10-23','13:00','choc','Jack H.','collection','2026-10-13T20:10:00Z',{outsideRefund:{amount:2000,at:'2026-10-21T16:40:00Z'},attention:'quarantine'}),
 stub('GK-1046','2026-10-24','12:30','rasp','Freya N.','collection','2026-10-14T11:00:00Z'),
 stub('GK-1051','2026-10-24','13:30','garden','Theo C.','courier','2026-10-12T16:45:00Z'),
 // Instagram order under 192 hours: the chef's deliberate choice (Creator answer 1).
 stub('GK-1053','2026-10-24','14:00','pink','Ruby J.','collection','2026-10-20T18:00:00Z',{source:'instagram',paymentStartedAt:'2026-10-20T17:10:00Z',shortNotice:{approvedAt:'2026-10-19T17:30:00Z',startBy:'2026-10-20T17:30:00Z',completeBy:'2026-10-23T13:00:00Z',reason:'Regular customer, simple design; I have time on Friday.'}}),
 stub('GK-1054','2026-10-29','11:00','pink','Oscar V.','collection','2026-10-08T10:00:00Z'),
 stub('GK-1055','2026-10-29','11:00','rasp','Evie G.','courier','2026-10-09T12:30:00Z'),
 stub('GK-1056','2026-10-29','12:00','choc','Arthur Z.','collection','2026-10-10T19:00:00Z'),
 stub('GK-1057','2026-10-29','13:00','pink','Poppy E.','collection','2026-10-12T08:45:00Z'),
 stub('GK-1058','2026-10-29','14:00','garden','Harry Q.','collection','2026-10-14T17:20:00Z'),
 stub('GK-1059','2026-10-29','15:00','rasp','Sophia Y.','courier','2026-10-16T09:10:00Z'),
 stub('GK-1060','2026-10-29','16:00','pink','George I.','collection','2026-10-18T21:00:00Z'),
 // Payment started in time but finished too late for the 192-hour promise: waiting for Elina (K04 Q2).
 stub('GK-1039','2026-10-29','12:00','pink','Lily O.','collection','2026-10-22T08:20:00Z',{stage:'agreement',payment:{state:'succeeded',amount:9000},paymentStartedAt:'2026-10-21T10:30:00Z',linkSentAt:'2026-10-20T15:00:00Z',emails:[['Payment received · we’ll confirm your date','delivered','2026-10-22T08:21:00Z']]}),
 stub('GK-1061','2026-10-30','10:00','choc','Alfie U.','collection','2026-10-12T13:00:00Z'),
 stub('GK-1062','2026-10-30','11:00','pink','Ella X.','collection','2026-10-15T18:40:00Z'),
 stub('GK-1063','2026-10-30','13:00','garden','Henry A.','courier','2026-10-18T11:15:00Z'),
 stub('GK-1064','2026-10-30','15:00','rasp','Daisy K.','collection','2026-10-20T20:30:00Z'),
 stub('GK-1047','2026-10-31','13:00','rasp','Zara M.','delivery','2026-10-20T19:00:00Z',{address:'12 Example Wharf, London SE16 7XX',delivery:{state:'requested',requestedAt:'2026-10-21T08:15:00Z',window:'13:00–14:00 (requested, subject to your confirmation)'}}),
 stub('GK-1065','2026-10-31','12:30','pink','Max T.','collection','2026-10-11T10:00:00Z'),
 stub('GK-1066','2026-10-31','12:30','choc','Nina B.','collection','2026-10-12T15:30:00Z'),
 stub('GK-1067','2026-10-31','13:30','rasp','Omar S.','courier','2026-10-13T09:00:00Z'),
 stub('GK-1069','2026-10-31','14:00','pink','Ivy W.','collection','2026-10-16T19:45:00Z'),
 stub('GK-1070','2026-10-31','14:00','garden','Felix R.','collection','2026-10-19T12:00:00Z'),
 stub('GK-1042','2026-10-31','12:30','choc','Rosa L.','collection','2026-10-22T09:05:00Z',{stage:'awaiting',paidAt:null,payment:{state:'unknown',amount:9000,startedAt:'2026-10-22T09:05:00Z',checkedAt:'2026-10-22T09:30:00Z'},emails:[]}),
 stub('GK-1071','2026-11-01','11:00','rasp','Adam C.','collection','2026-10-17T16:00:00Z'),
 stub('GK-1072','2026-11-05','12:00','pink','Beth H.','collection','2026-10-19T10:30:00Z'),
 stub('GK-1073','2026-11-05','14:00','choc','Carl D.','courier','2026-10-20T13:00:00Z'),
 stub('GK-1074','2026-11-06','11:00','garden','Dina F.','collection','2026-10-21T17:40:00Z',{extra:true,source:'email',custom:true,cake:'Custom cake · wedding tasting tier',price:18000}),
 stub('GK-1075','2026-11-07','13:00','pink','Eric G.','collection','2026-10-21T20:05:00Z'),
 stub('GK-1076','2026-11-13','12:00','rasp','Gwen J.','collection','2026-10-22T07:50:00Z'),
 stub('GK-1068','2026-11-15','11:00','pink','Hugo K.','collection','2026-10-20T11:25:00Z'),
 // r03: states that are live in the backend (api/README.md, Payments and Full return).
 // Ordinary chef link, 194 hours before handover when sent: start paying until 192 h before (sooner than 24 h).
 stub('GK-1078','2026-10-30','14:00','rasp','Kai B.','collection','2026-10-22T09:20:00Z',{stage:'awaiting',paidAt:null,source:'instagram',link:{sentAt:'2026-10-21T17:00:00Z',startBy:'2026-10-22T14:00:00Z',completeBy:'2026-10-22T14:00:00Z'},payment:{state:'pending',amount:10500,startedAt:'2026-10-22T09:20:00Z'},emails:[]}),
 // Payment succeeded a moment ago; the order is being confirmed (confirmation_pending).
 stub('GK-1079','2026-11-01','11:00','pink','Lara N.','collection','2026-10-22T09:43:00Z',{stage:'confirming',payment:{state:'succeeded',amount:9000},emails:[]}),
 // Released by the hold review: nobody completed the payment / the free order wasn't confirmed. Nothing was taken.
 stub('GK-1080','2026-11-05','12:00','garden','Mason E.','collection','2026-10-21T19:40:00Z',{stage:'cancelled',paidAt:null,cancelReason:'payment_not_completed',cancelledAt:'2026-10-21T20:12:00Z',source:'email',payment:{state:'cancelled',amount:12000,startedAt:'2026-10-21T19:52:00Z'},emails:[]}),
 stub('GK-1081','2026-11-06','13:00','pink','Tess O.','collection','2026-10-21T11:00:00Z',{stage:'cancelled',paidAt:null,price:0,free:true,cake:'Pink piping · giveaway prize',cancelReason:'free_not_confirmed',cancelledAt:'2026-10-21T11:16:00Z',emails:[]}),
 // Payment arrived after the order had been released: the paid confirmation is refused, the order stays
 // cancelled and the full return is started automatically (late_capture_terminal).
 stub('GK-1082','2026-11-05','14:00','rasp','Ava Q.','courier','2026-10-21T08:02:00Z',{stage:'cancelled',cancelReason:'payment_not_completed',cancelledAt:'2026-10-20T19:31:00Z',payment:{state:'succeeded',amount:10500,startedAt:'2026-10-20T19:14:00Z'},attention:'refused',
  cancel:{by:'system',at:'2026-10-20T19:31:00Z',goal:{target:10500,requestedAt:'2026-10-21T08:03:00Z'},reason:'No cake: the order had already been released'},collections:[{kind:'cake',paid:10500,refunds:[{amount:10500,status:'pending',at:'2026-10-21T08:04:00Z'}]}],emails:[]}),
 // Payment evidence that doesn't match the order: kept unknown and held, never confirmed (payment mismatch).
 stub('GK-1083','2026-11-06','14:00','rasp','Cleo W.','collection','2026-10-21T15:30:00Z',{stage:'awaiting',paidAt:null,payment:{state:'unknown',amount:10500,startedAt:'2026-10-21T15:30:00Z',checkedAt:'2026-10-22T09:30:00Z',mismatch:'£95.00 received for this £105 order.'},attention:'mismatch',emails:[]}),
 // A refund the provider confirmed and later reported as failed (refund contradiction).
 stub('GK-1084','2026-10-31','14:00','pink','Iris P.','collection','2026-10-12T10:00:00Z',{stage:'cancelled',attention:'contradiction',
  cancel:{by:'chef',at:'2026-10-19T09:00:00Z',rule:100,goodwill:0,reason:'You cancelled: oven repair that weekend'},collections:[{kind:'cake',paid:9000,refunds:[{amount:9000,status:'succeeded',at:'2026-10-19T09:01:00Z',confirmedAt:'2026-10-19T09:20:00Z',changedAt:'2026-10-21T15:05:00Z',contradiction:'Confirmed Mon 19 Oct, 10:20; the provider reported it as failed Wed 21 Oct, 16:05.'}]}],
  emails:[['Order confirmation','delivered','2026-10-12T10:01:00Z'],['Cancellation and refund started','delivered','2026-10-19T09:02:00Z']]}),
);
const order=id=>ORDERS.find(o=>o.id===id);
const startOf=o=>london(o.date,o.slot.slice(0,5));
const paidFor=o=>o.price-(o.discount||0);
const deliveryPaid=o=>o.mode==='delivery'&&o.delivery&&['paid','dispatched','impossible'].includes(o.delivery.state)?o.delivery.quote:0;
const detailsLabel=o=>o.mode==='delivery'?'Delivery details':'Collection details';
// Collection details go out about 48 hours before (GK-001 address_release).
const detailsDue=o=>addH(startOf(o),-48);
const detailsSent=o=>o.emails.some(e=>e[0]===detailsLabel(o)&&e[1]!=='bounced'&&e[1]!=='failed');
const detailsOverdue=o=>['confirmed','preparing','making','ready'].includes(o.stage)&&!detailsSent(o)&&before(detailsDue(o),NOW)&&before(NOW,startOf(o));

const COMMITTED=new Set(['confirmed','preparing','making','ready','out','done']);
// Holds kept while a payment may still succeed or is being confirmed (hold review, api/README.md step 7).
const HELD=o=>o.stage==='confirming'||o.stage==='awaiting'&&o.payment&&['created','pending','unknown'].includes(o.payment.state);
function dayStats(date){const hours=SCHEDULE.weekdays[weekdayNum(date)];const ov=DAY_OVERRIDES[date];const limit=ov?ov.limit:SCHEDULE.standardLimit;
 const list=ORDERS.filter(o=>o.date===date);const sum=a=>a.reduce((n,o)=>n+o.cakes,0);
 const committed=sum(list.filter(o=>COMMITTED.has(o.stage)&&!o.extra)),extras=sum(list.filter(o=>COMMITTED.has(o.stage)&&o.extra));
 const tentative=(TENTATIVE[date]||0)+sum(list.filter(HELD));
 const awaitingDecision=list.filter(o=>o.stage==='agreement');
 let status;
 if(date<TODAY)status='past';else if(!hours)status='no_times';else if(SCHEDULE.closed.includes(date))status='closed';else if(date>SCHEDULE.bookableUntil)status='outside_horizon';
 else{const anyOpen=hours.starts.some(s=>hoursBetween(NOW,london(date,s))>=LEAD_H);status=!anyOpen?'lead_time':(committed+tentative>=limit?'full':'open')}
 return {date,hours,limit,override:ov,committed,extras,tentative,awaitingDecision,workload:committed+extras,remaining:Math.max(0,limit-committed-tentative),unused:Math.max(0,limit-committed),status,list}}
const DAY_STATUS={open:['Open online','ok'],full:['Full online','warn'],lead_time:['closed online','info'],closed:['Closed date','bad'],outside_horizon:['Beyond bookable-until','neutral'],no_times:['Not a working day','neutral'],past:['Past','neutral']};

const STAGES={awaiting:'Waiting for payment',confirming:'Paid · confirming the order',agreement:'Paid late · your decision',confirmed:'Paid · automatic start later',preparing:'In production (automatic) · not started by you',making:'You started making',ready:'Ready for handover',out:'Out for delivery',done:'Handed over',cancelled:'Cancelled'};
const EMAIL={queued:['Waiting to send','neutral'],sending:['Sending','neutral'],accepted:['Accepted by our email service','neutral'],delivered:['Delivered to their email server','ok'],bounced:['Bounced · not delivered','bad'],failed:['Failed to send','bad'],unknown:['Result unknown','warn'],due:['Not sent yet','neutral'],overdue:['Not sent · due about 48 h before','bad']};
const ICON={today:'<path d="M3 10h18M7 3v4M17 3v4"/><rect x="3" y="5" width="18" height="16" rx="3"/>',orders:'<path d="M5 4h14v16H5z"/><path d="M9 9h6M9 13h6M9 17h3"/>',plus:'<circle cx="12" cy="12" r="9"/><path d="M12 8v8M8 12h8"/>',calendar:'<rect x="3" y="5" width="18" height="16" rx="3"/><path d="M3 10h18M8 14h2M14 14h2M8 17h2"/>',stock:'<path d="M4 8l8-4 8 4-8 4-8-4Z"/><path d="M4 8v8l8 4 8-4V8"/>',basket:'<path d="M4 9h16l-2 11H6L4 9Z"/><path d="M9 9l3-5 3 5"/>',mail:'<rect x="3" y="5" width="18" height="14" rx="3"/><path d="m4 7 8 6 8-6"/>',support:'<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="3.5"/><path d="m5.6 5.6 3.9 3.9M14.5 14.5l3.9 3.9M18.4 5.6l-3.9 3.9M9.5 14.5l-3.9 3.9"/>',money:'<rect x="3" y="6" width="18" height="12" rx="3"/><circle cx="12" cy="12" r="2.5"/>',arrow:'<path d="M5 12h14m-5-5 5 5-5 5"/>',back:'<path d="M19 12H5m5-5-5 5 5 5"/>',check:'<path d="m5 12 4 4 10-10"/>',alert:'<path d="M12 4 2 20h20L12 4Z"/><path d="M12 10v4M12 17h.01"/>',clock:'<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',search:'<circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/>',truck:'<path d="M3 6h11v10H3zM14 10h4l3 3v3h-7z"/><circle cx="7" cy="17" r="2"/><circle cx="17" cy="17" r="2"/>'};
const icon=(n,c='icon')=>`<svg class="${c}" viewBox="0 0 24 24" aria-hidden="true">${ICON[n]||ICON.arrow}</svg>`;
const ACTIONS={preparing:['Start making','start'],making:['Mark ready','ready'],ready:['Hand over','handover']};
// Live backend vocabulary (api/README.md at origin/main bed0a67). Chef-facing words; the backend name is kept for Sasha.
const PAY_STATE={created:['Payment page being prepared','neutral'],pending:['Payment in progress','info'],unknown:['Payment result unknown','warn'],succeeded:['Paid','ok'],failed:['Payment failed · may try again','neutral'],cancelled:['Payment not completed','neutral']};
const CANCEL_REASON={payment_not_completed:'Not paid · released',free_not_confirmed:'Free order not confirmed · released'};
const REFUND_STATE={requested:['Refund owed · nothing sent yet','info'],pending:['Refund in progress · not confirmed','info'],partly_returned:['Partly refunded','warn'],returned:['Refunded','ok'],needs_attention:['Refund needs attention','bad']};
const OPEN_REFUND=['reserved','submitting','pending','unknown','requires_action'];
const stageText=o=>{if(o.stage==='awaiting'){const p=o.payment;if(p&&p.mismatch)return 'Payment doesn’t match · held';return p?{created:'Waiting for payment · page being prepared',pending:'Waiting for payment · in progress',unknown:'Payment result unknown',failed:'Waiting for payment · last try failed',cancelled:'Waiting for payment · last try not completed'}[p.state]:'Waiting for payment · link sent'}
 if(o.stage==='cancelled'&&o.cancelReason&&!o.collections)return CANCEL_REASON[o.cancelReason];if(o.stage==='cancelled'&&o.cancel&&o.cancel.by==='system')return 'Cancelled · full return';return STAGES[o.stage]};
// Order-level refund state, as the backend's order view reports it (never "returned" while anything is unresolved).
function refundState(o){const C=o.collections||[];if(!C.length)return null;const all=C.flatMap(c=>c.refunds);if(all.some(r=>r.contradiction)||o.attention==='quarantine')return 'needs_attention';
 const T=refundTarget(o),S=all.filter(r=>r.status==='succeeded').reduce((n,r)=>n+r.amount,0);if(all.some(r=>OPEN_REFUND.includes(r.status)))return 'pending';if(S>=T)return 'returned';return S?'partly_returned':'requested'}
function refundTarget(o){const C=o.collections||[];const P=C.reduce((n,c)=>n+c.paid,0);const c0=o.cancel;if(!c0)return 0;if(c0.goal)return c0.goal.target;return Math.min(P,halfUp(P,c0.rule)+(c0.goodwill||0))}
// Manager cases the worker raises (job attention); the resolving tools are GK-026 / manager tooling (not built yet).
const ATTENTION={refused:'Payment arrived after the order was released',mismatch:'Payment doesn’t match the order',contradiction:'Refund changed after it was confirmed',quarantine:'Refund made outside GK Cakes'};
const nextAction=o=>{if(o.stage==='ready'&&o.mode==='delivery')return o.delivery&&o.delivery.state==='paid'?['Out for delivery','dispatch']:null;if(o.stage==='out')return ['Delivered · hand over','handover'];return ACTIONS[o.stage]||null};

// Needs you: 48-hour collection-details alert is computed (Creator answer 7); the rest are example cases.
function stockTitle(){const n=BATCHES.filter(b=>['negative','expired','today'].includes(batchState(b))).length;return `${n} ingredient warnings`}
function stockDetail(){const by=k=>BATCHES.filter(b=>batchState(b)===k);const neg=by('negative'),exp=by('expired'),td=by('today');return [neg.length?`Below zero: ${neg.map(b=>`${b.item} ${b.id} (${qtyText(b.qty,b.unit)})`).join(', ')}.`:'',exp.length?`Expired, still on the shelf: ${exp.map(b=>`${b.item} ${b.id}`).join(', ')}.`:'',td.length?`Last usable day today: ${td.map(b=>`${b.item} ${b.id}`).join(', ')}.`:''].filter(Boolean).join(' ')}
function needs(){const list=ORDERS.filter(detailsOverdue).map(o=>({kind:'details',order:o.id,title:`${detailsLabel(o)} not sent`,detail:`${o.cake} · ${dayShort(o.date)}, ${o.slot}. Handover in about ${Math.round(hoursBetween(NOW,startOf(o)))} hours; these usually go out about 48 hours before.`,action:'Send details',cap:'details'}));
 return list.concat([
 {kind:'late',order:'GK-1039',title:'Paid late · decide',detail:'Pink piping · Thu 29 Oct, 12:00. The customer started paying in time, but the £90 completed Thu 22 Oct, 09:20 — after the 192-hour point. Not accepted automatically: make it if you can and the customer agrees, or return the full £90.',action:'Decide',cap:'decide',fdetail:'Pink piping · Thu 29 Oct, 12:00. Paid late: waiting for Elina’s decision. Don’t start this cake.'},
 {kind:'delivery',order:'GK-1047',title:'Delivery quote requested',detail:'Raspberry ribbons · Sat 31 Oct · London SE16. Check the address and send a delivery price (from £100).',action:'Send quote',cap:'quote',fdetail:'Raspberry ribbons · Sat 31 Oct · London SE16. Elina prices the delivery; the cake can’t go out until delivery is paid.'},
 {kind:'refund',order:'GK-1029',title:'Delivery refund is your next step',detail:'Customer cancelled: £115 owed in total. The cake refund of £60 is confirmed; £55 for delivery is still owed.',action:'Refund delivery',cap:'refund',fdetail:'Cancelled by the customer. Refunds are handled by Elina.'},
 {kind:'unknown',order:'GK-1042',title:'Payment result unknown',detail:'Chocolate swirl · Sat 31 Oct. We’re checking with the payment provider. Don’t ask the customer to pay again.',action:'View order',cap:null,fdetail:'Chocolate swirl · Sat 31 Oct. Not paid yet as far as we know: don’t start this cake.'},
 {kind:'refund',order:'GK-1031',title:'Refund result not confirmed',detail:'£45 sent to the payment provider on Wed 21 Oct, 15:02. We’re checking with them. Don’t send another refund.',action:'View refund',cap:null,fdetail:'Cancelled. Elina is waiting for the refund result; nothing to make.'},
 {kind:'email',order:'GK-1044',title:'Email bounced · flagged by Sasha',detail:'Order confirmation for Mia D. was not delivered. The address may have a typo. Fix it, then Sasha can resend.',action:'Fix address',cap:'email',href:'support.html?state=flagged'},
 ...(seesMoney?[{kind:'attention',order:null,title:`${ORDERS.filter(o=>o.attention).length} money cases need a manager`,detail:ORDERS.filter(o=>o.attention).map(o=>`${o.id}: ${ATTENTION[o.attention].toLowerCase()}`).join(' · ')+'. Nothing is sent or refunded automatically for these.',action:'Needs attention',cap:null,href:'support.html'}]:[]),
 {kind:'stock',order:null,title:stockTitle(),detail:stockDetail(),action:'View ingredients',cap:null,href:'stock.html?filter=warnings'}])}

function reviewBar(states){const link=(k,v,l)=>{const u=new URL(location.href);u.searchParams.set(k,v);if(k==='role')u.searchParams.delete('x');const cur=k==='role'?REVIEW.role===v:(q.get('state')||states[0][0])===v;return `<a href="${esc(u.pathname.split('/').pop()+u.search)}" ${cur?'aria-current="true"':''}>${l}</a>`};
 return `<div class="review-bar" role="region" aria-label="Design preview controls"><span><strong>Design preview r03</strong> · example data · nothing is saved, sent, charged or refunded</span><span class="review-group"><span class="review-label">Role</span>${link('role','chef','Chef')}${link('role','fulfillment','Fulfilment')}${link('role','support','Technical support')}</span>${states&&states.length>1?`<span class="review-group"><span class="review-label">State</span>${states.map(([v,l])=>link('state',v,l)).join('')}</span>`:''}</div>`}
function shell(main,active,states){const nav=[['today','Today','index.html'],['orders','Orders','orders.html'],['plus','New order','new-order.html'],['calendar','Calendar & capacity','calendar.html'],['stock','Ingredients','stock.html'],['mail','Messages','messages.html'],['support','Needs attention','support.html'],['basket','Groceries',''],['money','Money','']];
 return `<a class="skip" href="#main">Skip to content</a>${reviewBar(states)}<div class="layout"><aside class="sidebar" aria-label="Chef workspace"><a class="brand" href="index.html" aria-label="GK Cake London chef workspace, Today"><img src="assets/chrome-emblem.png" alt="" width="40" height="40"><span><strong>GK Cake London</strong><small>Chef workspace</small></span></a><nav aria-label="Sections"><ul>${nav.map(([i,l,h])=>h?`<li><a href="${h}" ${active===i?'aria-current="page"':''}>${icon(i)}<span>${l}</span></a></li>`:`<li><span class="nav-later" title="Designed in a later revision">${icon(i)}<span>${l}</span><small>later</small></span></li>`).join('')}</ul></nav><div class="who"><span class="avatar" aria-hidden="true">${ROLE.initial}</span><span><strong>${ROLE.who}</strong><small>${ROLE.label}</small></span></div></aside><main id="main" class="main" tabindex="-1">${main}</main></div><div id="toast" class="toast" role="status" aria-live="polite"></div>`}
const stagePill=o=>`<span class="pill pill-${o.stage}${o.cancelReason&&!o.collections?' pill-released':''}">${o.stage==='ready'||o.stage==='done'?icon('check','pill-icon'):''}${stageText(o)}</span>`;
const actionButton=(o,primary,extraAttr='')=>{const n=nextAction(o);if(!n||!can(n[1]))return '';return `<button class="btn ${primary||n[1]==='handover'?'btn-primary':''}" type="button" data-act="${n[1]}" data-id="${o.id}"${extraAttr}>${n[0]}</button>`};
const chip=(text,tone)=>`<span class="chip-s chip-${tone}">${esc(text)}</span>`;
const sourceTag=o=>o.source&&o.source!=='online'?`<span class="tag-source">${o.source==='instagram'?'Instagram':'Email'}</span>`:'';
const mount=html=>{document.querySelector('#app').innerHTML=html};

function toast(msg){const t=document.querySelector('#toast');if(!t)return;t.textContent='';setTimeout(()=>{t.textContent=msg},30);clearTimeout(toast.t);toast.t=setTimeout(()=>t.textContent='',5200)}
function say(id,msg){const el=document.getElementById(id);if(el)el.textContent=msg}

// Shared actions: "later" notices and the two-step handover (Today and order detail).
document.addEventListener('click',e=>{const b=e.target.closest('button');if(!b)return;
 if(b.hasAttribute('data-soon')){toast('Designed in a later revision. Nothing changed.');return}
 if(b.hasAttribute('data-keep')){const row=b.closest('.confirm-row');const orig=row.previousElementSibling;orig.hidden=false;row.remove();orig.focus();return}
 const act=b.dataset.act;if(!act)return;const o=order(b.dataset.id);
 if(act==='handover'){b.hidden=true;b.insertAdjacentHTML('afterend',`<div class="confirm-row" role="group" aria-label="Confirm handover"><p class="small"><strong>Hand over ${esc(o.id)} to ${esc(o.customer)}</strong> · ${MODES[o.mode]}. Recorded as ${esc(ROLE.who)} (${esc(ROLE.label)}). This completes the order.</p><button class="btn btn-primary" type="button" data-act="handover-confirmed" data-id="${esc(o.id)}">Confirm handover</button><button class="btn btn-quiet" type="button" data-keep>Not yet</button></div>`);b.nextElementSibling.querySelector('button').focus();e.stopImmediatePropagation();return}
 if(act==='handover-confirmed'){const row=b.closest('.confirm-row');row.innerHTML=`<p class="small" tabindex="-1"><strong>Handed over ✓</strong> · recorded by ${esc(ROLE.who)}</p>`;row.querySelector('p').focus();toast('Preview: handed over. The order is complete.');e.stopImmediatePropagation();return}
 if(act==='start'&&can('batches'))return; // the chef chooses batches (page script)
 const msg={start:'Preview: start recorded at this time. Elina chooses the ingredient batches used.',ready:'Preview: marked ready for handover.',dispatch:'Preview: out for delivery. Handover is recorded when the cake reaches the customer.'}[act];
 if(msg){b.disabled=true;b.textContent='Done ✓';toast(msg);e.stopImmediatePropagation()}});

// Ingredients and component batches (K07, GK-007). Expiry = last usable London date.
const BATCHES=[
 {id:'C-0311',item:'Vanilla sponge',kind:'component',qty:900,unit:'g',expires:'2026-10-23',made:'2026-10-20'},
 {id:'C-0315',item:'Vanilla sponge',kind:'component',qty:600,unit:'g',expires:'2026-10-25',made:'2026-10-21'},
 {id:'C-0320',item:'Chocolate sponge',kind:'component',qty:1200,unit:'g',expires:'2026-10-24',made:'2026-10-21'},
 {id:'C-0309',item:'Classic cream-cheese filling',kind:'component',qty:400,unit:'g',expires:'2026-10-22',made:'2026-10-19'},
 {id:'C-0316',item:'Classic cream-cheese filling',kind:'component',qty:1100,unit:'g',expires:'2026-10-24',made:'2026-10-21'},
 {id:'C-0322',item:'Chocolate cream-cheese filling',kind:'component',qty:1300,unit:'g',expires:'2026-10-24',made:'2026-10-21'},
 {id:'C-0323',item:'White chocolate filling',kind:'component',qty:800,unit:'g',expires:'2026-10-23',made:'2026-10-20'},
 {id:'C-0321',item:'Snickers filling',kind:'component',qty:900,unit:'g',expires:'2026-10-25',made:'2026-10-21'},
 {id:'C-0302',item:'Raspberry compote',kind:'component',qty:350,unit:'g',expires:'2026-10-21',made:'2026-10-17'},
 {id:'C-0317',item:'Raspberry compote',kind:'component',qty:500,unit:'g',expires:'2026-10-26',made:'2026-10-21'},
 {id:'C-0312',item:'Swiss meringue buttercream',kind:'component',qty:450,unit:'g',expires:'2026-10-24',made:'2026-10-19'},
 {id:'I-1108',item:'Milk for soaking',kind:'ingredient',qty:1800,unit:'ml',expires:'2026-10-26',made:'2026-10-20'},
 {id:'I-1012',item:'Butter',kind:'ingredient',qty:-200,unit:'g',expires:'2026-11-14',made:'2026-10-12',reason:'Made buttercream C-0312 before the new butter delivery was recorded.'},
 {id:'I-1018',item:'Cream cheese',kind:'ingredient',qty:2000,unit:'g',expires:'2026-10-22',made:'2026-10-15'},
 {id:'I-1021',item:'Frozen raspberries',kind:'ingredient',qty:1500,unit:'g',expires:'2027-01-31',made:'2026-10-10'},
 {id:'I-1030',item:'Eggs',kind:'ingredient',qty:24,unit:'eggs',expires:'2026-11-01',made:'2026-10-18'}];
const batchState=b=>b.qty<0?'negative':b.expires<TODAY?'expired':b.expires===TODAY?'today':addDays(TODAY,2)>=b.expires?'soon':'ok';
const BATCH_STATE={negative:['Below zero · flagged','bad'],expired:['Expired · not usable','bad'],today:['Last usable day today','warn'],soon:['Expires soon','warn'],ok:['OK','ok']};
const qtyText=(n,u)=>`${n<0?'−':''}${Math.abs(n).toLocaleString('en-GB')} ${u}`;
// Planned lines per recipe version (VR-6 is GK-007's supplied example; the others are example lines until GK-038's measurements).
const RECIPES={'VR-6 v1':[['Vanilla sponge',300,'g'],['Milk for soaking',45,'g'],['Classic cream-cheese filling',550,'g'],['Raspberry compote',200,'g'],['Swiss meringue buttercream',600,'g']],
 'CR-7 v1':[['Chocolate sponge',400,'g'],['Raspberry compote',250,'g'],['Chocolate cream-cheese filling',650,'g'],['Swiss meringue buttercream',700,'g']],
 'SN-6 v1':[['Chocolate sponge',300,'g'],['Snickers filling',500,'g'],['Swiss meringue buttercream',600,'g']],
 'WCR-8 custom v1':[['Vanilla sponge',450,'g'],['White chocolate filling',700,'g'],['Raspberry compote',250,'g'],['Swiss meringue buttercream',800,'g']],
 'WCR-7 v1':[['Vanilla sponge',400,'g'],['White chocolate filling',600,'g'],['Raspberry compote',200,'g'],['Swiss meringue buttercream',700,'g']]};

// Chef start: choose a batch for every planned line; nothing is chosen automatically.
function batchPanel(o){const lines=RECIPES[o.recipe]||[];
 return `<div class="batch-panel" role="group" aria-labelledby="bp-h-${o.id}"><p id="bp-h-${o.id}" class="strong">Start making ${esc(o.id)} · choose the batches you use</p><p class="muted small">Recipe ${esc(o.recipe)}${o.recipe==='VR-6 v1'?'':' (example lines until the measured recipes arrive)'}. Nothing is picked for you. Expired batches can’t be used.</p>
 ${lines.map(([item,qty,unit],i)=>{const opts=BATCHES.filter(b=>b.item===item);return `<div class="batch-line" data-line="${i}" data-qty="${qty}" data-unit="${unit}"><label class="field">${esc(item)} · planned ${qty.toFixed(2)} ${unit}<select data-batch><option value="">Choose a batch</option>${opts.map(b=>{const st=batchState(b);return `<option value="${b.id}" ${st==='expired'?'disabled':''}>${b.id} · ${qtyText(b.qty,b.unit)} left · use by ${dayShort(b.expires)}${st==='expired'?' · expired':st==='today'?' · last day today':''}</option>`}).join('')}${opts.length?'':'<option value="" disabled>No batch recorded</option>'}</select></label><div class="batch-note" aria-live="polite"></div></div>`}).join('')}
 <p class="small muted">The automatic 48-hour status never uses stock; this start does. Corrections later are added as separate, reasoned entries.</p>
 <div class="actions"><button class="btn btn-primary" type="button" data-batch-confirm data-id="${o.id}" disabled>Start making and use these batches</button><button class="btn btn-quiet" type="button" data-batch-cancel>Not now</button></div><p class="small" data-batch-status aria-live="polite"></p></div>`}
function checkBatchPanel(p){let ok=true,missing=0;p.querySelectorAll('.batch-line').forEach(l=>{const sel=l.querySelector('[data-batch]');const note=l.querySelector('.batch-note');const b=BATCHES.find(x=>x.id===sel.value);const need=Number(l.dataset.qty),unit=l.dataset.unit;
  if(!b){note.innerHTML='';ok=false;missing++;return}
  if(b.unit!==unit){if(!note.querySelector('[data-amount]'))note.innerHTML=`<p class="warn-text small">This batch is in ${b.unit}; the recipe says ${unit}. There is no measured conversion yet, so nothing is converted.</p><label class="field">Amount you use (${b.unit})<input data-amount inputmode="decimal" autocomplete="off"></label>`;if(!(Number(note.querySelector('[data-amount]').value)>0))ok=false;return}
  const after=b.qty-need;if(after<0){if(!note.querySelector('[data-reason]'))note.innerHTML=`<p class="warn-text small"><strong>This takes ${b.id} below zero (${qtyText(after,b.unit)}).</strong> That’s allowed, but the batch stays flagged until you correct it. Add a reason.</p><label class="field">Reason (required)<input data-reason autocomplete="off"></label>`;if(!note.querySelector('[data-reason]').value.trim())ok=false}else note.innerHTML=`<p class="small muted">${qtyText(b.qty,b.unit)} → ${qtyText(after,b.unit)} left in ${b.id}.</p>`});
 const btn=p.querySelector('[data-batch-confirm]');btn.disabled=!ok;p.querySelector('[data-batch-status]').textContent=ok?'Ready to start.':missing?`Choose ${missing} more ${missing===1?'batch':'batches'}.`:'Add the missing reason or amount.'}
document.addEventListener('click',e=>{const b=e.target.closest('button');if(!b)return;
 if(b.dataset.act==='start'&&can('batches')){const o=order(b.dataset.id);if(b.nextElementSibling&&b.nextElementSibling.classList.contains('batch-panel'))return;b.hidden=true;b.insertAdjacentHTML('afterend',batchPanel(o));const p=b.nextElementSibling;checkBatchPanel(p);p.querySelector('select').focus();return}
 if(b.hasAttribute('data-batch-cancel')){const p=b.closest('.batch-panel');const orig=p.previousElementSibling;p.remove();orig.hidden=false;orig.focus();return}
 if(b.hasAttribute('data-batch-confirm')){const p=b.closest('.batch-panel');p.innerHTML=`<p class="small" tabindex="-1"><strong>Started ✓</strong> · batches recorded (preview only; no stock changed)</p>`;p.querySelector('p').focus();toast('Preview: started at this time with the chosen batches. Nothing was saved.')}});
document.addEventListener('input',e=>{const p=e.target.closest('.batch-panel');if(p)checkBatchPanel(p)});
document.addEventListener('change',e=>{const p=e.target.closest('.batch-panel');if(p)checkBatchPanel(p)});

// Finished cakes (ready stock or resale), mirroring the GK-023 lots: created unreleased, released, withdrawn.
const LOTS=[
 {id:'L-0005',kind:'ready',cake:'rasp',qty:2,reserved:1,price:9500,expires:'2026-10-25',state:'released',version:3},
 {id:'L-0007',kind:'ready',cake:'pink',qty:1,reserved:0,price:8000,expires:'2026-10-24',state:'released',version:2},
 {id:'L-0008',kind:'resale',cake:'garden',qty:1,reserved:0,base:12000,price:6000,from:'GK-1027 (cancelled Tue 20 Oct)',originalHandover:london('2026-10-23','14:00'),expires:'2026-10-23',state:'unreleased',version:1},
 {id:'L-0004',kind:'ready',cake:'pink',qty:1,reserved:0,price:8000,expires:'2026-10-20',state:'withdrawn',version:4}];
const lotOffered=l=>l.state==='released'&&l.expires>=TODAY&&(!l.originalHandover||before(NOW,l.originalHandover))&&l.qty-l.reserved>0;
const CODES={DEMO10:{kind:'percentage',bps:1000,label:'10% off the cake'},FREEDELIVERY:{kind:'free_delivery',label:'Free GK delivery (after your distance check)'}};
