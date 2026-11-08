const assert=require('node:assert/strict');const A=require('./analysis.js');
assert.equal(A.quantile([10,20,30,40],.5),25);assert.equal(A.quantile([],.5),null);
assert.deepEqual(A.prices([{price:0,currency:'MNT'},{price:2,currency:'USD'},{price:30,currency:'MNT'}]),[30]);
const r={title:'2 өрөө орон сууц',transaction_type:'sale',currency:'MNT',price:200e6,attributes:{Талбай:'50 м²'},phones:['+97699112233','99112233']};
assert.equal(A.perArea(r),4e6);assert.equal(A.perArea({...r,transaction_type:'rent'}),null);assert.equal(A.perArea({...r,attributes:{Талбай:'1 га'}}),null);assert.equal(A.perArea({...r,price:1000}),null);
assert.equal(A.groups([r,{...r,external_id:'2'}])[0][1].length,2);assert.equal(A.apartment({title:'3 өрөө хаус'}),false);
console.log('Analysis calculations passed');
