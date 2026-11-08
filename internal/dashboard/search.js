(function(root){
  'use strict';
  const letters={а:'a',б:'b',в:'v',г:'g',д:'d',е:'e',ё:'yo',ж:'j',з:'z',и:'i',й:'i',к:'k',л:'l',м:'m',н:'n',о:'o',ө:'o',п:'p',р:'r',с:'s',т:'t',у:'u',ү:'u',ф:'f',х:'h',ц:'ts',ч:'ch',ш:'sh',щ:'sh',ъ:'',ы:'i',ь:'',э:'e',ю:'yu',я:'ya'};
  function normalize(value){
    return String(value??'').normalize('NFKC').toLowerCase()
      .replace(/[а-яөүё]/g,c=>letters[c])
      .replace(/kh/g,'h')
      .replace(/([a-z])[-‐‑–—]+(?=[a-z])/g,'$1')
      .replace(/(\d)([a-z])/g,'$1 $2').replace(/([a-z])(\d)/g,'$1 $2')
      .replace(/[^a-z0-9]+/g,' ').trim()
      .replace(/\b(?:uruu|oroo|orootoi|orootoi|orootei|uruutei)\b/g,'oroo');
  }
  // One insertion, deletion, replacement or adjacent transposition in longer words.
  function near(a,b){
    if(a.length<4||b.length<4||Math.max(a.length,b.length)<5||Math.abs(a.length-b.length)>1)return false;
    let i=0;while(i<Math.min(a.length,b.length)&&a[i]===b[i])i++;
    if(a.length===b.length)return a.slice(i+1)===b.slice(i+1)||(a[i]===b[i+1]&&a[i+1]===b[i]&&a.slice(i+2)===b.slice(i+2));
    return a.length>b.length?a.slice(i+1)===b.slice(i):a.slice(i)===b.slice(i+1);
  }
  const cache=new WeakMap();
  function index(row){
    let entry=cache.get(row);if(entry)return entry;
    const fields=[row.title,row.location].map(normalize);
    const words=fields.flatMap(x=>x.split(' ').filter(Boolean));
    const joined=fields.map(x=>x.replaceAll(' ',''));
    const candidates=[...words];
    for(const field of fields){const parts=field.split(' ');for(let i=0;i<parts.length-1;i++)if(/^[a-z]+$/.test(parts[i])&&/^[a-z]+$/.test(parts[i+1]))candidates.push(parts[i]+parts[i+1]);}
    entry={words,joined,candidates,title:normalize(row.title),identifiers:[row.external_id,...(row.phones||[])].map(x=>String(x??'').replace(/\D/g,''))};
    cache.set(row,entry);return entry;
  }
  function compile(query){
    const normalized=normalize(query);
    const rooms=[...normalized.matchAll(/\b(\d+) oroo\b/g)].map(m=>m[1]);
    const tokens=normalized.replace(/\b\d+ oroo\b/g,' ').split(' ').filter(Boolean);
    return row=>{
      if(!normalized)return true;
      const data=index(row);
      if(!rooms.every(n=>new RegExp('(?:^| )'+n+' oroo(?: |$)').test(data.title)))return false;
      return tokens.every(token=>{
        if(/^\d+$/.test(token))return data.words.includes(token)||(token.length>=4&&data.identifiers.some(id=>id.includes(token)));
        return data.joined.some(text=>text.includes(token))||data.candidates.some(word=>near(token,word));
      });
    };
  }
  const api={normalize,compile};
  if(typeof module!=='undefined'&&module.exports)module.exports=api;
  else root.ListingSearch=api;
})(globalThis);
