// round9 independent review — recheck of the "已窮舉整個色域,不存在任何單一顏色" claim
// for the unread badge (T-081b-final-delivery.md §四.2 / chrome.css .nav-tab__badge).
const L=(h)=>{const c=[1,3,5].map(i=>parseInt(h.slice(i,i+2),16)/255).map(v=>v<=0.03928?v/12.92:((v+0.055)/1.055)**2.4);return 0.2126*c[0]+0.7152*c[1]+0.0722*c[2];};
const R=(a,b)=>{const x=L(a),y=L(b);return (Math.max(x,y)+0.05)/(Math.min(x,y)+0.05);};
const hex=(r,g,b)=>'#'+[r,g,b].map(v=>v.toString(16).padStart(2,'0')).join('');
const BG=['#191c24','#242832','#2c3350'];   // --color-bg / --color-card / --color-indigo

console.log('== shipped state ==');
console.log('fill #ba5953 vs white(--color-on-danger):', R('#ba5953','#ffffff').toFixed(2));
for(const b of BG) console.log('  fill vs',b,'=',R('#ba5953',b).toFixed(2));
console.log('ring --color-bg #191c24 vs --color-indigo #2c3350 =', R('#191c24','#2c3350').toFixed(2), '(NIT-1)');

console.log('\n== A. text pinned to WHITE (the premise chrome.css states) ==');
let white=0;
for(let r=0;r<256;r+=2)for(let g=0;g<256;g+=2)for(let b=0;b<256;b+=2){
  const h=hex(r,g,b);
  if(R(h,'#ffffff')>=4.5 && BG.every(x=>R(h,x)>=3)) white++;
}
console.log('solutions:',white,' → the claim IS true under this premise');
console.log('analytic proof: AA-vs-white needs Lf<=0.1833; 3:1 vs #2c3350 needs Lf>=0.2044 — disjoint.');

console.log('\n== B. text FREE (--color-on-danger is a token this ticket carved out;');
console.log('      its only readers are chrome.css:271, office.css:275, office.css:383) ==');
let free=[],reds=[];
for(let r=0;r<256;r+=4)for(let g=0;g<256;g+=4)for(let b=0;b<256;b+=4){
  const h=hex(r,g,b);
  if(!BG.every(x=>R(h,x)>=3)) continue;
  if(R(h,'#000000')>=4.5) free.push(h);
}
for(let r=0;r<256;r+=2)for(let g=0;g<256;g+=2)for(let b=0;b<256;b+=2){
  if(!(r>g+40&&r>b+40)) continue;
  const h=hex(r,g,b);
  if(BG.every(x=>R(h,x)>=3) && R(h,'#000000')>=4.5) reds.push(h);
}
console.log('solutions with a dark count colour (step 4):',free.length);
console.log('RED-hue-only solutions (independent, finer step 2 scan):',reds.length);
console.log('example #ff8f88: vs black',R('#ff8f88','#000000').toFixed(2),
  '| vs three bgs',BG.map(x=>R('#ff8f88',x).toFixed(2)).join(' / '),'— no ring needed');
