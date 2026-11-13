import { test } from 'node:test';
import assert from 'node:assert/strict';
import ts from 'typescript';
import { readFileSync } from 'node:fs';
const code = ts.transpileModule(readFileSync(new URL('../src/lib/api.ts', import.meta.url), 'utf8'), { compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2020} }).outputText;
const api = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
const reply = (data,status=200) => new Response(JSON.stringify(data),{status,headers:{'Content-Type':'application/json'}});
test('maps protobuf chat response and proposal without approving it',()=>{
  const m=api.messageFromApi({id:'7',role:'MESSAGE_ROLE_ASSISTANT',text:'Ответ',createdAt:'2026-11-13T10:00:00Z',sources:[{documentId:'doc',title:'Инструкция',url:'https://example.com'}],proposal:{id:'8',resourceId:'wiki',resourceLabel:'Wiki',accessLevel:'read'}});
  assert.equal(m.proposal.status,'draft');assert.equal(m.sources[0].id,'doc');assert.equal(m.proposal.proposal_id,'8');
});
test('sends numeric idempotency key unchanged and reads assistantMessage',async()=>{
  globalThis.fetch=async(path,init)=>{assert.equal(path,'/v1/chat/messages');assert.deepEqual(JSON.parse(init.body),{text:'Привет',idempotencyKey:'123'});return reply({assistantMessage:{id:'2',text:'Ответ'}});};
  assert.equal((await api.sendChatMessage('Привет','123')).text,'Ответ');
});
test('server errors are propagated instead of fabricated success',async()=>{
  globalThis.fetch=async()=>reply({message:'AI unavailable'},503);
  await assert.rejects(api.sendChatMessage('Вопрос','4'),/AI unavailable/);
  await assert.rejects(api.confirmAccessProposal('7'),/AI unavailable/);
});
test('preserves server step completion and snake_case resource ID',async()=>{
  globalThis.fetch=async()=>reply({steps:[{id:'a',status:'completed',resource_id:'wiki',period:'2 месяц'},{id:'b',status:'pending'}]});
  const steps=await api.fetchOnboardingPlan('1');assert.equal(steps[0].completed,true);assert.equal(steps[0].resource_id,'wiki');assert.equal(steps[1].current,true);
});
test('assigns a missing plan through existing endpoint',async()=>{
  const calls=[];globalThis.fetch=async(path,init)=>{calls.push([path,init.method]);return calls.length===1?reply({},404):reply({steps:[]});};
  await api.fetchOnboardingPlan('9');assert.deepEqual(calls,[['/v1/users/9/plan',undefined],['/v1/users/9/plan','POST']]);
});
test('reopens a step using PATCH and pending status',async()=>{
  globalThis.fetch=async(path,init)=>{assert.equal(path,'/v1/step/9/a/completion');assert.equal(init.method,'PATCH');assert.deepEqual(JSON.parse(init.body),{status:'pending'});return reply({});};
  await api.toggleStep('9',{step_id:'a',completed:true});
});
test('refreshes once and retries unauthorized request with new bearer token',async()=>{
  let calls=0;globalThis.fetch=async(path,init)=>{calls++;if(calls===1)return reply({},401);if(calls===2){assert.equal(path,'/v1/auth/refresh');return reply({access_token:'fresh'});}assert.equal(init.headers.get('Authorization'),'Bearer fresh');return reply({messages:[],nextPageToken:'next'});};
  assert.equal((await api.fetchHistory()).next,'next');assert.equal(calls,3);
});
