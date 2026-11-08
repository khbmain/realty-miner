// ==UserScript==
// @name         Realty Miner — Unegui details
// @namespace    realty-data-miner.local
// @version      3.2.0
// @description  Collect the saved listing queue through normal browser pages into your local dashboard.
// @match        https://www.unegui.mn/*
// @noframes
// @grant        GM_xmlhttpRequest
// @grant        GM_getValue
// @grant        GM_setValue
// @connect      127.0.0.1
// @run-at       document-end
// ==/UserScript==
(function(){
'use strict';
if(document.getElementById('realty-collector-v2'))return;
const token='__LOCAL_TOKEN__',base='http://127.0.0.1:8080';
const box=document.createElement('div');box.id='realty-collector-v2';box.style.cssText='position:fixed;bottom:20px;left:20px;z-index:2147483647;background:#122237;color:white;padding:16px;border-radius:12px;font:13px/1.5 sans-serif;max-width:320px;box-shadow:0 4px 20px #0004';
const status=document.createElement('div'),saveButton=document.createElement('button'),runButton=document.createElement('button');
status.textContent='Realty Miner · Queue v2';saveButton.textContent='Энэ зарыг хадгалах';runButton.textContent='Бүх дэлгэрэнгүйг эхлүүлэх';
for(const b of [saveButton,runButton])b.style.cssText='background:#138c74;color:white;border:0;padding:9px;margin:8px 5px 0 0;border-radius:7px;cursor:pointer';
box.append(status,saveButton,runButton);document.body.append(box);
let running=false,leaseTimer;const isAdvert=/^\/adv\/\d+_/.test(location.pathname);const worker=sessionStorage.getItem('realty-worker')||crypto.randomUUID();sessionStorage.setItem('realty-worker',worker);
function waitFor(test,timeout,batch){return new Promise((resolve,reject)=>{let finished=false,observer,timer,deadline;function finish(error,value){if(finished)return;finished=true;observer?.disconnect();clearInterval(timer);clearTimeout(deadline);error?reject(error):resolve(value)}function check(){try{if(batch&&!GM_getValue('realty-batch:'+worker,false)){finish(Error('Түр зогссон'));return}if(document.querySelector('iframe[src*="captcha"],iframe[src*="challenges.cloudflare"],.g-recaptcha,.h-captcha')){finish(Error('CAPTCHA — зогслоо'));return}const result=test();if(result)finish(null,result)}catch(e){finish(e)}}observer=new MutationObserver(check);observer.observe(document.body,{subtree:true,childList:true,characterData:true,attributes:true,attributeFilter:['disabled','aria-busy','href']});timer=setInterval(check,250);deadline=setTimeout(()=>finish(Error('Текст эсвэл утас ачаалах хугацаа дууслаа')),timeout);check()})}
function request(path,data){return new Promise((resolve,reject)=>GM_xmlhttpRequest({method:data?'POST':'GET',url:base+path,headers:{'Content-Type':'application/json','Authorization':'Bearer '+token,'X-Collector-Worker':worker},data:data?JSON.stringify(data):undefined,timeout:20000,onload:r=>{if(r.status!==200){reject(Error('Local API: '+r.status));return}try{resolve(JSON.parse(r.responseText))}catch{reject(Error('Invalid response'))}},onerror:()=>reject(Error('Dashboard холбогдсонгүй')),ontimeout:()=>reject(Error('Local API timeout'))}))}
const visible=e=>e.getClientRects().length>0;
function isClosed(){return [...document.querySelectorAll('button,[data-marker]')].some(e=>visible(e)&&/^Зар хаагдсан байна[.!]?$/i.test(e.textContent.trim()))}
function collectCategory(){
const nav=[...document.querySelectorAll('[data-slot="breadcrumb"],nav[aria-label="breadcrumb"]')].find(visible);
if(!nav)return [];
const nodes=nav.querySelectorAll('[data-slot="breadcrumb-link"],[data-slot="breadcrumb-page"]');
return [...nodes].map(n=>n.textContent.replace(/\s+/g,' ').trim()).filter(t=>t&&t!=='Бүх зар').slice(0,12);
}
function collect(){
const match=location.pathname.match(/^\/adv\/(\d+)_/);if(!match||!document.querySelector('h1'))throw Error('Зар нээгдсэнгүй; хандалтаа шалгаарай');
const phones=[...new Set([...document.querySelectorAll('a[href^="tel:"],button[data-marker*="cta_call"]')].filter(visible).map(e=>(e.getAttribute('href')?.slice(4)||e.textContent).replace(/[^+\d]/g,'')).filter(s=>/^\+?\d{8,15}$/.test(s)))];
const attributes={};document.querySelectorAll('dl dt').forEach(dt=>{const dd=dt.nextElementSibling;if(dd&&dd.tagName==='DD')attributes[dt.textContent.trim().replace(/:$/,'')]=dd.textContent.trim()});
const category=collectCategory();if(category.length){attributes['Ангиллын зам']=category.join(' → ');attributes['Дэд ангилал']=category[category.length-1];}
const heading=[...document.querySelectorAll('h2')].find(e=>e.textContent.trim()==='Тайлбар');let section=heading?.parentElement,description='';
for(let i=0;section&&i<4;i++,section=section.parentElement){const text=section.textContent.replace(/^Тайлбар\s*/,'').trim();if(text.length>15){description=text;break}}
return {status:isClosed()?'closed':'active',external_id:match[1],url:location.origin+location.pathname,phones,attributes,description:description.slice(0,50000)};
}
function report(message){request('/api/collector/status',{mode:isAdvert?'details':'listings',message,url:location.href}).catch(()=>{})}
function stop(message){clearInterval(leaseTimer);GM_setValue(isAdvert?'realty-batch:'+worker:'realty-listing-batch:'+worker,false);status.textContent=message;runButton.textContent='Үргэлжлүүлэх';running=false;report(message)}
async function process(batch){
if(running)return;running=true;const deadline=Date.now()+10000;const remaining=()=>Math.max(1,deadline-Date.now());
try{
status.textContent='Текст мэдээлэл ачаалж байна…';await waitFor(()=>document.querySelector('h1')&&(document.querySelector('dl dt')||[...document.querySelectorAll('h2')].some(e=>e.textContent.trim()==='Тайлбар')),remaining(),batch);
if(batch&&!GM_getValue('realty-batch:'+worker,false)){running=false;return}
if(document.querySelector('iframe[src*="captcha"],iframe[src*="challenges.cloudflare"],.g-recaptcha,.h-captcha')||!document.querySelector('h1'))throw Error('Хандалт эсвэл CAPTCHA — зогслоо');
const more=[...document.querySelectorAll('button')].find(e=>e.textContent.trim()==='Цааш унших'&&visible(e));if(more)more.click();
if(!isClosed()){
status.textContent='Утасны товчийг хүлээж байна…';
const phone=await waitFor(()=>isClosed()?'closed':collect().phones.length?true:[...document.querySelectorAll('button')].find(e=>e.textContent.trim()==='Дугаар харах'&&visible(e)&&!e.disabled&&e.getAttribute('aria-busy')!=='true'),remaining(),batch);
if(phone!==true&&phone!=='closed')phone.click();let lastPhoneClick=Date.now(),phoneAttempts=1;
await waitFor(()=>{const dialog=document.querySelector('[role="dialog"]');if(dialog&&/Зөвшөөрөх|Нэвтрэх|captcha/i.test(dialog.textContent))throw Error('Зөвшөөрөл эсвэл нэвтрэх шаардлагатай — зогслоо');if(isClosed()||collect().phones.length)return true;const retry=[...document.querySelectorAll('button')].find(e=>e.textContent.trim()==='Дугаар харах'&&visible(e)&&!e.disabled&&e.getAttribute('aria-busy')!=='true');if(retry&&phoneAttempts<3&&Date.now()-lastPhoneClick>=2000){phoneAttempts++;lastPhoneClick=Date.now();retry.click()}return false},remaining(),batch);
if(batch&&!GM_getValue('realty-batch:'+worker,false)){running=false;return}
}
const payload=collect();if(payload.status!=='closed'&&!payload.phones.length)throw Error('Дугаар харагдсангүй — зогслоо');payload.complete=true;
await request('/api/collector/lease');const ack=await request('/api/collector/details',payload);if(!ack.saved||ack.saved_id!==payload.external_id)throw Error('Miner хадгалалтыг баталгаажуулсангүй');status.textContent=payload.status==='closed'?'✓ Хаагдсан зар хадгалсан':'✓ Хадгалсан · '+payload.phones.length+' утас';
if(!batch){running=false;return}
runButton.textContent='Түр зогсоох';
const next=ack.next;if(!next)throw Error('Хадгалсан боловч miner дараагийн даалгаврыг өгсөнгүй; Үргэлжлүүлэх дарна уу');
if(next.done){stop('✓ Бүх хадгалсан зарын дэлгэрэнгүй дууслаа · '+next.completed);return}
status.textContent=next.completed+' / '+next.total+' · хадгалсан, дараагийн зар руу…';
if(!GM_getValue('realty-batch:'+worker,false)){running=false;return}
const u=new URL(next.url);if(u.origin!=='https://www.unegui.mn'||!/^\/adv\/\d+_/.test(u.pathname))throw Error('Буруу queue URL');location.assign(u.href);
}catch(e){
if(batch&&GM_getValue('realty-batch:'+worker,false)&&/хугацаа дууслаа|Дугаар харагдсангүй|Зар нээгдсэнгүй/.test(e.message)){
try{const id=location.pathname.match(/^\/adv\/(\d+)_/)?.[1];report('Алгассан: '+e.message);const ack=await request('/api/collector/skip',{external_id:id,reason:e.message});if(!ack.skipped)throw Error('Алгасалтыг баталгаажуулаагүй');if(ack.next.done){stop('Дараалалд одоо авах зар алга · үлдсэн '+(ack.next.pending||0));return}const u=new URL(ack.next.url);if(u.origin!=='https://www.unegui.mn'||!/^\/adv\/\d+_/.test(u.pathname))throw Error('Буруу queue URL');if(GM_getValue('realty-batch:'+worker,false))location.assign(u.href);return}catch(skipError){stop('Зогссон: '+skipError.message);return}
}
stop('Зогссон: '+e.message)
}
}
async function crawlListingPage(){
if(running)return;running=true;runButton.textContent='Түр зогсоох';GM_setValue('realty-listing-batch:'+worker,true);
try{
status.textContent='Жагсаалтын текст, thumbnail холбоосыг хадгалж байна…';
await waitFor(()=>document.querySelector('[itemscope][itemtype*="schema.org/Product"]'),45000,false);
const ack=await request('/api/collector/listings',{url:location.href,html:document.documentElement.outerHTML});
if(!ack.saved||ack.saved_url!==location.href)throw Error('Miner хадгалалтыг баталгаажуулаагүй');
if(ack.done){running=false;GM_setValue('realty-listing-batch:'+worker,false);status.textContent='✓ Miner: сүүлийн хуудас хадгалагдсан';return}
const task=new URL(ack.next);if(task.origin!==location.origin||task.pathname!==location.pathname)throw Error('Miner буруу даалгавар өгсөн');
const here=new URL(location.href),current=Number(here.searchParams.get('page')||1);
status.textContent='✓ '+ack.count+' зар хадгалсан · хуудас '+current;
const candidates=[...document.querySelectorAll('a[href]')].filter(a=>{const u=new URL(a.href,location.href);return visible(a)&&u.origin===here.origin&&u.pathname===here.pathname&&u.href===task.href});
const next=candidates.find(a=>/Дараагийнх/.test(a.textContent));
if(!next)throw Error('Miner дараагийн хуудас өгсөн боловч хуудсан дээрх холбоос олдсонгүй');
const fingerprint=()=>[...document.querySelectorAll('[itemscope][itemtype*="schema.org/Product"] a[itemprop="url"]')].map(a=>a.getAttribute('href')).join('|');
const before=fingerprint();
status.textContent+=(ack.all_duplicates?' · бүгд давхардсан, шууд дараагийнх':' · шинэ '+ack.new_count)+' · '+(current+1)+'-р хуудас руу шилжиж байна…';
if(!ack.all_duplicates)await new Promise(r=>setTimeout(r,5000));if(!GM_getValue('realty-listing-batch:'+worker,false))return;
next.scrollIntoView({block:'center'});next.click();
// Client-side pagination keeps this userscript alive: wait for the new cards and continue.
await waitFor(()=>{if(!GM_getValue('realty-listing-batch:'+worker,false))throw Error('Түр зогссон');return Number(new URL(location.href).searchParams.get('page')||1)===current+1&&fingerprint()!==before&&document.querySelector('[itemscope][itemtype*="schema.org/Product"]')},45000,false);
running=false;crawlListingPage();
}catch(e){stop('Зогссон: '+e.message)}
}
async function start(){
if(/^\/l-hdlh\//.test(location.pathname)){crawlListingPage();return}

if(running)return;
try{await request('/api/collector/lease')}catch(e){status.textContent=e.message.includes('409')?'5 collector tab ажиллаж байна':'Dashboard-оо асаагаад Үргэлжлүүлэх дарна уу';runButton.textContent='Үргэлжлүүлэх';return}
GM_setValue('realty-batch:'+worker,true);runButton.textContent='Түр зогсоох';clearInterval(leaseTimer);leaseTimer=setInterval(()=>request('/api/collector/lease').catch(e=>stop('Холболт тасарсан: '+e.message)),20000);
if(!isAdvert){status.textContent='Miner-ээс дараагийн зар авч байна…';try{const next=await request('/api/collector/next');if(next.done){stop('✓ Бүх зарын дэлгэрэнгүй дууссан');return}const u=new URL(next.url);if(u.origin!=='https://www.unegui.mn'||!/^\/adv\/\d+_/.test(u.pathname))throw Error('Буруу queue URL');location.assign(u.href)}catch(e){stop('Зогссон: '+e.message)}return}
try{const next=await request('/api/collector/next');if(next.done){stop('Одоогоор сул даалгавар алга · үлдсэн '+(next.pending||0));return}if(next.external_id!==location.pathname.match(/^\/adv\/(\d+)_/)?.[1]){const u=new URL(next.url);if(u.origin!=='https://www.unegui.mn'||!/^\/adv\/\d+_/.test(u.pathname))throw Error('Буруу queue URL');location.assign(u.href);return}process(true)}catch(e){stop('Зогссон: '+e.message)}
}
saveButton.hidden=!isAdvert;saveButton.onclick=()=>process(false);
runButton.onclick=()=>{if(running){stop('Түр зогссон');return}start()};
// Opening any Unegui page starts or resumes the single local worker.
start();
})();
