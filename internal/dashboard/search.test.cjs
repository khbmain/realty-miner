const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const S=require('./search.js');
const queries=['баянмонгол 2өрөө','bayanmongol 2 uruu','баян-монгол 2 өрөө','2 өрөө баян-монгол'];
for(const title of ['Баянмонгол хотхонд 2 өрөө зарна','2 өрөөтэй Баян-Монгол','Bayan Mongol 2 uruu','Баян Монгол 2өрөө']){
  for(const q of queries)assert.equal(S.compile(q)({title}),true,`${q}: ${title}`);
}
for(const title of ['Баянмонгол 12 өрөө','Баянмонгол 3 өрөө, 2 давхар','Баянмонгол 2 давхар']){
  for(const q of queries)assert.equal(S.compile(q)({title}),false,title);
}
assert.equal(S.compile('bayn mongol 2 uruu')({title:'Баян Монгол 2 өрөө'}),true);
assert.equal(S.compile('баянмонгло')({title:'Баянмонгол'}),true);
assert.equal(S.compile('khanuul')({location:'Хан-Уул',title:'Байр'}),true);
assert.equal(S.compile('99112233')({phones:['+97699112233']}),true);
assert.equal(S.compile('99112234')({phones:['+97699112233']}),false);
assert.equal(S.compile('123456')({external_id:'123456'}),true);
assert.equal(S.compile('')({}),true);
assert.equal(S.compile('2')({title:'12 өрөө',external_id:'222222'}),false);
assert.equal(S.compile('баянмонгол 2 өрөө')({title:'Нарны хороолол 2 өрөө'}),false);
const html=fs.readFileSync(__dirname+'/index.html','utf8');
assert.ok(html.includes('<script src="/search.js"></script>'));
assert.ok(html.includes('searchMatches(r)'));
new vm.Script(html.match(/<script>([\s\S]*?)<\/script>/)[1]);
console.log('Smart search: spelling variants, room numbers, typos, identifiers and integration passed');
