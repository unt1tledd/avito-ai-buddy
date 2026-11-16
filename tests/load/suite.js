import http from 'k6/http';
import exec from 'k6/execution';
import { sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const config = JSON.parse(open(__ENV.LOAD_CONFIG));
const { prefix, mode, rate, seconds, users, taskToken, decider } = config;
const steps = { a: '01', b: '02', q: '03', r: '04', c: '05', d: '06', e: '07', f: '08', g: '09' };
const AUTH = 'http://127.0.0.1:8081/v1/auth/login';
const CHAT = 'http://127.0.0.1:8082/v1/chat';
const TASK = 'http://127.0.0.1:8086/internal/v1/tasks';
const isMock = ['mock', 'faults', 'soak'].includes(mode);
const isTask = ['task', 'queue', 'drain'].includes(mode);
const counts = new Counter('workflows_completed');
const errors = new Rate('workflow_failed');
const outcomes = new Counter('workflow_outcomes');
const latency = new Trend('step_duration_ms', true);

function openRate(perMinute, vus, entrypoint) {
  return {
    executor: 'constant-arrival-rate', rate: perMinute, timeUnit: '1m',
    duration: `${seconds}s`, preAllocatedVUs: vus, gracefulStop: '100s',
    exec: entrypoint,
  };
}

const scenarios = {};
if (['history', 'mixed', 'mock', 'soak'].includes(mode)) {
  scenarios.history = openRate(mode === 'history' ? rate * 60 : config.historyRps * 60, 60, 'readHistory');
}
if (['ai', 'context', 'mixed', 'mock', 'faults', 'soak'].includes(mode)) {
  scenarios.questions = openRate(rate, Math.max(12, Math.ceil(rate * 90 / 60) + 4), 'askQuestion');
}
if (isTask) {
  scenarios.tasks = mode === 'drain'
    ? { executor: 'shared-iterations', vus: 20, iterations: users.task.length, maxDuration: '5m', exec: 'runTask' }
    : openRate(rate, Math.max(16, Math.ceil(rate * 70 / 60) + 4), 'runTask');
}

const minimum = (perMinute) => Math.max(1, Math.floor(perMinute * seconds / 60));
const thresholds = { dropped_iterations: ['count==0'], workflow_failed: ['rate==0'] };
for (const name of Object.keys(scenarios)) {
  const perMinute = name === 'history' ? (mode === 'history' ? rate : config.historyRps) * 60 : rate;
  const expected = mode === 'drain' ? users.task.length : minimum(perMinute);
  thresholds[`workflows_completed{scenario:${name}}`] = [`count>=${expected}`];
}

export const options = {
  setupTimeout: '12m',
  summaryTrendStats: ['med', 'p(95)', 'p(99)', 'max'],
  scenarios,
  thresholds,
};

function json(response) {
  try { return response.json(); } catch { return null; }
}

function login(user) {
  const response = http.post(AUTH, JSON.stringify({ email: user.email, password: user.password }), {
    headers: { 'Content-Type': 'application/json' },
    redirects: 0, timeout: '8s', tags: { name: 'login' },
  });
  return response.status === 200 ? response.cookies.access_token?.[0]?.value : null;
}

export function setup() {
  const sessions = {};
  for (const [group, members] of Object.entries(users)) {
    sessions[group] = members.map((user) => {
      const token = login(user);
      if (!token) throw new Error(`Login failed for fixture ${user.index}`);
      return { ...user, token, issued: Date.now() };
    });
  }
  if (sessions.history) {
    const user = sessions.history[0];
    const id = prefix + user.index.toString(16).padStart(4, '0') + '000000000000';
    const response = http.get(`${CHAT}/messages?page_size=20`, {
      cookies: { access_token: { value: user.token, replace: true } }, headers: { 'X-Request-ID': id },
      redirects: 0, timeout: '5s', tags: { name: 'preflight_history' },
    });
    if (response.status !== 200 || response.headers['X-Request-Id'] !== id ||
        json(response)?.messages?.at(-1)?.text !== `${user.marker}-${config.messages}`) {
      throw new Error(`History preflight failed, HTTP ${response.status}, request_id=${id}`);
    }
  }
  for (const [index, user] of (sessions.probe || []).entries()) {
    const [kind, text, source] = isMock
      ? ['mock', '[load:fast] Как проверить работу тестовой инструкции?', '/load-test/guide']
      : mode === 'context' ? contextCase : liveCases[index];
    const id = prefix + user.index.toString(16).padStart(4, '0') + '000000000000';
    const response = http.post(`${CHAT}/messages`, JSON.stringify({
  text,
  idempotencyKey: mode === 'context' ? '2' : '1',
}), {
      cookies: { access_token: { value: user.token, replace: true } },
      headers: { 'Content-Type': 'application/json', 'X-Request-ID': id },
      redirects: 0, timeout: '75s', tags: { name: 'preflight_question' },
    });
    const result = { response, data: json(response), correlated: response.headers['X-Request-Id'] === id };
    const reason = answerProblem(result, answerFrom(result), source);
    console.log(JSON.stringify({ phase: 'preflight', kind, reason: reason || 'ok', request_id: id }));
    if (reason) throw new Error(`AI preflight ${kind}: ${reason}, request_id=${id}`);
  }
  return sessions;
}

const renewed = {};
function sessionFor(group, sessions) {
  const index = exec.scenario.iterationInTest;
  const pool = sessions[group];
  // A terminal start at the duration boundary may have no dedicated fixture.
  if (group !== 'history' && mode !== 'soak' && index >= pool.length) return null;
  const keepAccountPerVU = mode === 'soak' && group === 'question';
  const user = pool[keepAccountPerVU
    ? exec.vu.idInTest - 1
    : index % pool.length];
  if (!user) throw new Error(`Missing ${group} fixture for VU ${exec.vu.idInTest}`);
  return user;
}

function rid(user, step, attempt = 0) {
  return prefix + user.index.toString(16).padStart(4, '0') + steps[step] +
    exec.scenario.iterationInTest.toString(16).padStart(7, '0') +
    attempt.toString(16).padStart(3, '0');
}

function request(user, step, method, url, body, opts = {}) {
  const id = rid(user, step, opts.attempt);
  const headers = { 'X-Request-ID': id, 'Content-Type': 'application/json' };
  const params = {
    headers, timeout: opts.timeout || '5s', redirects: 0,
    tags: { name: opts.name },
  };
  if (opts.internal) {
    headers.Authorization = `Bearer ${taskToken}`;
  } else {
    let session = renewed[user.index] || user;
    if (Date.now() - session.issued >= 8 * 60 * 1000) {
      const token = login(user);
      if (!token) throw new Error(`Session renewal failed for fixture ${user.index}`);
      session = { token, issued: Date.now() };
      renewed[user.index] = session;
    }
    params.cookies = { access_token: { value: session.token, replace: true } };
  }
  const response = http.request(method, url, body === null ? null : JSON.stringify(body), params);
  if (opts.measure !== false) latency.add(response.timings.duration, { step: opts.name, kind: opts.kind || 'default' });
  return { response, data: json(response), id, correlated: response.headers['X-Request-Id'] === id };
}

function history(user, size, cursor = '', attempt = 0, measure = true) {
  const query = `page_size=${size}${cursor ? `&page_token=${encodeURIComponent(cursor)}` : ''}`;
  return request(user, cursor ? 'b' : 'a', 'GET', `${CHAT}/messages?${query}`, null,
    { name: cursor ? 'history_next' : 'history_first', attempt, measure });
}

function finish(user, reason, sample, kind = 'default') {
  counts.add(1);
  errors.add(Boolean(reason), { kind });
  outcomes.add(1, { kind, reason: reason || 'ok' });
  if (reason || sample) {
    console.log(JSON.stringify({
      fixture: user.index, scenario: exec.scenario.name, kind, reason: reason || (sample?.response.timings.duration >= 15000 ? 'slow' : 'ok'),
      request_id: sample?.id, http_status: sample?.response.status,
      ms: sample?.response.timings.duration,
    }));
  }
}

export function readHistory(sessions) {
  const user = sessionFor('history', sessions);
  const pages = config.pages;
  let cursor = '';
  const seen = new Set();
  for (let page = 0; page < pages; page += 1) {
    const result = history(user, 20, cursor, page);
    const messages = result.data?.messages;
    const offset = page * 20;
    const expected = Math.min(20, config.messages - offset);
    if (result.response.status !== 200 || !result.correlated ||
        !Array.isArray(messages) || messages.length !== expected ||
        messages.some((message, index) => {
          if (!message.id || seen.has(message.id)) return true;
          seen.add(message.id);
          return message.text !== `${user.marker}-${config.messages - offset - expected + index + 1}`;
        })) {
      if (exec.scenario.iterationInTest < 3) {
        console.warn(JSON.stringify({
          check: 'history_content',
          request_id: result.id,
          status: result.response.status,
          correlated: result.correlated,
          expected_count: expected,
          actual_count: Array.isArray(messages) ? messages.length : null,
          expected_texts: Array.from(
            { length: expected },
            (_, i) => `${user.marker}-${config.messages - offset - expected + i + 1}`,
          ),
          actual_texts: Array.isArray(messages) ? messages.map((row) => row.text) : null,
        }));
      }    
      finish(user, 'history_content', result);
      return;
    }
    const next = result.data.nextPageToken ?? result.data.next_page_token ?? '';
    if ((offset + expected < config.messages && (!next || next === cursor)) ||
        (offset + expected === config.messages && next)) {
      finish(user, 'history_cursor', result);
      return;
    }
    cursor = next;
  }
  finish(user, '', null);
}

const liveCases = [
  ['ordinary', 'Что должно быть в первом pull request стажёра?', '/load-test/guide'],
  ['typos', 'Кокие блоки нужны в первом пул реквесте стажора?', '/load-test/guide'],
  ['paraphrase', 'Я стажёр и застрял с ошибкой в коде. Кто из бадди, наставника и руководителя помогает с техническими ошибками?', '/load-test/guide'],
];

const contextCase = [
  'follow_up', 'А какие четыре блока в нём должны быть?',
  '/load-test/guide',
];

function questionCase(index) {
  if (mode === 'context') {
    return contextCase;
  }

  if (!isMock) {
    return liveCases[index % liveCases.length];
  }

  const faultKinds = ['fast', 'slow', '429', '500', 'timeout'];
  const kind = mode === 'faults'
    ? faultKinds[index % faultKinds.length]
    : index % 2 === 0 ? 'fast' : 'slow';

  return [
    kind,
    `[load:${kind}] Как проверить работу тестовой инструкции? Маркер ${index}.`,
    '/load-test/guide',
  ];
}

function answerFrom(result) {
  return result.data?.assistantMessage ?? result.data?.assistant_message;
}

function answerProblem(result, answer, source) {
  if (result.response.status !== 200) return `answer_http_${result.response.status}`;
  if (!result.correlated) return 'request_id_mismatch';
  if (!answer?.id || typeof answer.text !== 'string' || !answer.text.trim()) return 'invalid_answer';
  if (!Array.isArray(answer.sources) ||
      !answer.sources.some((item) => item?.url === `https://wiki.avito.example${source}`)) {
    return 'expected_source_missing';
  }
  return '';
}

export function askQuestion(sessions) {
  const user = sessionFor('question', sessions);
  if (!user) return;

  const index = exec.scenario.iterationInTest;
  const [kind, text, source] = questionCase(index);
  const key = String(index + 2);

  const send = (step) => request(
    user,
    step,
    'POST',
    `${CHAT}/messages`,
    { text, idempotencyKey: key },
    { name: step === 'r' ? 'ask_replay' : 'ask', kind, timeout: '75s' },
  );

  const result = send('q');
  const answer = answerFrom(result);
  const reason = answerProblem(result, answer, source);

  if (reason) {
    finish(user, reason, result, kind);
    return;
  }

  const injectedFailure =
    mode === 'faults' && ['429', '500', 'timeout'].includes(kind);

  if (injectedFailure && !answer.text.startsWith('AI временно недоступен.')) {
    finish(user, 'expected_fallback_missing', result, kind);
    return;
  }

  if (mode === 'mock' && index % 10 === 0) {
    const replay = send('r');
    if (
      replay.response.status !== 200 ||
      !replay.correlated ||
      answerFrom(replay)?.id !== answer.id
    ) {
      finish(user, 'idempotency', replay, kind);
      return;
    }
  }

  const after = history(user, 10, '', 0, false);
  const rows = after.data?.messages;
  if (
    after.response.status !== 200 ||
    !Array.isArray(rows) ||
    rows.filter((row) => row.text === text).length !== 1 ||
    rows.filter((row) => row.id === answer.id && row.text === answer.text).length !== 1 ||
    (mode === 'context' ? rows.length !== 4 : mode !== 'soak' && rows.length !== 2)
  ) {
    finish(user, 'answer_history', after, kind);
    return;
  }

  finish(user, '', result, kind);
}

function poll(check) {
  const deadline = Date.now() + 30000;
  for (let attempt = 0; Date.now() < deadline; attempt += 1) {
    const found = check(attempt);
    if (found) return found;
    sleep(0.5);
  }
  return null;
}

export function runTask(sessions) {
  const user = sessionFor('task', sessions);
  if (!user) return;
  const confirm = (step) => request(user, step, 'POST', `${CHAT}/proposals/${user.proposal}/confirm`, {},
    { name: step === 'c' ? 'confirm' : 'confirm_replay' });
  const startedAt = Date.now();
  if (mode !== 'drain') {
    const result = confirm('c');
    const state = result.data?.accessRequest ?? result.data?.access_request;
    if (result.response.status !== 200 || !result.correlated || !state?.status) {
      finish(user, 'confirm', result);
      return;
    }
    const replay = confirm('d');
    if (replay.response.status !== 200) {
      finish(user, 'confirm_replay', replay);
      return;
    }
  }
  if (mode === 'queue') {
    finish(user, '', null);
    return;
  }

  let lastPoll;
  const pending = poll((attempt) => {
    const url = `${TASK}?team_id=${config.team}&after_id=${config.taskAfter}&limit=100`;
    const list = request(user, 'e', 'GET', url, null,
      { name: 'task_poll', internal: true, attempt, measure: false });

    const rows = Array.isArray(list.data) ? list.data : list.data?.tasks;
    const match = rows?.find((row) =>
      String(row.proposalId ?? row.proposal_id) === user.proposal &&
      String(row.userId ?? row.user_id) === user.userId);

    lastPoll = {
      url,
      status: list.response.status,
      error_code: list.response.error_code,
      request_id: list.id,
      expected_proposal: user.proposal,
      expected_proposal_type: typeof user.proposal,
      expected_user: user.userId,
      expected_user_type: typeof user.userId,
      parsed_type: Array.isArray(list.data) ? 'array' : typeof list.data,
      body_preview: String(list.response.body ?? '').slice(0, 300),
      row_count: rows?.length,
    };

    return list.response.status === 200 && match;
  });
  if (!pending) {
    if (exec.scenario.iterationInTest < 3) {
      console.warn(JSON.stringify({ check: 'task_poll', ...lastPoll }));
    }
    finish(user, 'task_delivery_timeout', null);
    return;
  }
  latency.add(Date.now() - startedAt, { step: mode === 'drain' ? 'recovery_visible' : 'task_visible', kind: 'default' });
  const taskID = String(pending.taskId ?? pending.task_id);
  if (!/^[1-9]\d*$/.test(taskID)) {
    finish(user, 'invalid_task_id', null);
    return;
  }
  console.log(JSON.stringify({
    fixture: user.index, scenario: exec.scenario.name,
    proposal_id: user.proposal, task_id: taskID,
  }));
  const approved = user.index % 2 === 1;
  const status = approved ? 'approved' : 'rejected';
  const decision = request(user, 'f', 'PUT', `${TASK}/${taskID}/decision`, {
    teamId: config.team, decidedByUserId: decider,
    status: `ACCESS_REQUEST_STATUS_${status.toUpperCase()}`,
    comment: 'Synthetic load test decision',
  }, { name: 'task_decision', internal: true });
  if (decision.response.status !== 200 ||
      decision.data?.status !== `ACCESS_REQUEST_STATUS_${status.toUpperCase()}` ||
      String(decision.data?.taskId ?? decision.data?.task_id) !== taskID) {
    finish(user, 'task_decision', decision);
    return;
  }
  const decidedAt = Date.now();
  const expectedText = `Статус задачи ${taskID} изменён: ${status}.`;

  let lastHistory;
  let duplicate = false;

  const final = poll((attempt) => {
    lastHistory = request(user, 'g', 'GET', `${CHAT}/messages?page_size=10`, null, {
      name: 'status_poll',
      attempt,
      measure: false,
    });

    const messages = lastHistory.data?.messages;
    if (lastHistory.response.status !== 200 ||
        !lastHistory.correlated ||
        !Array.isArray(messages)) {
      return false;
    }

    const matches = messages.filter((row) => row.text === expectedText);
    duplicate = matches.length > 1;
    return matches.length > 0;
  });

  if (final && !duplicate) {
    latency.add(Date.now() - decidedAt, {
      step: 'decision_visible_in_chat',
      kind: 'default',
    });
  }

  const reason = duplicate ? 'duplicate_status_message' : final ? '' : 'chat_status_timeout';
  finish(user, reason, lastHistory || decision);

}
