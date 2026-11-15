import test, { afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { basename, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { compileFunction } from 'node:vm';
import ts from 'typescript';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

const require = createRequire(import.meta.url);
const coverageDir = process.env.NODE_V8_COVERAGE
  ? fileURLToPath(new URL('../coverage/', import.meta.url))
  : null;

function load(file, overrides = {}) {
  const sourcePath = fileURLToPath(new URL(`../src/${file}`, import.meta.url));
  const source = readFileSync(sourcePath, 'utf8');
  const { outputText, sourceMapText } = ts.transpileModule(source, {
    fileName: sourcePath,
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2020,
      jsx: ts.JsxEmit.React,
      esModuleInterop: true,
      sourceMap: Boolean(coverageDir),
      inlineSources: Boolean(coverageDir),
    },
  });

  const module = { exports: {} };
  const resolve = id => overrides[id] || require(id);
  const args = ['require', 'module', 'exports'];

  if (coverageDir) {
    mkdirSync(coverageDir, { recursive: true });
    const generatedDir = mkdtempSync(join(coverageDir, 'compiled-'));
    const filename = join(generatedDir, `${basename(file)}.cjs`);
    const sourceMap = JSON.parse(sourceMapText);
    sourceMap.sources = [sourcePath];
    const encoded = Buffer.from(JSON.stringify(sourceMap)).toString('base64');
    const code = outputText.replace(
      /sourceMappingURL=.*$/,
      `sourceMappingURL=data:application/json;base64,${encoded}`,
    );

    writeFileSync(filename, code);
    compileFunction(code, args, { filename })(resolve, module, module.exports);
  } else {
    compileFunction(outputText, args, { filename: sourcePath })(
      resolve, module, module.exports,
    );
  }

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
