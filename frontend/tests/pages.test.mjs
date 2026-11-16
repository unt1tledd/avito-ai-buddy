import assert from 'node:assert/strict';
import test from 'node:test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { load } from './load-source.mjs';

const api = load('lib/api.ts');
const appChrome = load('pages/AppChrome.tsx');
const { TasksPage } = load('pages/TasksPage.tsx');
const { ProfilePage } = load('pages/ProfilePage.tsx');
const { RequestsDialog } = load('pages/RequestsDialog.tsx', { '../lib/api': api });
const homePage = load('pages/HomePage.tsx', {
  '../lib/api': api,
  './RoadmapSummaryDialog': load('pages/RoadmapSummaryDialog.tsx'),
  './RequestsDialog': { RequestsDialog },
});
const messageContent = load('pages/MessageContent.tsx');
const chatPage = load('pages/ChatPage.tsx', {
  '../lib/api': api,
  '../lib/chatEvents': load('lib/chatEvents.ts'),
  './AccessProposalCard': load('pages/AccessProposalCard.tsx', {
    '../lib/api': api,
    './MessageContent': messageContent,
  }),
  './MessageContent': messageContent,
  './AppChrome': appChrome,
  './HomePage': homePage,
  './OnboardingPage': load('pages/OnboardingPage.tsx', { '../lib/api': api }),
});
const { ChatPage } = chatPage;
const { default: App } = load('App.tsx', {
  './pages/ChatPage': chatPage,
  './pages/AuthForm': load('pages/AuthForm.tsx'),
  './pages/SetupPage': load('pages/SetupPage.tsx', { '../lib/api': api }),
  './pages/AppChrome': appChrome,
  './lib/api': api,
});
const { ResizableSidebar } = load('pages/ResizableSidebar.tsx');
const { RequestList } = load('pages/RequestList.tsx');
const cookies = load('lib/cookies.ts');

const noop = () => {};
const render = (Component, props) => renderToStaticMarkup(React.createElement(Component, props));

function mockGlobal(t, key, value) {
  const previous = Object.getOwnPropertyDescriptor(globalThis, key);
  Object.defineProperty(globalThis, key, { configurable: true, value });
  t.after(() => {
    if (previous) Object.defineProperty(globalThis, key, previous);
    else delete globalThis[key];
  });
}

function* buttons(node) {
  if (Array.isArray(node)) {
    for (const child of node) yield* buttons(child);
  } else if (React.isValidElement(node)) {
    if (node.type === 'button') yield node;
    yield* buttons(node.props.children);
  }
}

test('home request preview includes at most three requests regardless of status, while the full list retains all', () => {
  const items = ['approved', 'pending_approval', 'failed', 'draft', 'rejected'].map((status, index) => ({
    conversationId: `chat-${index}`,
    proposal: { proposal_id: String(index), resource_label: `Ресурс ${index}`, access_level: 'Read', status },
  }));
  const { RequestsDialog: PreviewDialog } = load('pages/RequestsDialog.tsx', {
    '../lib/useRequests': { useRequests: () => ({ items, loaded: true, loading: false, failed: 0, refresh: noop }) },
  });
  const preview = render(PreviewDialog, { userId: '42', onClose: noop, onChat: noop });
  assert.equal((preview.match(/<article\b/g) || []).length, 3);
  for (const index of [0, 1, 2]) assert.match(preview, new RegExp(`Ресурс ${index}`));
  assert.doesNotMatch(preview, /Ресурс [34]/);
  assert.equal((render(RequestList, { items }).match(/<article\b/g) || []).length, 5);
  assert.doesNotMatch(render(RequestList, { items, limit: 0 }), /<article\b/);
  assert.doesNotMatch(render(RequestList, { items: [] }), /<article\b/);
});

test('request refresh errors retain the previous cards and never claim that the list is empty', () => {
  for (const items of [[], [{ conversationId: 'chat', proposal: { proposal_id: '1', resource_label: 'Git', access_level: 'Read', status: 'approved' } }]]) {
    const { RequestsDialog: FailedDialog } = load('pages/RequestsDialog.tsx', {
      '../lib/useRequests': { useRequests: () => ({ items, loaded: true, loading: false, failed: 1, refresh: noop }) },
    });
    const html = render(FailedDialog, { userId: '42', onClose: noop, onChat: noop });
    assert.match(html, /role="alert"/);
    assert.doesNotMatch(html, /Заявок пока нет/);
    assert.equal((html.match(/<article\b/g) || []).length, items.length);
    if (items.length) assert.match(html, /Git/);
  }
});

test('request actions open the matching conversation and remain absent for tasks without a conversation', () => {
  const onChat = test.mock.fn();
  const proposal = { proposal_id: '1', resource_id: 'repo', access_level: 'Read', status: 'approved' };
  const items = [{ conversationId: 'first', proposal }, { conversationId: '', proposal }, { conversationId: 'second', proposal }];
  const actions = [...buttons(RequestList({ items, onChat }))];
  assert.equal(actions.length, 2);
  actions[1].props.onClick();
  actions[0].props.onClick();
  assert.deepEqual(onChat.mock.calls.map(call => call.arguments), [['second'], ['first']]);
});

test('tasks show the empty state and only pending requests offer approval', () => {
  const onApproveTask = test.mock.fn();
  const props = { role: 'intern', onApproveTask, onClose: noop };
  const empty = render(TasksPage, { ...props, tasks: [] });
  assert.match(empty, /Пока нет созданных заявок/);
  assert.match(empty, /<strong>Стажёр<\/strong>/);

  const tasks = [
    { task_id: '1', status: 'approved', title: 'Git', tracker_issue_key: 'A-1' },
    { task_id: '2', status: 'pending_approval', title: 'Wiki', tracker_issue_key: 'A-2' },
  ];
  const leadProps = { ...props, tasks, role: 'teamlead' };
  const html = render(TasksPage, leadProps);
  assert.match(html, /Тимлид \(руководитель\)/);
  assert.match(html, /Доступ выдан/);
  assert.match(html, /Ожидает тимлида/);
  const approve = [...buttons(TasksPage(leadProps))].filter(button => /Одобрить доступ/.test(button.props.children));
  assert.equal(approve.length, 1);
  approve[0].props.onClick();
  assert.deepEqual(onApproveTask.mock.calls.map(call => call.arguments), [['2']]);
});

test('profile role buttons report the selected role and close independently', () => {
  const onChangeRole = test.mock.fn();
  const onClose = test.mock.fn();
  const props = { role: 'intern', onChangeRole, onClose };
  assert.match(render(ProfilePage, props), /Стажёр Backend-команды/);
  assert.match(render(ProfilePage, { ...props, role: 'teamlead' }), /Тимлид \/ Наставник/);
  const [close, intern, lead] = buttons(ProfilePage(props));
  lead.props.onClick();
  intern.props.onClick();
  close.props.onClick();

  assert.deepEqual(onChangeRole.mock.calls.map(call => call.arguments), [['teamlead'], ['intern']]);
  assert.equal(onClose.mock.callCount(), 1);
});

test('theme toggle announces its destination and invokes the toggle callback', () => {
  const onToggle = test.mock.fn();
  const { ThemeToggle } = appChrome;
  assert.match(render(ThemeToggle, { dark: false, onToggle }), /Переключить на тёмную тему/);
  assert.match(render(ThemeToggle, { dark: true, onToggle }), /Переключить на светлую тему/);
  ThemeToggle({ dark: false, onToggle }).props.onClick();
  assert.equal(onToggle.mock.callCount(), 1);
});

test('workspace restores the profile screen and saved menu width on desktop', t => {
  const values = new Map([
    ['buddy-screen-42', 'profile'],
    ['buddy-sidebar-width', '360'],
  ]);
  mockGlobal(t, 'localStorage', { getItem: key => values.get(key) ?? null });
  mockGlobal(t, 'window', { innerWidth: 1200, matchMedia: () => ({ matches: false }) });
  const html = render(ChatPage, {
    userId: '42',
    profile: { name: 'Анна Иванова', email: 'anna@example.test', team: 'Platform' },
    dark: true,
    onLogout: noop,
    onToggleTheme: noop,
  });

  assert.match(html, /<h1>Профиль<\/h1>/);
  assert.match(html, /--menu-width:360px/);
  assert.match(html, /aria-label="Свернуть меню"/);
  assert.match(html, />АИ<\/span>/);
  assert.match(html, /<dd>anna@example.test<\/dd>/);
  assert.match(html, /<dd>Platform<\/dd>/);
  assert.match(html, /<dd>Не указано<\/dd>/);
});

test('workspace falls back to home and keeps the menu collapsed on mobile', t => {
  mockGlobal(t, 'localStorage', {
    getItem: key => key === 'buddy-screen-42' ? 'unknown' : null,
  });
  mockGlobal(t, 'window', { innerWidth: 390, matchMedia: () => ({ matches: true }) });
  const html = render(ChatPage, {
    userId: '42',
    profile: { name: '  Анна Иванова  ', email: '' },
    dark: false,
    onLogout: noop,
    onToggleTheme: noop,
  });

  assert.match(html, /<h1>Главная<\/h1>/);
  assert.match(html, /<h2>Привет, Анна!<\/h2>/);
  assert.match(html, /aria-label="Открыть меню"[^>]*aria-expanded="false"/);
  assert.match(html, /Загружаем переписки/);
  assert.doesNotMatch(html, /role="separator"/);
});

test('home shows the most recent chat and restores the requests dialog while it loads', t => {
  mockGlobal(t, 'sessionStorage', {
    getItem: key => key === 'buddy-home-dialog-42' ? 'requests' : null,
  });
  const html = render(homePage.HomePage, {
    userId: '42',
    name: '',
    chats: [
      { id: '1', title: 'Первый чат', createdAt: '2026-01-01' },
      { id: '2', title: 'Последний вопрос', createdAt: '2026-01-02' },
    ],
    busy: false,
    onChat: noop,
    onQuestion: noop,
    onRoadmap: noop,
  });

  assert.match(html, /<h2>Привет!<\/h2>/);
  assert.match(html, /Последний вопрос/);
  assert.doesNotMatch(html, /Первый чат|Заявок пока нет/);
  assert.match(html, /role="status">Загружаем заявки/);
});

test('cookies parse and encode access tokens and keep the chosen role in storage', t => {
  const values = new Map();
  mockGlobal(t, 'localStorage', {
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
  });
  const document = { cookie: 'other_access_token=wrong; theme=dark' };
  mockGlobal(t, 'document', document);

  assert.equal(cookies.getAccessToken(), '');
  document.cookie += '; access_token=a%2Bb%3B%20c; other=value';
  assert.equal(cookies.getAccessToken(), 'a+b; c');
  cookies.setAccessToken('a+b; c');
  assert.equal(document.cookie, 'access_token=a%2Bb%3B%20c; path=/; max-age=86400');
  assert.equal(cookies.getSavedRole(), 'intern');
  cookies.setSavedRole('teamlead');
  assert.equal(cookies.getSavedRole(), 'teamlead');
});

test('app shows the session restore state before rendering login or workspace', t => {
  mockGlobal(t, 'document', { documentElement: { dataset: { theme: 'dark' } } });
  const html = render(App);
  assert.match(html, /role="status" aria-label="Восстанавливаем приложение"/);
  assert.doesNotMatch(html, /<form|<nav/);
});

test('sidebar clamps the saved width and hides its resize handle when closed', t => {
  mockGlobal(t, 'localStorage', { getItem: () => '1000' });

  const open = render(ResizableSidebar, { panel: 'history', children: 'Переписки' });
  assert.match(open, /drawer-shell--open-history/);
  assert.match(open, /role="separator"[^>]*aria-valuenow="640"/);
  assert.match(open, /aria-hidden="false">Переписки/);

  const closed = render(ResizableSidebar, { panel: null, children: 'Переписки' });
  assert.match(closed, /aria-hidden="true">Переписки/);
  assert.doesNotMatch(closed, /role="separator"/);
});
