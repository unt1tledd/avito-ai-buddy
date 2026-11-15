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
const { OnboardingPage, nextRoadmapStep } = load('pages/OnboardingPage.tsx', {'../lib/api': api});
const { SetupPage } = load('pages/SetupPage.tsx', {'../lib/api': api});
const originalFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = originalFetch; });
const step = { id:'git',title:'Доступ к Git',description:'Запроси доступ',period:'1 неделя',resource_id:'repo/team',action_type:'access_request',status:'pending' };
const unassigned = {assigned:false,steps:[],tracks:[],groupingAvailable:false};
const render = props => renderToStaticMarkup(React.createElement(OnboardingPage, {
  data:unassigned,userId:'123',onToggleStep(){},onSelectStepQuestion(){},onRetry(){},...props,
}));
function withSelection(selection,run) {
  const previous=Object.getOwnPropertyDescriptor(globalThis,'localStorage');
  Object.defineProperty(globalThis,'localStorage',{configurable:true,value:{getItem:()=>JSON.stringify(selection),setItem(){}}});
  try { return run(); } finally {
    if(previous)Object.defineProperty(globalThis,'localStorage',previous);else delete globalThis.localStorage;
  }
}

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
test('flat steps preserve order and completion without guessing a current step', async () => {
  globalThis.fetch = async () => Response.json({steps:[{...step,id:'intro',status:'completed'},step,{...step,id:'later'}]});
  const steps = await api.fetchOnboardingPlan('123');
  assert.deepEqual(steps.map(s => [s.step_id,s.completed,s.current]), [['intro',true,false],['git',false,false],['later',false,false]]);
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
  assert.match(render({data:null,loading:true}), /Загружаем Roadmap/);
  assert.doesNotMatch(render({data:null,loading:true}), /Твой путь скоро появится/);
  assert.match(render({data:null,error:'Нет связи'}), /Нет связи/);
  assert.doesNotMatch(render({data:null,error:'Нет связи'}), /Твой путь скоро появится/);
});
test('access action is available for an incomplete non-current step', () => {
  const html = withSelection({track:'common',step:'git',view:'stages'},()=>render({data:{assigned:true,steps:[{step_id:'git',step_number:2,title:step.title,description:'',period:step.period,completed:false,current:false,track:'common',action_type:'access_request'}],tracks:[{track:'common',display_name:'Общий онбординг'}],groupingAvailable:true}}));
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
  assert.match(queued,/<h4>Заявка принята<\/h4>/);assert.match(queued,/в очереди на отправку/);
  assert.match(queued,/proposal-card--waiting/);
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

const { registrationError } = load('pages/AuthForm.tsx');
test('registration rejects mismatched confirmation and preserves password whitespace',()=>{
  assert.match(registrationError('password123','password124'),/не совпадают/);
  assert.match(registrationError(' password123','password123'),/не совпадают/);
  assert.equal(registrationError(' password123 ',' password123 '),'');
});
test('registration follows server character and UTF-8 byte length constraints',()=>{
  assert.match(registrationError('1234567','1234567'),/8 символов/);
  assert.equal(registrationError('12345678','12345678'),'');
  assert.equal(registrationError('я'.repeat(36),'я'.repeat(36)),'');
  assert.match(registrationError('я'.repeat(37),'я'.repeat(37)),/72 байта/);
  assert.match(registrationError('😀'.repeat(7),'😀'.repeat(7)),/8 символов/);
});

const { shouldRefreshHistory, historyRefreshQueue, reconcileProposal, mergeChatMessages } = load('lib/chatEvents.ts');
test('live history ignores malformed, unknown and other-conversation events',()=>{
  assert.equal(shouldRefreshHistory('not JSON','12'),false);
  assert.equal(shouldRefreshHistory('null','12'),false);
  assert.equal(shouldRefreshHistory(JSON.stringify({type:'ping'}),'12'),false);
  const message=JSON.stringify({type:'message.created',message:{ConversationID:12}});
  assert.equal(shouldRefreshHistory(message,'12'),true);
  assert.equal(shouldRefreshHistory(message,'13'),false);
  assert.equal(shouldRefreshHistory(message,''),false);
  assert.equal(shouldRefreshHistory(JSON.stringify({type:'message.created'}),'12'),false);
  for(const type of ['task.status_changed','access_request.failed'])
    assert.equal(shouldRefreshHistory(JSON.stringify({type}),'12'),true);
});
test('live refresh coalesces bursts, serializes requests and cancels on unmount',async()=>{
  let calls=0,finish;
  const queue=historyRefreshQueue(()=>{calls++;return new Promise(resolve=>{finish=resolve;});},1);
  const tick=()=>new Promise(resolve=>setTimeout(resolve,15));
  queue.schedule();queue.schedule();queue.schedule();
  await tick();assert.equal(calls,1);
  queue.schedule();queue.schedule();
  await tick();assert.equal(calls,1);
  finish();await tick();assert.equal(calls,2);
  queue.schedule();queue.stop();finish();
  await tick();assert.equal(calls,2);
  queue.schedule();await tick();assert.equal(calls,2);
});


test('final access decisions survive delayed confirmation and history responses',()=>{
  const message = status => api.messageFromApi({id:'9',role:'MESSAGE_ROLE_ASSISTANT',text:'Заявка',createdAt:'2026-11-15T10:00:00Z',
    proposal:{id:'17',resourceId:'repo/team',resourceLabel:'Git',accessLevel:'Developer',accessRequest:{status,taskId:'23'}}});
  for(const final of ['approved','rejected']) {
    for(const stale of ['draft','pending_dispatch','pending','pending_approval','failed']) {
      assert.equal(reconcileProposal(message(final).proposal,message(stale).proposal).status,final);
      assert.equal(mergeChatMessages([message(final)],[message(stale)])[0].proposal.status,final);
    }
    assert.equal(mergeChatMessages([message('pending_dispatch')],[message(final)])[0].proposal.status,final);
    assert.equal(mergeChatMessages([],[message(final)])[0].proposal.status,final);
  }
  assert.equal(reconcileProposal({...proposal,status:'failed'},{...proposal,status:'pending_dispatch'}).status,'pending_dispatch');
  assert.equal(reconcileProposal({...proposal,status:'pending_dispatch'},{...proposal,status:'draft'}).status,'pending_dispatch');
});
test('final access cards show the server decision without a waiting spinner or resend button',()=>{
  const approved=card('approved');
  assert.match(approved,/proposal-card--approved/);assert.match(approved,/<h4>Доступ одобрен<\/h4>/);
  const rejected=card('rejected');
  assert.match(rejected,/proposal-card--failed/);assert.match(rejected,/<h4>В доступе отказано<\/h4>/);
  for(const html of [approved,rejected])assert.doesNotMatch(html,/proposal-card--waiting|proposal-spinner|proposal-submit/);
});


test('roadmap groups assigned steps and uses only server current steps per track',async()=>{
  const calls=[];
  globalThis.fetch=async(path,init)=>{
    calls.push([path,init.method || 'GET']);
    return Response.json(path.endsWith('/plan_context')?{tracks:[
      {track:'common',display_name:'Общий онбординг',current_step:{id:'later'}},
      {track:'backend',display_name:'Backend'},
    ]}:{id:'1',steps:[{...step,id:'first',track:'common'},{...step,id:'later',track:'common'},{...step,id:'backend',track:'backend',status:'completed'}]});
  };
  const data=await api.fetchOnboardingRoadmap('123');
  assert.equal(data.assigned,true);assert.equal(data.groupingAvailable,true);
  assert.deepEqual(data.steps.map(item=>[item.step_id,item.track,item.current]),[['first','common',false],['later','common',true],['backend','backend',false]]);
  assert.deepEqual(data.tracks.map(item=>[item.display_name,item.current_step_id]),[['Общий онбординг','later'],['Backend',undefined]]);
  assert.deepEqual(calls,[['/v1/users/123/plan','GET'],['/v1/users/123/plan_context','GET']]);
});
test('roadmap never assigns a missing plan and does not mask a context failure',async()=>{
  const calls=[];
  globalThis.fetch=async(path,init)=>{calls.push([path,init.method||'GET']);return Response.json({},{status:404});};
  assert.deepEqual(await api.fetchOnboardingRoadmap('123'),unassigned);
  assert.deepEqual(calls,[['/v1/users/123/plan','GET']]);
  globalThis.fetch=async path=>path.endsWith('/plan_context')?Response.json({message:'Контекст недоступен'},{status:503}):Response.json({steps:[{...step,track:'common'}]});
  await assert.rejects(api.fetchOnboardingRoadmap('123'),error=>error.status===503);
});
test('legacy roadmap keeps all steps without inventing track membership',async()=>{
  globalThis.fetch=async path=>Response.json(path.endsWith('/plan_context')?{tracks:[{track:'common',display_name:'Общая адаптация',current_step:{id:'git'}}]}:{steps:[step,{...step,id:'unknown'}]});
  const data=await api.fetchOnboardingRoadmap('123');
  assert.equal(data.groupingAvailable,false);assert.equal(data.steps.length,2);
  assert.ok(data.steps.every(item=>item.track===undefined));
  assert.equal(data.steps[0].current,true);assert.equal(data.steps[1].current,false);
});
test('roadmap renders selected-track progress and retains completed tracks',()=>{
  const data={assigned:true,groupingAvailable:true,tracks:[{track:'common',display_name:'Общий онбординг'},{track:'backend',display_name:'Backend'}],steps:[
    {step_id:'a',step_number:1,track:'common',title:'Знакомство',description:'',period:'1 неделя',completed:true},
    {step_id:'b',step_number:2,track:'common',title:'Доступ',description:'',period:'2 неделя',completed:false,current:true},
    {step_id:'c',step_number:3,track:'backend',title:'Технический шаг',description:'',period:'1 неделя',completed:true},
  ]};
  const common=withSelection({track:'common',step:'',view:'stages'},()=>render({data}));
  assert.match(common,/2 трека/);assert.match(common,/1 из 2 шагов выполнено/);assert.match(common,/50%/);
  assert.doesNotMatch(common,/Технический шаг/);
  const backend=withSelection({track:'backend',step:'',view:'stages'},()=>render({data}));
  assert.match(backend,/1 из 1 шагов выполнено/);assert.match(backend,/100%/);assert.match(backend,/Все шаги выполнены/);
});
test('step completion and reopening use existing endpoints without assigning a plan',async()=>{
  const calls=[];
  globalThis.fetch=async(path,init)=>{calls.push([path,init.method,init.body && JSON.parse(init.body)]);return Response.json({});};
  await api.toggleStep('123',{step_id:'git/team',completed:false});
  await api.toggleStep('123',{step_id:'git/team',completed:true});
  assert.deepEqual(calls,[['/v1/step/123/git%2Fteam/completion','PUT',undefined],['/v1/step/123/git%2Fteam/completion','PATCH',{status:'pending'}]]);
});


test('roadmap uses accessible check circles and names simultaneous current tracks',()=>{
  const data={assigned:true,groupingAvailable:false,assignedTrack:'backend',tracks:[
    {track:'common',display_name:'Общая адаптация',current_step_id:'a'},
    {track:'backend',display_name:'Backend',current_step_id:'b'},
  ],steps:[
    {step_id:'a',step_number:1,title:'Команда',description:'',period:'1 неделя',completed:false,current:true,current_track_name:'Общая адаптация'},
    {step_id:'b',step_number:2,title:'Окружение',description:'',period:'1 неделя',completed:false,current:true,current_track_name:'Backend'},
    {step_id:'c',step_number:3,title:'Знакомство',description:'',period:'1 неделя',completed:true},
  ]};
  const html=render({data});
  assert.equal((html.match(/role="checkbox"/g)||[]).length,3);
  assert.equal((html.match(/aria-checked="false"/g)||[]).length,2);
  assert.equal((html.match(/aria-checked="true"/g)||[]).length,1);
  assert.match(html,/Текущий шаг · Общая адаптация/);assert.match(html,/Текущий шаг · Backend/);
  assert.match(html,/Назначенное направление: Backend/);
  assert.match(html,/Снять отметку: Знакомство/);assert.doesNotMatch(html,/roadmap-complete/);
});
test('completing the selected current step advances only within its server track',()=>{
  const before={assigned:true,groupingAvailable:false,tracks:[
    {track:'common',display_name:'Общий',current_step_id:'a'},
    {track:'backend',display_name:'Backend',current_step_id:'b'},
  ],steps:[{step_id:'a',completed:false},{step_id:'b',completed:false},{step_id:'next-common',completed:false},{step_id:'next-backend',completed:false}]};
  const after={...before,steps:before.steps.map(step=>({...step,completed:step.step_id==='b'})),tracks:[before.tracks[0],{track:'backend',display_name:'Backend',current_step_id:'next-backend'}]};
  assert.equal(nextRoadmapStep(before,after,'b'),'next-backend');
  assert.equal(nextRoadmapStep(before,after,'a'),'a');
  assert.equal(nextRoadmapStep(after,before,'b'),'b');
  assert.equal(nextRoadmapStep(before,{...after,tracks:[]},'b'),'b');
  assert.equal(nextRoadmapStep(before,{...after,tracks:[{track:'backend',current_step_id:'missing'}]},'b'),'b');
});
