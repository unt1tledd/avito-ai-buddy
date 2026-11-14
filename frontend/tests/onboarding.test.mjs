import test, { afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import ts from 'typescript';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

const require = createRequire(import.meta.url);
function load(file, overrides = {}) {
  const source = readFileSync(new URL(`../src/${file}`, import.meta.url), 'utf8');
  const { outputText } = ts.transpileModule(source, { compilerOptions: {
    module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020,
    jsx: ts.JsxEmit.React, esModuleInterop: true,
  }});
  const module = { exports: {} };
  new Function('require', 'module', 'exports', outputText)(id => overrides[id] || require(id), module, module.exports);
  return module.exports;
}
const api = load('lib/api.ts');
const { OnboardingPage } = load('pages/OnboardingPage.tsx', {'../lib/api': api});
const { SetupPage } = load('pages/SetupPage.tsx', {'../lib/api': api});
const originalFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = originalFetch; });
const step = { id:'git',title:'Доступ к Git',description:'Запроси доступ',period:'1 неделя',resource_id:'repo/team',action_type:'access_request',status:'pending' };
const render = props => renderToStaticMarkup(React.createElement(OnboardingPage, {
  steps:[],isClosing:false,onClose(){},onToggleStep(){},onSelectStepQuestion(){},onRetry(){},...props,
}));

test('404 leaves roadmap unassigned and never calls POST', async () => {
  const calls = [];
  globalThis.fetch = async (path, init) => { calls.push([path,init.method || 'GET']); return Response.json({}, {status:404}); };
  assert.deepEqual(await api.fetchOnboardingPlan('123'), []);
  assert.deepEqual(calls, [['/v1/users/123/plan','GET']]);
});
test('server failure remains an error rather than an empty roadmap', async () => {
  globalThis.fetch = async () => Response.json({message:'Unavailable'}, {status:503});
  await assert.rejects(api.fetchOnboardingPlan('123'), err => err.status === 503);
});
test('assigned steps preserve order, completion and action type', async () => {
  globalThis.fetch = async () => Response.json({steps:[{...step,id:'intro',status:'completed'},step,{...step,id:'later'}]});
  const steps = await api.fetchOnboardingPlan('123');
  assert.deepEqual(steps.map(s => [s.step_id,s.completed,s.current]), [['intro',true,false],['git',false,true],['later',false,false]]);
  assert.equal(steps[1].action_type,'access_request');
  assert.match(api.stepQuestion(steps[1]), /Доступ к Git.*repo\/team/);
});
test('confirm queues proposal without marking a step completed', async () => {
  const calls = [];
  globalThis.fetch = async (path, init) => { calls.push([path,init.method]); return Response.json({accessRequest:{status:'pending_dispatch'}}); };
  assert.equal((await api.confirmAccessProposal('17')).status,'pending_dispatch');
  assert.deepEqual(calls,[['/v1/chat/proposals/17/confirm','POST']]);
  assert.equal(api.statusLabel('pending_dispatch'),'В очереди');
  assert.match(api.statusLabel('pending'),/Заявка отправлена/);
  assert.match(api.statusLabel('approved'),/одобрен/);
});
test('unassigned, loading and error states do not show empty progress', () => {
  assert.match(render({}), /Твой путь скоро появится/);
  assert.doesNotMatch(render({}), /Выполнено|0%|Обновить Roadmap/);
  assert.match(render({loading:true}), /Загружаем Roadmap/);
  assert.doesNotMatch(render({loading:true}), /Твой путь скоро появится/);
  assert.match(render({error:'Нет связи'}), /Нет связи/);
  assert.doesNotMatch(render({error:'Нет связи'}), /Твой путь скоро появится/);
});
test('access action is available for an incomplete non-current step', () => {
  const html = render({steps:[{step_id:'git',step_number:2,title:step.title,description:'',period:step.period,completed:false,current:false,action_type:'access_request'}]});
  assert.match(html,/Спросить у AI-buddy/);
  assert.doesNotMatch(html,/Отправить заявку/);
});
test('setup has no self-assignment selector', () => {
  const html = renderToStaticMarkup(React.createElement(SetupPage,{userId:'123',onDone(){}}));
  assert.doesNotMatch(html,/name="track"|План адаптации/);
  assert.match(html,/Треки адаптации назначит бадди/);
  assert.doesNotMatch(html,/checkbox|тестов|6 месяцев/);
  assert.match(html,/name="start_date"/);
  assert.match(html,/name="end_date"/);
});

const { AccessProposalCard } = load('pages/AccessProposalCard.tsx', {'../lib/api':api});
const proposal = {proposal_id:'17',title:'Заявка',resource_id:'repo/team',resource_label:'Репозиторий команды',access_level:'Developer',status:'draft'};
const card = (status, extras={}) => renderToStaticMarkup(React.createElement(AccessProposalCard, {proposal:{...proposal,status},sending:false,onConfirm(){},...extras}));

test('chat sends original question and numeric key to the selected conversation', async()=>{
  globalThis.fetch=async(path,init)=>{
    assert.equal(path,'/v1/chat/messages');assert.equal(init.method,'POST');
    assert.equal(init.headers.get('X-Conversation-Id'),'23');
    assert.deepEqual(JSON.parse(init.body),{text:'Нужен доступ к Git',idempotencyKey:'123456'});
    return Response.json({assistantMessage:{id:'9',role:'MESSAGE_ROLE_ASSISTANT',text:'Подготовил заявку',proposal:{id:'17',resourceId:'repo/team',resourceLabel:'Репозиторий команды',accessLevel:'Developer'}}});
  };
  const result=await api.sendChatMessage('Нужен доступ к Git','123456','23');
  assert.equal(result.proposal.proposal_id,'17');assert.equal(result.proposal.status,'draft');
  assert.equal(result.proposal.resource_id,'repo/team');
});
test('plain AI reply has no fabricated proposal',()=>{
  assert.equal(api.messageFromApi({id:'10',role:'MESSAGE_ROLE_ASSISTANT',text:'Ответ'}).proposal,undefined);
});
test('draft card shows resource and submit action, pending card has no submit action or spinner',()=>{
  assert.match(card('draft'),/repo\/team/);assert.match(card('draft'),/Developer/);
  assert.match(card('draft'),/Отправить заявку/);
  const queued=card('pending_dispatch');
  assert.match(queued,/В очереди/);assert.match(queued,/proposal-status--waiting/);
  assert.doesNotMatch(queued,/Отправить заявку|proposal-spinner|одобрен/);
});
test('spinner exists only during confirmation HTTP request',()=>{
  const sending=card('draft',{sending:true});
  assert.match(sending,/disabled/);assert.match(sending,/proposal-spinner/);
  assert.match(sending,/Отправляем/);assert.doesNotMatch(card('draft'),/proposal-spinner/);
});
test('confirmation failure propagates and never becomes fake approval',async()=>{
  globalThis.fetch=async()=>Response.json({message:'Недоступно'},{status:503});
  await assert.rejects(api.confirmAccessProposal('17'),err=>err.status===503);
  globalThis.fetch=async()=>Response.json({accessRequest:{}});
  await assert.rejects(api.confirmAccessProposal('17'),/не вернул результат/);
  assert.match(card('draft',{error:'Недоступно'}),/role="alert"/);
});
test('history preserves server result messages and proposal dispatch status',async()=>{
  globalThis.fetch=async(path,init)=>{
    assert.equal(init.headers.get('X-Conversation-Id'),'23');
    return Response.json({messages:[{id:'9',role:'MESSAGE_ROLE_ASSISTANT',text:'Заявка',proposal:{id:'17',resourceId:'repo/team',resourceLabel:'Git',accessRequest:{status:'pending_dispatch'}}},{id:'10',role:'MESSAGE_ROLE_ASSISTANT',text:'Доступ подтверждён'}]});
  };
  const result=await api.fetchHistory('','23');
  assert.equal(result.messages[1].text,'Доступ подтверждён');
  assert.equal(result.messages[0].proposal.status,'pending_dispatch');
});
