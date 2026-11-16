import test, { beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { load } from './load-source.mjs';

const originalStorage=Object.getOwnPropertyDescriptor(globalThis,'localStorage');
beforeEach(()=>{
  Object.defineProperty(globalThis,'localStorage',{configurable:true,value:{getItem:()=>null}});
});
afterEach(()=>{
  if(originalStorage)Object.defineProperty(globalThis,'localStorage',originalStorage);else delete globalThis.localStorage;
});

test('ChatPage loads its real TSX and TypeScript dependencies without component stubs',()=>{
  const {ChatPage}=load('pages/ChatPage.tsx');
  assert.equal(typeof ChatPage,'function');
});

test('relative TSX imports resolve from the importing source, including nested imports',()=>{
  const {AccessProposalCard}=load('pages/AccessProposalCard.tsx');
  const html=renderToStaticMarkup(React.createElement(AccessProposalCard,{
    proposal:{proposal_id:'1',resource_id:'https://example.com/repo',resource_label:'Git',access_level:'Read',status:'draft'},
    sending:false,onConfirm(){},
  }));
  assert.match(html,/Отправить заявку/);
  assert.match(html,/href="https:\/\/example.com\/repo"/);
});

test('explicit mocks remain supported while other local imports are resolved normally',()=>{
  const {AccessProposalCard}=load('pages/AccessProposalCard.tsx',{'../lib/api':{statusLabel:()=> 'Проверяемый статус'}});
  const html=renderToStaticMarkup(React.createElement(AccessProposalCard,{
    proposal:{proposal_id:'1',resource_id:'repo',resource_label:'Git',access_level:'Read',status:'custom'},sending:false,onConfirm(){},
  }));
  assert.match(html,/Проверяемый статус/);
});
