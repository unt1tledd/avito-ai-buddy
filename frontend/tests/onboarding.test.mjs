import test, { beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { load } from './load-source.mjs';

// Deterministic browser storage; never rely on Node's experimental localStorage.
const storage = new Map();
Object.defineProperty(globalThis,'localStorage',{configurable:true,value:{
  getItem:key=>storage.get(key) ?? null,
  setItem:(key,value)=>storage.set(key,String(value)),
  removeItem:key=>storage.delete(key),
}});
beforeEach(()=>{
  storage.clear();
  globalThis.fetch=async()=>{throw new Error('Unexpected HTTP request: install a fetch mock in this test');};
});
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
  assert.match(render({}), /Твой путь скоро начнётся/);
  assert.doesNotMatch(render({}), /Выполнено|0%|Обновить Roadmap/);
  assert.match(render({data:null,loading:true}), /Загружаем Roadmap/);
  assert.doesNotMatch(render({data:null,loading:true}), /Твой путь скоро начнётся/);
  assert.match(render({data:null,error:'Нет связи'}), /Нет связи/);
  assert.doesNotMatch(render({data:null,error:'Нет связи'}), /Твой путь скоро начнётся/);
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

const messageContent = load('pages/MessageContent.tsx');
const { AccessProposalCard } = load('pages/AccessProposalCard.tsx', {'../lib/api':api,'./MessageContent':messageContent});
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
test('live refresh coalesces bursts, serializes requests and cancels on unmount',async(t)=>{
  t.mock.timers.enable({apis:['setTimeout']});
  let calls=0,finish;
  const queue=historyRefreshQueue(()=>{calls++;return new Promise(resolve=>{finish=resolve;});},150);
  t.after(()=>queue.stop());
  queue.schedule();queue.schedule();queue.schedule();
  t.mock.timers.tick(149);assert.equal(calls,0);
  t.mock.timers.tick(1);assert.equal(calls,1);
  queue.schedule();queue.schedule();
  t.mock.timers.tick(1000);assert.equal(calls,1);
  finish();await Promise.resolve();
  t.mock.timers.tick(150);assert.equal(calls,2);
  queue.schedule();queue.stop();finish();await Promise.resolve();
  t.mock.timers.tick(1000);assert.equal(calls,2);
  queue.schedule();t.mock.timers.tick(1000);assert.equal(calls,2);
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
  assert.doesNotMatch(common,/2 трека|Обновить Roadmap|Выбранный трек/);assert.match(common,/1 из 2 шагов выполнено/);assert.match(common,/50%/);
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
  assert.doesNotMatch(html,/Назначенное направление:|Выбранный трек|Обновить Roadmap/);
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


test('roadmap renders one switch button per assigned step track, with no fabricated tracks',async()=>{
  const calls=[];
  globalThis.fetch=async(path,init)=>{
    calls.push([path,init.method||'GET']);
    return Response.json(path.endsWith('/plan_context')?{tracks:[
      {track:'common',display_name:'Общий онбординг',current_step:{id:'common-1'}},
      {track:'backend',display_name:'Backend',current_step:{id:'backend-1'}},
      {track:'security',display_name:'Безопасность',current_step:{id:'security-1'}},
      {track:'not-assigned',display_name:'Не назначен'},
    ]}:{id:'1',steps:['common','backend','security'].map(track=>({...step,id:track+'-1',track,title:'Шаг '+track}))});
  };
  const data=await api.fetchOnboardingRoadmap('123');
  assert.deepEqual(data.tracks.map(track=>track.track),['common','backend','security']);
  for(const track of data.tracks) {
    const html=withSelection({track:track.track,step:'',view:'stages'},()=>render({data}));
    const switches=html.match(/<div class="roadmap-track-buttons"[^>]*>(.*?)<\/div>/s)[1];
    assert.equal((switches.match(/<button/g)||[]).length,3);
    assert.equal((switches.match(/aria-pressed="true"/g)||[]).length,1);
    assert.doesNotMatch(html,/<select|Не назначен/);
    assert.match(html,new RegExp('Шаг '+track.track));
    for(const other of data.tracks.filter(item=>item.track!==track.track))assert.doesNotMatch(html,new RegExp('Шаг '+other.track));
  }
  // Switching the rendered selection requires no extra API requests.
  assert.deepEqual(calls,[['/v1/users/123/plan','GET'],['/v1/users/123/plan_context','GET']]);
});


test('assistant display removes service citations and localizes only task status notices',()=>{
  assert.equal(messageContent.assistantDisplayText('Готово. [source:synth-repository-access]'),'Готово.');
  assert.equal(messageContent.assistantDisplayText('Ответ [source:one], продолжение [source:two].'),'Ответ, продолжение.');
  assert.equal(messageContent.assistantDisplayText('Статус задачи 2 изменён: approved.'),'Заявка №2: одобрено.');
  assert.equal(messageContent.assistantDisplayText('Статус задачи 3 изменён: rejected.'),'Заявка №3: отклонено.');
  assert.equal(messageContent.assistantDisplayText('Значение approved в примере [важно]'),'Значение approved в примере [важно]');
  const user=renderToStaticMarkup(React.createElement(messageContent.MessageText,{text:'[source:example] approved'}));
  assert.match(user,/\[source:example\] approved/);
});
test('assistant links are clickable and escaped; citation markers stay hidden',()=>{
  const html=renderToStaticMarkup(React.createElement(messageContent.MessageText,{assistant:true,text:'Открой https://people.example/absence. Или [инструкцию](https://docs.example/guide). [source:demo] <script>alert(1)</script>'}));
  assert.match(html,/href="https:\/\/people.example\/absence"/);
  assert.match(html,/href="https:\/\/docs.example\/guide"/);
  assert.match(html,/>инструкцию<\/a>/);assert.doesNotMatch(html,/source:demo|<script>/);
  assert.match(html,/&lt;script&gt;/);
});
test('source cards retain demo labels and support materials without a safe URL',()=>{
  const html=renderToStaticMarkup(React.createElement(messageContent.MessageSources,{sources:[
    {id:'1',title:'[ДЕМО] Документ',url:'https://docs.example/guide'},
    {id:'2',title:'Материал без ссылки',url:''},
    {id:'3',title:'Небезопасная ссылка',url:'javascript:alert(1)'},
  ]}));
  assert.match(html,/message-source-card/);assert.match(html,/\[ДЕМО\] Документ/);
  assert.match(html,/Материал без ссылки/);assert.equal((html.match(/<a /g)||[]).length,1);
  assert.doesNotMatch(html,/href="javascript:/);
});

test('requests include old paginated proposals and retain the newest status per conversation', async () => {
  const calls=[];
  globalThis.fetch=async(path,init)=>{
    calls.push([path,init.headers.get('X-Conversation-Id')]);
    const old=new URL(path,'http://localhost').searchParams.get('pageToken')==='older';
    const proposal={id:'access',resourceId:'git',accessRequest:{status:old?'pending_approval':'approved'}};
    return Response.json({messages:[{id:old?'old':'new',role:'MESSAGE_ROLE_ASSISTANT',text:'',createdAt:old?'2026-11-14T10:00:00Z':'2026-11-15T10:00:00Z',proposal}],nextPageToken:old?'':'older'});
  };
  const result=await api.fetchConversationProposals([{id:'chat-1',title:'Git',createdAt:''}]);
  assert.equal(result.failed,0);
  assert.equal(result.items.length,1);
  assert.equal(result.items[0].proposal.status,'approved');
  assert.equal(result.items[0].conversationId,'chat-1');
  assert.equal(calls.length,2);
  assert.ok(calls.every(([,id])=>id==='chat-1'));
  assert.equal(new URL(calls[1][0],'http://localhost').searchParams.get('pageToken'),'older');
});

test('requests report unavailable conversations rather than claiming there are no requests', async () => {
  globalThis.fetch=async()=>Response.json({message:'Unavailable'},{status:503});
  const result=await api.fetchConversationProposals([{id:'chat-1',title:'Git',createdAt:''}]);
  assert.equal(result.failed,1);
  assert.deepEqual(result.items,[]);
});

test('home roadmap preview finds the next unfinished step only within the current track', () => {
  const {nextSummaryStep}=load('pages/RoadmapSummaryDialog.tsx');
  const current={step_id:'a',track:'tech',current:true,completed:false};
  const other={step_id:'b',track:'hr',completed:false};
  const done={step_id:'c',track:'tech',completed:true};
  const next={step_id:'d',track:'tech',completed:false};
  const data={assigned:true,groupingAvailable:true,tracks:[],steps:[current,other,done,next]};
  assert.equal(nextSummaryStep(data,current),next);
  assert.equal(nextSummaryStep({...data,groupingAvailable:false},current),undefined);
  assert.equal(nextSummaryStep({...data,steps:[current,other,done]},current),undefined);
});

test('roadmap dialog preserves loaded steps and empty state during refresh', () => {
  const {RoadmapSummaryDialog}=load('pages/RoadmapSummaryDialog.tsx');
  const renderDialog=data=>renderToStaticMarkup(React.createElement(RoadmapSummaryDialog,{data,loading:true,error:false,onRetry(){},onClose(){},onOpen(){}}));
  const html=renderDialog({assigned:true,groupingAvailable:true,tracks:[],steps:[{step_id:'git',track:'tech',title:'Получить доступ к Git',current:true,completed:false}]});
  assert.match(html,/Получить доступ к Git/);
  assert.doesNotMatch(html,/Загружаем план адаптации/);
  const empty=renderDialog(unassigned);
  assert.match(empty,/Твой путь скоро начнётся/);
  assert.doesNotMatch(empty,/Загружаем план адаптации/);
});

test('requests keep successful chats when another history fails and detect repeated page tokens', async()=>{
  const calls=[];
  globalThis.fetch=async(path,init)=>{
    const id=init.headers.get('X-Conversation-Id');calls.push(id);
    if(id==='broken')return Response.json({}, {status:503});
    if(id==='loop')return Response.json({messages:[],nextPageToken:'same'});
    return Response.json({messages:[{id:'m',createdAt:'2026-11-15',proposal:{id:'p',accessRequest:{status:'pending_approval'}}}]});
  };
  const result=await api.fetchConversationProposals(['good','broken','loop'].map(id=>({id,title:id,createdAt:''})));
  assert.equal(result.failed,2);
  assert.deepEqual(result.items.map(item=>item.conversationId),['good']);
  assert.equal(calls.filter(id=>id==='loop').length,2);
});

test('requests limit parallel history reads to four and do not merge proposals across chats', async()=>{
  let active=0,peak=0;
  globalThis.fetch=async()=>{
    active++;peak=Math.max(peak,active);
    await new Promise(resolve=>setImmediate(resolve));active--;
    return Response.json({messages:[{id:'m',createdAt:'2026-11-15',proposal:{id:'same',accessRequest:{status:'pending_approval'}}}]});
  };
  const result=await api.fetchConversationProposals(Array.from({length:9},(_,i)=>({id:String(i),title:'Chat',createdAt:''})));
  assert.equal(result.failed,0);assert.equal(result.items.length,9);
  assert.equal(new Set(result.items.map(item=>item.conversationId)).size,9);
  assert.equal(peak,4);
});

test('empty conversation list makes no history requests',async()=>{
  let calls=0;globalThis.fetch=async()=>{calls++;throw new Error('Unexpected fetch');};
  assert.deepEqual(await api.fetchConversationProposals([]),{items:[],failed:0});
  assert.equal(calls,0);
});

test('request refreshes a 401 session only once and preserves conversation and body',async()=>{
  const isolated=load('lib/api.ts');
  const calls=[];
  globalThis.fetch=async(path,init)=>{
    calls.push({path,init});
    if(path==='/v1/auth/refresh')return Response.json({access_token:'refreshed'});
    return calls.length===1?Response.json({}, {status:401}):Response.json({assistantMessage:{id:'reply',text:'Ответ'}});
  };
  await isolated.sendChatMessage('Вопрос','key','chat');
  assert.deepEqual(calls.map(call=>call.path),['/v1/chat/messages','/v1/auth/refresh','/v1/chat/messages']);
  assert.equal(calls[2].init.headers.get('Authorization'),'Bearer refreshed');
  assert.equal(calls[2].init.headers.get('X-Conversation-Id'),'chat');
  assert.equal(calls[2].init.body,calls[0].init.body);
  assert.ok(calls.every(call=>call.init.credentials==='same-origin'));
  calls.length=0;
  globalThis.fetch=async(path)=>{calls.push(path);return Response.json({}, {status:path==='/v1/auth/refresh'?200:401});};
  await assert.rejects(isolated.fetchHistory('','chat'),err=>err.status===401);
  assert.equal(calls.length,3);
});

test('unknown access status is displayed without approval or send action',()=>{
  const html=card('custom_review');
  assert.match(html,/custom_review/);
  assert.match(html,/proposal-card--neutral/);
  assert.doesNotMatch(html,/Доступ одобрен|proposal-submit|proposal-spinner/);
});

test('roadmap refresh failure retains loaded steps and reports stale data',()=>{
  const {RoadmapSummaryDialog}=load('pages/RoadmapSummaryDialog.tsx');
  const html=renderToStaticMarkup(React.createElement(RoadmapSummaryDialog,{data:{assigned:true,groupingAvailable:true,steps:[{step_id:'git',title:'Доступ Git',current:true,completed:false,track:'tech'}],tracks:[]},loading:false,error:true,onRetry(){},onClose(){},onOpen(){}}));
  assert.match(html,/Доступ Git/);assert.match(html,/Показаны последние загруженные данные/);
  assert.match(html,/role="alert"/);
});

test('user tasks use the dedicated endpoint and map protobuf decision statuses',async()=>{
  const paths=[];
  globalThis.fetch=async(path)=>{paths.push(path);return Response.json([
    {taskId:'9007199254740993',proposalId:'p1',resourceId:'Git',accessLevel:'Developer',status:'ACCESS_REQUEST_STATUS_APPROVED',createdAt:'2026-11-15T12:00:00Z',externalRequestId:'TASK-1'},
    {taskId:'2',proposalId:'p2',resourceId:'VPN',status:'ACCESS_REQUEST_STATUS_PENDING'},
    {taskId:'3',proposalId:'p3',resourceId:'Docs',status:3},
    {taskId:'4',proposalId:'p4',status:'NEW_STATUS'},
  ]);};
  const items=await api.fetchUserRequests('42');
  assert.deepEqual(paths,['/v1/tasks/42?limit=100&after_id=0']);
  assert.deepEqual(items.map(item=>item.proposal.status),['approved','pending_approval','rejected','NEW_STATUS']);
  assert.equal(items[0].proposal.tracker_issue_key,'TASK-1');
  assert.ok(items.every(item=>!item.conversationId));
});

test('user requests render the fields emitted by the Task gateway', async () => {
  globalThis.fetch = async () => Response.json([
    { task_id:'11', proposal_id:'21', resource_id:'repo/old', access_level:'Reporter',
      status:'ACCESS_REQUEST_STATUS_PENDING', created_at:'2026-11-14T12:00:00Z' },
    { task_id:'9007199254740993', proposal_id:'22', resource_id:'repo/platform', access_level:'Developer',
      status:'ACCESS_REQUEST_STATUS_APPROVED', created_at:'2026-11-15T12:00:00Z',
      external_request_id:'TASK-22' },
  ]);

  const items = await api.fetchUserRequests('42');
  assert.deepEqual(items.map(({ proposal }) => [proposal.proposal_id, proposal.tracker_issue_key]), [
    ['22', 'TASK-22'], ['21', '11'],
  ]);
  const { RequestList } = load('pages/RequestList.tsx', { '../lib/api': api });
  const html = renderToStaticMarkup(React.createElement(RequestList, { items }));
  assert.match(html, /TASK-22/);
  assert.match(html, /repo\/platform/);
  assert.match(html, /Уровень доступа: Developer/);
  assert.match(html, /Доступ одобрен/);
  assert.doesNotMatch(html, /undefined/);
});

test('user tasks paginate with lossless int64 cursors and reject broken responses',async()=>{
  const paths=[];
  globalThis.fetch=async(path)=>{paths.push(path);return Response.json(paths.length===1?Array.from({length:100},(_,i)=>({task_id:i===99?'9007199254740993':String(i+1),proposal_id:String(i),status:1})):[]);};
  assert.equal((await api.fetchUserRequests('42')).length,100);
  assert.match(paths[1],/after_id=9007199254740993$/);
  globalThis.fetch=async()=>Response.json({tasks:[]});
  await assert.rejects(api.fetchUserRequests('42'),/Invalid tasks response/);
  globalThis.fetch=async()=>Response.json(Array.from({length:100},()=>({taskId:'0'})));
  await assert.rejects(api.fetchUserRequests('42'),/pagination/);
  globalThis.fetch=async()=>Response.json({}, {status:503});
  await assert.rejects(api.fetchUserRequests('42'),err=>err.status===503);
});

test('logout revokes the server session before marking the browser signed out',async()=>{
  const isolated=load('lib/api.ts');
  let calls=0;
  globalThis.fetch=async(path,init)=>{
    calls++;assert.equal(path,'/v1/auth/logout');assert.equal(init.method,'POST');
    assert.equal(init.credentials,'same-origin');
    assert.equal(storage.get('buddy-signed-out'),undefined);
    return new Response(null,{status:204});
  };
  await isolated.logout();assert.equal(calls,1);assert.equal(storage.get('buddy-signed-out'),'true');
  await assert.rejects(isolated.refreshSession(),/Войди/);
  assert.equal(calls,1);
});

test('failed logout does not pretend that the server session was revoked',async()=>{
  const isolated=load('lib/api.ts');
  globalThis.fetch=async()=>Response.json({}, {status:503});
  await assert.rejects(isolated.logout(),err=>err.status===503);
  assert.equal(storage.get('buddy-signed-out'),undefined);
  globalThis.fetch=async()=>Response.json({access_token:'still-valid'});
  await isolated.refreshSession();
});

test('server history replaces only the optimistic user copy, preserving real repeated messages',()=>{
  const local={id:'local-123',sender:'user',text:'Вопрос',timestamp:'2026-11-15T12:00:00Z'};
  const server={...local,id:'saved',timestamp:'2026-11-15T12:00:01Z'};
  const error={id:'error-123',sender:'assistant',text:'Ошибка',error:true};
  const merged=mergeChatMessages([local,error],[server]);
  assert.deepEqual(merged.filter(m=>m.sender==='user').map(m=>m.id),['saved']);
  assert.ok(merged.some(m=>m.error));
  const repeated=mergeChatMessages([server],[{...server,id:'saved-again'}]);
  assert.equal(repeated.length,2);
  const old={...server,id:'old',timestamp:'2026-11-14T12:00:00Z'};
  assert.equal(mergeChatMessages([local],[old]).length,2);
});

test('network errors are readable and do not expose raw server errors',()=>{
  assert.match(api.friendlyError(new TypeError('Load failed')),/Проверь соединение/);
  assert.match(api.friendlyError(new api.ApiError(503,'database password and internal stack')),/временно недоступен/);
  assert.match(api.friendlyError(new api.ApiError(401,'Unauthorized')),/Сессия истекла/);
  assert.match(api.friendlyError(new DOMException('Aborted','AbortError')),/дождаться ответа/);
  assert.doesNotMatch(api.friendlyError(new Error('internal stack')),/internal stack/);
});

test('logout confirmation and copy controls have accessible labels',()=>{
  const {LogoutDialog}=load('pages/LogoutDialog.tsx',{'../lib/api':api});
  const {MessageCopyButton}=load('pages/MessageCopyButton.tsx');
  const html=renderToStaticMarkup(React.createElement(LogoutDialog,{onClose(){},async onLogout(){}}));
  assert.match(html,/Выйти из аккаунта\?/);assert.match(html,/Остаться/);
  assert.doesNotMatch(html,/всех устройствах/);
  const copy=renderToStaticMarkup(React.createElement(MessageCopyButton,{text:'Привет'}));
  assert.match(copy,/aria-label="Копировать сообщение"/);
  assert.match(copy,/type="button"/);
  assert.match(copy,/role="tooltip"/);
  assert.match(copy,/aria-describedby=/);
  assert.doesNotMatch(copy,/title=/);
  assert.match(html,/Переписки и прогресс адаптации сохранятся<\/p>/);
});


test('unassigned roadmap dialog has a single footer action to check assignment',()=>{
  const {RoadmapSummaryDialog}=load('pages/RoadmapSummaryDialog.tsx');
  const html=renderToStaticMarkup(React.createElement(RoadmapSummaryDialog,{data:unassigned,loading:false,error:false,onRetry(){},onClose(){},onOpen(){}}));
  const footer=html.match(/<footer[^>]*>(.*?)<\/footer>/s)[1];
  assert.equal((footer.match(/<button/g)||[]).length,1);
  assert.match(footer,/Проверить назначение/);
  assert.doesNotMatch(html,/Открыть весь Roadmap/);
});

test('sidebar remembers collapsed and expanded state separately for each account',()=>{
  const preferences=load('lib/uiPreferences.ts');
  assert.equal(preferences.readMenuOpen('first',true),true);
  assert.equal(preferences.readMenuOpen('first',false),false);
  preferences.saveMenuOpen('first',false);
  preferences.saveMenuOpen('second',true);
  // Re-load the module as a page reload would, keeping browser storage.
  const restored=load('lib/uiPreferences.ts');
  assert.equal(restored.readMenuOpen('first',true),false);
  assert.equal(restored.readMenuOpen('second',false),true);
  localStorage.setItem('buddy-menu-open-first','invalid');
  assert.equal(restored.readMenuOpen('first',true),true);
});

test('profile help and logout confirmation survive reload and stay closed after dismissal',()=>{
  const descriptor=Object.getOwnPropertyDescriptor(globalThis,'sessionStorage');
  const session=new Map();
  Object.defineProperty(globalThis,'sessionStorage',{configurable:true,value:{getItem:key=>session.get(key)??null,setItem:(key,value)=>session.set(key,value),removeItem:key=>session.delete(key)}});
  try {
    const preferences=load('lib/uiPreferences.ts');
    for(const dialog of ['help','logout']) {
      preferences.saveProfileDialog('first',dialog);
      const restored=load('lib/uiPreferences.ts');
      assert.equal(restored.readProfileDialog('first'),dialog);
      assert.equal(restored.readProfileDialog('second'),'');
      restored.saveProfileDialog('first','');
      assert.equal(load('lib/uiPreferences.ts').readProfileDialog('first'),'');
      assert.equal(session.has('buddy-profile-dialog-first'),false);
    }
    session.set('buddy-profile-dialog-first','unexpected');
    assert.equal(preferences.readProfileDialog('first'),'');
  } finally {
    if(descriptor)Object.defineProperty(globalThis,'sessionStorage',descriptor);else delete globalThis.sessionStorage;
  }
});

test('UI preferences tolerate blocked browser storage',()=>{
  const local=Object.getOwnPropertyDescriptor(globalThis,'localStorage');
  const session=Object.getOwnPropertyDescriptor(globalThis,'sessionStorage');
  const denied={configurable:true,get(){throw new Error('Storage denied');}};
  Object.defineProperty(globalThis,'localStorage',denied);
  Object.defineProperty(globalThis,'sessionStorage',denied);
  try {
    const preferences=load('lib/uiPreferences.ts');
    assert.equal(preferences.readMenuOpen('first',false),false);
    assert.equal(preferences.readProfileDialog('first'),'');
    assert.doesNotThrow(()=>preferences.saveMenuOpen('first',true));
    assert.doesNotThrow(()=>preferences.saveProfileDialog('first','help'));
    assert.doesNotThrow(()=>preferences.saveProfileDialog('first',''));
  } finally {
    if(local)Object.defineProperty(globalThis,'localStorage',local);else delete globalThis.localStorage;
    if(session)Object.defineProperty(globalThis,'sessionStorage',session);else delete globalThis.sessionStorage;
  }
});

test('reloading already confirmed history must not erase a new identical pending message',()=>{
  const saved={id:'server-1',sender:'user',text:'Привет',timestamp:'2026-11-15T12:00:00Z'};
  const pending={...saved,id:'local-new',timestamp:'2026-11-15T12:00:10Z'};
  const messages=mergeChatMessages([saved,pending],[saved]);
  assert.deepEqual(messages.map(message=>message.id),['server-1','local-new']);
});

test('roadmap preview cannot choose a next step if the requested current step is absent',()=>{
  const {nextSummaryStep}=load('pages/RoadmapSummaryDialog.tsx');
  const data={assigned:true,groupingAvailable:true,tracks:[],steps:[{step_id:'first',track:'tech',completed:false}]};
  assert.equal(nextSummaryStep(data,{step_id:'missing',track:'tech',completed:false}),undefined);
});

test('successful malformed JSON is an error, while empty 204 and non-JSON server failures are supported',async()=>{
  globalThis.fetch=async()=>new Response('not JSON',{status:200});
  await assert.rejects(api.request('/test'),SyntaxError);
  globalThis.fetch=async()=>new Response(null,{status:204});
  assert.deepEqual(await api.request('/test'),{});
  globalThis.fetch=async()=>new Response('<html>Bad gateway</html>',{status:502});
  await assert.rejects(api.request('/test'),error=>error instanceof api.ApiError && error.status===502);
});

test('HTTP timeout still applies when the caller provides its own cancellation signal',async(t)=>{
  t.mock.timers.enable({apis:['setTimeout']});
  const caller=new AbortController();
  let signal;
  globalThis.fetch=async(_path,init)=>{
    signal=init.signal;
    return new Promise((_resolve,reject)=>signal.addEventListener('abort',()=>reject(new DOMException('Timeout','AbortError')),{once:true}));
  };
  const result=api.request('/slow',{signal:caller.signal});
  const outcome=result.catch(error=>error);
  t.mock.timers.tick(120000);
  const aborted=signal.aborted;
  caller.abort(); // Clean up even when testing a broken timeout implementation.
  await outcome;
  assert.equal(aborted,true);
});

test('HTTP timeout includes reading a stalled response body',async(t)=>{
  t.mock.timers.enable({apis:['setTimeout']});
  let started;
  const reading=new Promise(resolve=>{started=resolve;});
  globalThis.fetch=async(_path,init)=>({
    status:200,ok:true,
    json(){started();return new Promise((_resolve,reject)=>init.signal.addEventListener('abort',()=>reject(new DOMException('Timeout','AbortError')),{once:true}));},
  });
  const result=api.request('/slow-body');
  const rejected=assert.rejects(result,error=>error.name==='AbortError');
  await reading;
  t.mock.timers.tick(120000);
  await rejected;
});

test('caller cancellation forwards its original reason to the HTTP request',async()=>{
  const caller=new AbortController();
  const reason=new DOMException('Cancelled by user','AbortError');
  globalThis.fetch=async(_path,init)=>new Promise((_resolve,reject)=>init.signal.addEventListener('abort',()=>reject(init.signal.reason),{once:true}));
  const result=api.request('/cancelled',{signal:caller.signal});
  const rejected=assert.rejects(result,error=>error===reason);
  caller.abort(reason);
  await rejected;
});
