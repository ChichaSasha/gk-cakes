const {chromium}=require('/Users/sasha.chycha/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright');
const out={};const OUT=require('os').tmpdir();
(async()=>{const b=await chromium.launch({executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'});
for(const [rev,port] of [['r10',8774],['r11',8775]]){const p=await b.newPage({viewport:{width:1440,height:950}});await p.goto(`http://127.0.0.1:${port}/shop.html`);await p.evaluate(()=>document.fonts.ready);await p.evaluate(()=>scrollTo(0,380));await p.mouse.move(5,5);await p.waitForTimeout(1500);
 const tile=p.locator('.product:nth-child(2) .product-photo');const box=await tile.boundingBox();const clip={x:box.x,y:box.y-8,width:box.width,height:box.height+8};
 const grab=async()=>(await p.screenshot({clip})).toString('base64');
 const rest=await grab();const frames=[];await p.mouse.move(box.x+box.width*0.5,box.y+box.height*0.55);const t0=Date.now();while(Date.now()-t0<1500){frames.push([Date.now()-t0,await grab()])}
 // cursor sweep while hovered (spotlight test): positions across the tile after settling
 await p.waitForTimeout(400);const settled=await grab();const sweep=[];for(const fx of [0.15,0.5,0.85]){await p.mouse.move(box.x+box.width*fx,box.y+box.height*0.3);await p.waitForTimeout(120);sweep.push(await grab())}
 const cut=await tile.evaluate(e=>({cutout:getComputedStyle(e.querySelector('.cutout')).transform,tile:getComputedStyle(e).transform,before:getComputedStyle(e,'::before').content,mx:e.style.getPropertyValue('--mx')}));
 const res=await p.evaluate(async({rest,frames,settled,sweep})=>{const dec=async b64=>{const i=new Image();i.src='data:image/png;base64,'+b64;await i.decode();const c=document.createElement('canvas');c.width=i.width;c.height=i.height;const x=c.getContext('2d');x.drawImage(i,0,0);return {w:c.width,h:c.height,d:x.getImageData(0,0,c.width,c.height).data}};
  const R=await dec(rest);const L=(d,i)=>.2126*d[i]+.7152*d[i+1]+.0722*d[i+2];
  // background band: upper area of the tile, inside the rounded corners, away from index label and heart
  const band=(img)=>{const pts=[];for(let y=Math.round(img.h*.10);y<Math.round(img.h*.20);y+=2)for(let x=Math.round(img.w*.25);x<Math.round(img.w*.75);x+=2)pts.push((y*img.w+x)*4);return pts};
  const pts=band(R);let maxBright=0,sparkPx=0,maxFull=0;
  for(const [t,f] of frames){const F=await dec(f);let fb=0,cnt=0;for(const i of pts){const dv=L(F.d,i)-L(R.d,i);if(dv>fb)fb=dv;if(dv>12)cnt++}maxBright=Math.max(maxBright,fb);sparkPx=Math.max(sparkPx,cnt/pts.length);
   let diffs=[];for(let i=0;i<F.d.length;i+=4*13)diffs.push(Math.abs(L(F.d,i)-L(R.d,i)));diffs.sort((a,b)=>a-b);maxFull=Math.max(maxFull,diffs[Math.floor(diffs.length*.99)])}
  const S=await dec(settled);let spot=0;for(const s of sweep){const F=await dec(s);for(const i of pts){spot=Math.max(spot,Math.abs(L(F.d,i)-L(S.d,i)))}}
  
  // aligned residual over the tile interior: undo the vertical lift, then measure what still changes (sweep/zoom/rotation/shimmer)
  const W=R.w,H=R.h;let worstShare=0,worstMean=0;
  for(const [t,f] of frames){const F=await dec(f);let best=null;
   for(let dy=-8;dy<=1;dy++){let sum=0,n=0;for(let y=Math.round(H*.12);y<Math.round(H*.76);y+=3)for(let x=14;x<W-14;x+=3){const yy=y-dy;if(yy<0||yy>=H)continue;sum+=Math.abs(L(F.d,(y*W+x)*4)-L(R.d,(yy*W+x)*4));n++}const m=sum/n;if(!best||m<best.m)best={dy,m}}
   let bright=0,n=0;for(let y=Math.round(H*.12);y<Math.round(H*.76);y+=3)for(let x=14;x<W-14;x+=3){const yy=y-best.dy;if(yy<0||yy>=H)continue;if(L(F.d,(y*W+x)*4)-L(R.d,(yy*W+x)*4)>25)bright++;n++}
   worstShare=Math.max(worstShare,bright/n);worstMean=Math.max(worstMean,best.m)}
return {frames:frames.length,bandMaxBrightening:Math.round(maxBright*10)/10,bandShareBrightenedOver12:Math.round(sparkPx*1000)/10+'%',tileP99Change:Math.round(maxFull*10)/10,spotlightBandChangeOnCursorMove:Math.round(spot*10)/10,alignedResidualMeanMax:Math.round(worstMean*100)/100,alignedBrighteningShareMax:Math.round(worstShare*1000)/10+'%'}},{rest,frames:frames.map(f=>[f[0],f[1]]),settled,sweep});
 out[rev]={...res,...cut,lift_px:Math.round((box.y-(await tile.boundingBox()).y)*10)/10};
 // filmstrip for the record: rest, mid, settled
 const mid=frames[Math.floor(frames.length/3)][1];require('fs').writeFileSync(OUT+`/${rev}-rest.png`,Buffer.from(rest,'base64'));require('fs').writeFileSync(OUT+`/${rev}-mid.png`,Buffer.from(mid,'base64'));require('fs').writeFileSync(OUT+`/${rev}-hover.png`,Buffer.from(settled,'base64'));
 await p.close()}
console.log(JSON.stringify(out,null,1));require('fs').writeFileSync(OUT+'/motion-measure.json',JSON.stringify(out,null,1));await b.close()})();
