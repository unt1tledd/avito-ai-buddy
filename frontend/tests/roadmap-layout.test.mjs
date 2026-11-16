import test, { beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import React from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
import {load} from './load-source.mjs';
const originalStorage=Object.getOwnPropertyDescriptor(globalThis,'localStorage');
beforeEach(()=>Object.defineProperty(globalThis,'localStorage',{configurable:true,value:{getItem:()=>null}}));
afterEach(()=>{if(originalStorage)Object.defineProperty(globalThis,'localStorage',originalStorage);else delete globalThis.localStorage;});
const {OnboardingPage}=load('pages/OnboardingPage.tsx');
const {MessageSources}=load('pages/MessageContent.tsx');
const steps=[{step_id:'first',step_number:1,title:'Первый шаг',period:'1 неделя',description:'',completed:true,track:'common'},{step_id:'second',step_number:2,title:'Второй шаг',period:'2 неделя',description:'',completed:true,track:'common'}];
const data={assigned:true,groupingAvailable:true,steps,tracks:[{track:'common',display_name:'Общая адаптация'}]};
const render=()=>renderToStaticMarkup(React.createElement(OnboardingPage,{data,userId:'test',onRetry(){},onToggleStep(){},onSelectStepQuestion(){}}));

test('legacy weekly preference still renders a flat plan in server order without the view switch',()=>{
  const storage=Object.getOwnPropertyDescriptor(globalThis,'localStorage');
  Object.defineProperty(globalThis,'localStorage',{configurable:true,value:{getItem:()=>JSON.stringify({track:'common',step:'second',view:'weeks'})}});
  try {
    const html=render();
    assert.doesNotMatch(html,/По неделям|Вид Roadmap|roadmap-period|Откроем новый чат/);
    assert.match(html,/roadmap-timeline/);
    assert.ok(html.indexOf('Первый шаг')<html.indexOf('Второй шаг'));
    assert.doesNotMatch(html,/Спросить бадди: Первый шаг/);
    assert.match(html,/Спросить бадди: Второй шаг/);
  } finally {if(storage)Object.defineProperty(globalThis,'localStorage',storage);else delete globalThis.localStorage;}
});

test('completion message belongs to the progress card rather than the steps list',()=>{
  const html=render();
  const progress=html.match(/<section class="roadmap-progress"[\s\S]*?<\/section>/)[0];
  assert.match(progress,/Общая адаптация/);
  assert.match(progress,/Все шаги выполнены. Отличная работа!/);
  assert.equal((html.match(/Все шаги выполнены/g)||[]).length,1);
});

test('source cards remove checklist badges without dropping sources or demo labels',()=>{
  const html=renderToStaticMarkup(React.createElement(MessageSources,{sources:[{id:'check',title:'[ЧЕК-ЛИСТ] Выпускной',url:'https://example.com/check'},{id:'demo',title:'[ДЕМО] Документ',url:'https://example.com/demo'}]}));
  assert.doesNotMatch(html,/ЧЕК-ЛИСТ/);
  assert.match(html,/Выпускной/);assert.match(html,/\[ДЕМО\] Документ/);
  assert.match(html,/href="https:\/\/example.com\/check"/);
  assert.equal((html.match(/<a /g)||[]).length,2);
});


test('roadmap shows no inline ask actions before selecting a step',()=>{
  assert.doesNotMatch(render(),/class="roadmap-step-ask"/);
});
test('only the selected step receives an inline ask action',()=>{
  const storage=Object.getOwnPropertyDescriptor(globalThis,'localStorage');
  Object.defineProperty(globalThis,'localStorage',{configurable:true,value:{getItem:()=>JSON.stringify({track:'common',step:'first'})}});
  try {
    const html=render();
    assert.equal((html.match(/class="roadmap-step-ask"/g)||[]).length,1);
    assert.match(html,/Спросить бадди: Первый шаг/);
    assert.doesNotMatch(html,/Спросить бадди: Второй шаг/);
  } finally {if(storage)Object.defineProperty(globalThis,'localStorage',storage);else delete globalThis.localStorage;}
});
