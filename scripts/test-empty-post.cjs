// 使用Axios真实浏览器XHR适配器检查请求头；伪XHR不产生网络请求。
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
let captured;
class FakeXHR {
 constructor(){this.headers={};this.onloadend=null;}
 open(method,url){this.method=method;this.url=url;}
 setRequestHeader(k,v){this.headers[k.toLowerCase()]=v;}
 getAllResponseHeaders(){return 'content-type: application/json\r\n';}
 send(data){captured={method:this.method,url:this.url,headers:this.headers,data};this.status=200;this.statusText='OK';this.responseText='{"errorNo":0}';queueMicrotask(()=>this.onloadend());}
}
global.XMLHttpRequest=FakeXHR;
const root=path.resolve(__dirname,'..');
const axios=require(root+'/fe/node_modules/axios/dist/browser/axios.cjs');
const http=axios.create({adapter:'xhr',headers:{'Content-Type':'application/json;charset=UTF-8;'}});
const source=fs.readFileSync(root+'/fe/src/utils/axios.js','utf8');
const start=source.indexOf('http.interceptors.request.use('),end=source.indexOf('//响应拦截器');
new Function('http',source.slice(start,end))(http);
(async()=>{
 for(const [method,data] of [['post',undefined],['post',{}],['post',{tag:'inbox'}],['POST',undefined]]){
  await http.request({url:'https://mail.example.com:8443/api/group/list',method,data});
  assert.match(captured.headers['content-type']||'',/^application\/json/,'POST must retain JSON content type');
  assert.deepEqual(JSON.parse(captured.data),data===undefined?{}:data);
 }
 await http.get('https://mail.example.com:8443/api/group');assert.equal(captured.data,null);
 console.log('PASS: browser Axios adapter sends JSON for empty/lowercase/uppercase POST; payload and GET preserved');
})().catch(e=>{console.error(e.message);process.exitCode=1;});
